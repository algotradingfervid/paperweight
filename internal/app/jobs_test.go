package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const longAgo = "2000-01-01T00:00:00.000Z"

// One RunJobs pass does all three hourly jobs.
func TestApp_RunJobsOnePass(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	ctx := context.Background()

	orphan, err := files.BlobPath(w.dir, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(orphan), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orphan, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	when := testkit.Clock().Now().Add(-2 * files.GCGrace)
	if err := os.Chtimes(orphan, when, when); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := w.app.DB().SQL().ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("exec %q: %v", q, err)
		}
	}
	exec(`INSERT INTO sessions (token_hash, user_id, data, created_at, last_seen_at) VALUES ('old', ?, x'00', ?, ?)`, adminID, longAgo, longAgo)
	exec(`INSERT INTO login_attempts (key, count, first_failure_at, expires_at) VALUES ('old', 9, ?, ?)`, longAgo, longAgo)

	if err := w.app.RunJobs(ctx); err != nil {
		t.Fatalf("RunJobs: %v", err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("the orphan past files.GCGrace still exists (%v)", err)
	}
	if n := w.count(`SELECT count(*) FROM sessions`); n != 0 {
		t.Errorf("sessions = %d, want the expired one gone", n)
	}
	if n := w.count(`SELECT count(*) FROM login_attempts`); n != 0 {
		t.Errorf("lockouts = %d, want the expired one gone", n)
	}
}

// Every job runs and every failure comes back.
func TestApp_RunJobsReportsEachFailure(t *testing.T) {
	w := newWorld(t)
	if err := w.app.DB().Close(); err != nil {
		t.Fatal(err)
	}
	err := w.app.RunJobs(context.Background())
	if err == nil {
		t.Fatal("RunJobs on a closed database = nil, want the failures")
	}
	for _, job := range []string{"session sweep", "lockout sweep"} {
		if !strings.Contains(err.Error(), job) {
			t.Errorf("RunJobs = %q, want it to name %s", err, job)
		}
		if !strings.Contains(w.logs.String(), job) {
			t.Errorf("logs = %q, want a line for %s", w.logs.String(), job)
		}
	}
}

// Run serves until its context ends, and the kernel's slog calls reach the
// same JSON logger (spec §7).
func TestApp_RunServesAndLogsJSON(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })

	logs := &lockedBuffer{}
	bound := make(chan string, 1)
	a, err := app.New(context.Background(), app.Config{
		DataDir: t.TempDir(), Addr: "127.0.0.1:0", LogOutput: logs,
		OnListen: func(addr string) { bound <- addr },
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	select {
	case <-bound:
	case err := <-done:
		t.Fatalf("Run stopped before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not listen within 5 s")
	}
	slog.Info("from the kernel")
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop within 5 s")
	}
	var found bool
	for line := range strings.Lines(logs.String()) {
		var v map[string]any
		if err := json.Unmarshal([]byte(line), &v); err != nil {
			t.Errorf("log line %q isn't JSON: %v", line, err)
		}
		found = found || v["msg"] == "from the kernel"
	}
	if !found {
		t.Errorf("logs = %q, want the default logger's line", logs.String())
	}
}

func TestApp_RunRefusesABusyAddress(t *testing.T) {
	w := newWorld(t)
	addr := strings.TrimPrefix(w.srv.URL, "http://")
	a, err := app.New(context.Background(), app.Config{DataDir: t.TempDir(), Addr: addr, LogOutput: &bytes.Buffer{}})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	if err := a.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "listen on") {
		t.Errorf("Run on a busy address = %v, want the listen error", err)
	}
}

// A connection that never sends a request can't hold up a stop.
func TestApp_RunStopsWithAnIdleConnection(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	bound := make(chan string, 1)
	a, err := app.New(context.Background(), app.Config{
		DataDir: t.TempDir(), Addr: "127.0.0.1:0", LogOutput: &lockedBuffer{},
		OnListen: func(addr string) { bound <- addr },
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", <-bound)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	time.Sleep(50 * time.Millisecond) // the server has to accept it first
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop within 5 s")
	}
}

// addRule adds an approval rule through its real screen.
func (b *browser) addRule(form url.Values) {
	b.w.t.Helper()
	if r := b.post("/admin/approval-rules", form); r.status >= http.StatusBadRequest {
		b.w.t.Fatalf("POST /admin/approval-rules = %d (%q)", r.status, r.pageText())
	}
}

// The check page's approval line (design-system §8.3), from approval.Evaluate.
func TestApp_ApprovalPreview(t *testing.T) {
	w := newWorld(t)
	admin, fx, m := w.canonical()
	adminB := w.signedIn(adminEmail)
	priya := w.signedIn(priyaEmail)
	check := func(b *browser, path string) string {
		t.Helper()
		return b.get(path + "/edit/check").pageText()
	}
	wantPreview := func(got, want string) {
		t.Helper()
		if !strings.Contains(got, want) {
			t.Errorf("check page %q, want %q", got, want)
		}
	}

	small := priya.newOrder(m, "PRJ-METRO", "1000.00")
	wantPreview(check(priya, orderURL(small)), "No approval needed. It will be approved and numbered when you submit.")

	adminB.purchaseManagerRule()
	big := priya.newOrder(m, "PRJ-METRO", "94164.00")
	wantPreview(check(priya, orderURL(big)), "This purchase order needs approval from a Purchase Manager because it is ₹50,000 or more.")

	// A rule no one can satisfy shows APR's refusal, and the page still opens.
	if err := w.app.Auth().CreateRole(context.Background(), admin, "Internal Audit"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	adminB.addRule(url.Values{"doctype": {fixture.Order}, "from_amount": {"100000.00"}, "approver_type": {"role"}, "role": {"Internal Audit"}})
	huge := priya.newOrder(m, "PRJ-METRO", "200000.00")
	wantPreview(check(priya, orderURL(huge)), "No one else can approve this Purchase Order.")
	if _, _, err := w.app.Auth().CreateUser(context.Background(), admin, auth.NewUser{
		Email: "ila@vardhan.test", Name: "Ila Sen", Password: testPassword, Roles: []string{"Internal Audit"},
	}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll}
	if err := w.app.Auth().SetRights(context.Background(), admin, "Internal Audit", fixture.Order, rights); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	wantPreview(check(priya, orderURL(huge)), "needs approval from an Internal Audit because it is ₹1,00,000 or more.")

	// A reporting-manager rule from zero names the manager and gives no reason.
	adminB.addRule(url.Values{
		"doctype": {fixture.Claim}, "from_amount": {"0.00"}, "approver_type": {"reporting_manager"}, "fallback_role": {"Director"},
	})
	var emp int64
	for _, e := range fx.Employees {
		if e.UserID == deepakID {
			emp = e.ID
		}
	}
	deepak := w.signedIn(deepakEmail)
	r := deepak.post("/doc/"+fixture.Claim+"/new", url.Values{
		"employee_id": {strconv.FormatInt(emp, 10)}, "claim_date": {"24-10-2026"},
	})
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST claim = %d (%q), want 303", r.status, r.pageText())
	}
	path, _, _ := strings.Cut(r.location(), "/edit/")
	wantPreview(check(deepak, path), "This expense claim needs approval from Priya Shah.")
}

// A document page's history: reasons, the system as author, and files by kind.
func TestApp_DocumentHistoryAndFiles(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	ravi := w.signedIn(raviEmail)
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	priya.addLine(m, id)
	for _, f := range []struct {
		name string
		data []byte
	}{{"photo.heic", filestest.HEIC("heic")}, {"note.pdf", filestest.PDF(1000)}} {
		r := priya.upload("/attachments/"+fixture.Order+"/"+strconv.FormatInt(id, 10), f.name, f.data, nil)
		if r.status != http.StatusCreated {
			t.Fatalf("upload %s = %d (%q), want 201", f.name, r.status, r.body)
		}
	}
	priya.act(id, "submit", nil)
	ravi.act(id, "cancel", url.Values{"reason": {"Wrong supplier"}})
	if _, err := w.app.DB().SQL().ExecContext(context.Background(),
		`INSERT INTO audit_log (doc_type, doc_id, action, user_id, at) VALUES (?, ?, 'imported', 0, ?)`, fixture.Order, id, longAgo); err != nil {
		t.Fatal(err)
	}
	r := priya.get(orderURL(id))
	if r.status != http.StatusOK {
		t.Fatalf("GET the order = %d (%q), want 200", r.status, r.pageText())
	}
	var changes []string
	r.doc.Find(".history-changes li").Each(func(_ int, s *goquery.Selection) { changes = append(changes, s.Text()) })
	for _, want := range []string{"Supplier: null → ", "Line 1: (empty) → "} {
		if !slices.ContainsFunc(changes, func(c string) bool { return strings.HasPrefix(c, want) }) {
			t.Errorf("change rows %q, want one starting %q (under the field's label)", changes, want)
		}
	}
	for _, want := range []string{
		"· System · imported", "· Ravi Kulkarni · cancel: Wrong supplier", "· Priya Shah · Approved on submit (no approval needed)",
		"HEIC image, opens as download", "note.pdf",
	} {
		if !strings.Contains(r.pageText(), want) {
			t.Errorf("page %q, want %q", r.pageText(), want)
		}
	}
}

// The rest of what the adapters and the export route pass through.
func TestApp_AdapterDetails(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	admin := w.signedIn(adminEmail)
	priya := w.signedIn(priyaEmail)
	ravi := w.signedIn(raviEmail)

	t.Run("form_dates", func(t *testing.T) {
		body := priya.get(orderPath + "/new").body
		for _, date := range []string{"24-10-2026", "23-10-2026"} {
			if !strings.Contains(body, date) {
				t.Errorf("the new-order form has no %s (Deps.Today)", date)
			}
		}
	})

	id := w.pendingOrder(admin, priya, m)

	// No fixture field is Filterable, so the kernel's refusal proves the
	// filter reached it.
	t.Run("list_filter", func(t *testing.T) {
		r := priya.get(orderPath + "?f=" + url.QueryEscape("amount:=:1000.00"))
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.pageText(), "You can't filter Purchase Orders by 'amount'.") {
			t.Errorf("filtered list = %d (%q), want list's 422", r.status, r.pageText())
		}
	})

	t.Run("next_request", func(t *testing.T) {
		second := priya.newOrder(m, "PRJ-METRO", "94164.00")
		priya.addLine(m, second)
		priya.act(second, "submit", nil)
		approved := ravi.act(second, "approve", nil)
		next := ravi.get(approved.location()).doc.Find(`a[href="/approvals"]:contains("Next request")`)
		if next.Length() != 1 {
			t.Errorf("after an approve at %s: no Next request link to the inbox", approved.location())
		}
	})

	t.Run("rejection_reason", func(t *testing.T) {
		ravi.act(id, "reject", url.Values{"reason": {"Rate too high"}})
		page := priya.get(orderURL(id))
		if !strings.Contains(page.pageText(), "· Ravi Kulkarni · Rejected: Rate too high") {
			t.Errorf("page %q, want the reason in the approval history", page.pageText())
		}
	})

	t.Run("export_refusals", func(t *testing.T) {
		if r := priya.get("/doc/no.Such/export.csv"); r.status != http.StatusNotFound {
			t.Errorf("export of an unknown DocType = %d, want 404", r.status)
		}
		if r := priya.get(orderPath + "/export.csv?sort=nothing.asc"); r.status != http.StatusUnprocessableEntity {
			t.Errorf("export with a bad sort = %d, want 422", r.status)
		}
	})
}

// The hour is the default, and Run does the jobs on its ticker.
func TestApp_RunDoesTheJobs(t *testing.T) {
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	w := newWorld(t)
	if got := w.app.JobEvery(); got != time.Hour {
		t.Errorf("jobs run every %v, want an hour", got)
	}
	w.firstAdmin()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := w.app.DB().SQL().ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, data, created_at, last_seen_at) VALUES ('old', ?, x'00', ?, ?)`, adminID, longAgo, longAgo); err != nil {
		t.Fatal(err)
	}
	w.app.SetJobEvery(5 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- w.app.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for w.count(`SELECT count(*) FROM sessions`) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("Run did not do the jobs within 5 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Run after cancel = %v, want nil", err)
	}
}

// Two administrators can't both save: the exists check runs inside the write.
// The POST starts while another save holds the write and waits at BEGIN
// IMMEDIATE, so a check made before the write would still see no company and
// get MST-019's refusal instead of the 404. The fixed clock means the session
// touch doesn't write, so the request isn't stopped before the handler.
func TestApp_CompanySetupCheckedInsideWrite(t *testing.T) {
	w := newWorld(t)
	admin := w.firstAdmin()
	b := w.signedIn(adminEmail)
	ctx := context.Background()
	in := company.SetupInput{LegalName: "Vardhan Infra Projects Pvt Ltd", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", BranchCode: "PUNE"}

	answer := make(chan *response, 1)
	err := w.app.DB().Write(ctx, func(tx *db.Tx) error {
		if err := w.app.Masters().Company.SetupTx(ctx, tx, admin, in); err != nil {
			return err
		}
		go func() { answer <- b.post("/setup/company", goodSetup()) }()
		waitForBegin(t, w)
		return nil
	})
	if err != nil {
		t.Fatalf("the held write: %v", err)
	}
	if r := <-answer; r.status != http.StatusNotFound {
		t.Errorf("POST behind the held write = %d (%q), want 404", r.status, r.pageText())
	}
	if n := w.count(`SELECT count(*) FROM company`); n != 1 {
		t.Errorf("company rows = %d, want 1", n)
	}
}

// waitForBegin returns once a second connection has stayed in use for 200 ms.
// The held write has the first; the request's reads last far less, so a
// connection that stays is the one waiting at BEGIN IMMEDIATE. The pool offers
// no other sign of it.
func waitForBegin(t *testing.T, w *world) {
	t.Helper()
	const steady = 200 * time.Millisecond
	deadline := time.Now().Add(4 * time.Second)
	since := time.Now()
	for time.Since(since) < steady {
		if time.Now().After(deadline) {
			t.Error("the POST never reached BEGIN IMMEDIATE")
			return
		}
		if w.app.DB().SQL().Stats().InUse < 2 {
			since = time.Now()
		}
		time.Sleep(time.Millisecond)
	}
}

func TestApp_CompanySetupDetails(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	if r := w.signedIn(priyaEmail).get("/setup/company"); r.alert() != "Only an administrator can set up the company." {
		t.Errorf("refusal = %q, want the administrator text", r.alert())
	}
	form := goodSetup()
	form.Set("branch_name", "")
	if r := w.signedIn(adminEmail).post("/setup/company", form); r.status != http.StatusSeeOther {
		t.Fatalf("POST with a blank head office name = %d (%q), want 303", r.status, r.pageText())
	}
	if got := w.text(`SELECT name FROM branch`); got != "Head Office" {
		t.Errorf("branch name = %q, want the default", got)
	}
}

// APR-006: a rule can't name a role that doesn't exist. Roles have no disabled
// state, so that half of the case can't arise.
func TestApp_ApprovalRuleRoleMustExist(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	r := w.signedIn(adminEmail).post("/admin/approval-rules", url.Values{
		"doctype": {fixture.Order}, "from_amount": {"50000.00"}, "approver_type": {"role"}, "role": {"Site Auditor"},
	})
	if want := "Role Site Auditor doesn't exist. Choose an active role."; !strings.Contains(r.pageText(), want) {
		t.Errorf("POST = %d (%q), want %q", r.status, r.pageText(), want)
	}
	if n := w.count(`SELECT count(*) FROM approval_rules`); n != 0 {
		t.Errorf("rules = %d, want none saved", n)
	}
}

func TestApp_MountRefusesMiddleware(t *testing.T) {
	sub := chi.NewRouter()
	sub.Use(func(next http.Handler) http.Handler { return next })
	sub.Get("/x", func(http.ResponseWriter, *http.Request) {})
	defer func() {
		if recover() == nil {
			t.Error("mount dropped a middleware without a word")
		}
	}()
	app.Mount(chi.NewRouter(), sub, func(_ string, h http.Handler) http.Handler { return h })
}
