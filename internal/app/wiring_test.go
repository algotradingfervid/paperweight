package app_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
)

// CD-198: Config.Secure and Config.TrustProxy reach authweb.
func TestApp_ProxyIPAndSecureCookie(t *testing.T) {
	const hops = "198.51.100.7, 203.0.113.9"
	xff := map[string]string{"X-Forwarded-For": hops}

	cookie := func(r *response) *http.Cookie {
		t.Helper()
		for _, c := range (&http.Response{Header: r.header}).Cookies() {
			if strings.HasSuffix(c.Name, "session") {
				return c
			}
		}
		t.Fatalf("no session cookie on the sign-in response: %v", r.header)
		return nil
	}

	t.Run("behind_the_proxy", func(t *testing.T) {
		w := newWorld(t, func(c *app.Config) { c.Secure, c.TrustProxy = true, true })
		w.firstAdmin()
		r := w.browser().trySignIn(adminEmail, xff)
		if r.status != http.StatusSeeOther {
			t.Fatalf("POST /signin = %d, want 303", r.status)
		}
		if got := w.text(`SELECT ip FROM auth_log WHERE event = 'login_success'`); got != "203.0.113.9" {
			t.Errorf("logged client IP = %q, want the entry the proxy added", got)
		}
		c := cookie(r)
		if c.Name != "__Host-session" || !c.Secure || !c.HttpOnly {
			t.Errorf("cookie %s Secure %v HttpOnly %v, want __Host-session, Secure and HttpOnly", c.Name, c.Secure, c.HttpOnly)
		}
	})

	t.Run("no_proxy", func(t *testing.T) {
		w := newWorld(t)
		w.firstAdmin()
		r := w.browser().trySignIn(adminEmail, xff)
		if r.status != http.StatusSeeOther {
			t.Fatalf("POST /signin = %d, want 303", r.status)
		}
		if got := w.text(`SELECT ip FROM auth_log WHERE event = 'login_success'`); got != "127.0.0.1" {
			t.Errorf("logged client IP = %q, want the peer's: the header must be ignored", got)
		}
		if c := cookie(r); c.Name != "session" || c.Secure {
			t.Errorf("cookie %s Secure %v, want session and not Secure", c.Name, c.Secure)
		}
	})
}

// CD-10, CD-158, CD-171: SeedDefaults runs before serving and the reset uses
// the same options. The reset's flash also proves web.WithSessions is reached
// through AUTH's middleware (CD-150).
func TestApp_SeedsDefaultsBeforeServing(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	readScope := func(role, dt string) auth.Scope {
		t.Helper()
		rights, err := w.app.Auth().RoleRights(ctx, role, dt)
		if err != nil {
			t.Fatalf("RoleRights(%s, %s): %v", role, dt, err)
		}
		return rights["read"]
	}
	if got := readScope("Purchase User", fixture.Order); got != auth.ScopeAll {
		t.Errorf("Purchase User read on Order = %q, want all (DefaultRights seeded)", got)
	}
	if got := readScope("Purchase User", fixture.Party); got != auth.ScopeAll {
		t.Errorf("Purchase User read on a linked master = %q, want all (LinkedMasterRead)", got)
	}

	w.firstAdmin()
	b := w.signedIn(adminEmail)
	r := b.post("/rights/"+fixture.Party+"/reset", nil)
	if r.status != http.StatusSeeOther {
		t.Fatalf("POST reset = %d (%q), want 303", r.status, r.pageText())
	}
	if got := readScope("Purchase User", fixture.Party); got != auth.ScopeAll {
		t.Errorf("Purchase User read on a linked master after a reset = %q, want all (authweb.Config.Seed)", got)
	}
	want := "Party rights were reset to defaults for every role."
	if page := b.get(r.location()); !strings.Contains(page.pageText(), want) {
		t.Errorf("page after the reset = %q, want the flash %q", page.pageText(), want)
	}
}

// CD-150: Sessions stays nil, so one sign-in saves one session row and later
// requests keep working.
func TestApp_SessionSavedOnce(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	b := w.signedIn(adminEmail)
	for range 2 {
		if r := b.get("/account/sessions"); r.status != http.StatusOK {
			t.Fatalf("GET /account/sessions = %d, want 200", r.status)
		}
	}
	if n := w.count(`SELECT count(*) FROM sessions WHERE user_id = ?`, adminID); n != 1 {
		t.Errorf("sessions for the admin = %d, want 1", n)
	}
}

// CD-194, CD-195.
func TestApp_PickerURL(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	dt, ok := auth.MasterFor(auth.DimProject)
	if !ok {
		t.Fatal("no master for the project dimension")
	}
	r := w.signedIn(adminEmail).get("/users/4/access")
	if r.status != http.StatusOK {
		t.Fatalf("GET /users/4/access = %d (%q), want 200", r.status, r.pageText())
	}
	if want := "/pick/" + url.PathEscape(dt.Name); !strings.Contains(r.body, want) {
		t.Errorf("the access page doesn't search %s", want)
	}
}

// CD-154, CD-209: files.New's Policies and Info, and Letterhead.LogoURL.
func TestApp_CompanyLogo(t *testing.T) {
	w := newWorld(t)
	adminP, _, _ := w.canonical()
	ctx := context.Background()
	version := w.text(`SELECT CAST(version AS TEXT) FROM company`)

	admin := w.signedIn(adminEmail)
	// LogoPolicy, not DefaultPolicy, which takes a PDF.
	r := admin.upload("/m/company/1/logo?version="+version, "logo.pdf", filestest.PDF(2000), nil)
	if !strings.Contains(r.pageText(), "Logo must be a PNG or JPG image.") {
		t.Errorf("PDF logo = %d (%q), want LogoPolicy's refusal", r.status, r.pageText())
	}
	r = admin.upload("/m/company/1/logo?version="+version, "logo.png", filestest.PNG(2000), nil)
	if r.status >= http.StatusBadRequest {
		t.Fatalf("logo upload = %d (%q)", r.status, r.pageText())
	}
	id := w.text(`SELECT CAST(id AS TEXT) FROM attachment WHERE parent_doctype = ?`, company.CompanyDocType)

	var got string
	asUser := auth.WithPrincipal(ctx, w.principal(priyaID)) // CD-209: Latest reads the user from ctx
	err := w.app.DB().Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = w.app.Masters().Letterhead.LogoURL(asUser, tx)
		return err
	})
	if err != nil {
		t.Fatalf("Letterhead.LogoURL: %v", err)
	}
	if want := "/files/" + id; got != want {
		t.Errorf("Letterhead.LogoURL = %q, want %q", got, want)
	}
	// FILE-048: LogoPolicy lets any signed-in user read the logo.
	nobody, _, err := w.app.Auth().CreateUser(ctx, adminP, auth.NewUser{Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{}})
	if err != nil {
		t.Fatalf("CreateUser with no roles: %v", err)
	}
	if err := w.app.Auth().Can(ctx, w.principal(nobody), company.CompanyDocType, "read", nil); err == nil {
		t.Fatal("the role-less user can read Company, so the check below proves nothing")
	}
	if r := w.signedIn(nobodyEmail).get(got); r.status != http.StatusOK {
		t.Errorf("GET %s with no Company read = %d, want 200", got, r.status)
	}
}

// CD-194, CD-198: the shell is built from rights, per request.
func TestApp_ShellNavInboxCSRF(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	admin := w.signedIn(adminEmail)
	priya := w.signedIn(priyaEmail)
	ravi := w.signedIn(raviEmail)
	w.pendingOrder(admin, priya, m)

	nav := func(r *response, href string) bool {
		return r.doc.Find(`.nav-list a[href="`+href+`"]`).Length() > 0
	}
	adminOnly := []string{"/users", "/roles", "/settings/numbering", "/admin/approval-rules"}

	t.Run("administrator", func(t *testing.T) {
		r := admin.get("/approvals")
		for _, href := range append([]string{"/approvals", "/approvals/mine", orderPath, "/m/expense-type"}, adminOnly...) {
			if !nav(r, href) {
				t.Errorf("Administrator's nav has no %s", href)
			}
		}
		// A master whose screens aren't built has no item: every link opens.
		r.doc.Find(`.app-sidebar .nav-list a`).Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			if page := admin.get(href); page.status != http.StatusOK {
				t.Errorf("nav item %s = %d, want 200", href, page.status)
			}
		})
	})

	t.Run("approver", func(t *testing.T) {
		r := ravi.get("/approvals")
		if !nav(r, orderPath) {
			t.Errorf("Ravi's nav has no %s", orderPath)
		}
		for _, href := range adminOnly {
			if nav(r, href) {
				t.Errorf("Ravi's nav has the administrator item %s", href)
			}
		}
		if got := strings.TrimSpace(r.doc.Find(`.inbox-link .badge`).Text()); got != "1" {
			t.Errorf("inbox count = %q, want 1", got)
		}
		if got := strings.TrimSpace(r.doc.Find(`.user-name`).First().Text()); got != "Ravi Kulkarni" {
			t.Errorf("shell user = %q, want Ravi Kulkarni", got)
		}
		if got, _ := r.doc.Find(`.nav-list a[aria-current="page"]`).First().Attr("href"); got != "/approvals" {
			t.Errorf("current nav item = %q, want /approvals (Shell.Path)", got)
		}
		hx, _ := r.doc.Find("body").Attr("hx-headers")
		if !strings.Contains(hx, ravi.csrf) {
			t.Errorf("body hx-headers = %q, want the session's CSRF token (CD-15)", hx)
		}
	})

	t.Run("no_read_right_no_item", func(t *testing.T) {
		r := w.signedIn(deepakEmail).get("/approvals")
		if nav(r, orderPath) {
			t.Errorf("Deepak's nav has %s, which he can't read", orderPath)
		}
		if !nav(r, "/doc/"+fixture.Claim) {
			t.Errorf("Deepak's nav has no /doc/%s", fixture.Claim)
		}
	})
}

// CD-145: the route's guard is read, the handler checks export.
func TestApp_ExportCSV(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	w.signedIn(priyaEmail).newOrder(m, "PRJ-METRO", "1000.00")
	path := orderPath + "/export.csv"

	r := w.signedIn(priyaEmail).get(path)
	if r.status != http.StatusOK || !strings.HasPrefix(r.header.Get("Content-Type"), "text/csv") {
		t.Fatalf("export as Priya = %d %q, want 200 text/csv", r.status, r.header.Get("Content-Type"))
	}
	if !strings.Contains(r.body, "SHREE") {
		t.Errorf("the CSV %q has no row for the order", r.body)
	}
	for _, c := range []struct{ email, want string }{
		{sureshEmail, "You don't have permission to export Purchase Order. Ask your administrator for a role that allows it."},
		{deepakEmail, "You don't have permission to read Purchase Order. Ask your administrator for a role that allows it."},
	} {
		r := w.signedIn(c.email).get(path)
		if r.status != http.StatusForbidden || r.alert() != c.want {
			t.Errorf("export as %s = %d %q, want 403 %q", c.email, r.status, r.alert(), c.want)
		}
	}
}

// CD-206, CD-211: the ceiling runs after the security headers and the access
// log, ahead of anything that parses the form. Served in this goroutine, so
// the log line is there when ServeHTTP returns.
func TestApp_BodyCeiling(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	const declared = 33 << 20 // past any limit: the ceiling stays under 32 MiB (CD-211)

	req := httptest.NewRequest(http.MethodPost, "/attachments/"+fixture.Order+"/1", strings.NewReader("x"))
	req.ContentLength = declared
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	rec := httptest.NewRecorder()
	w.app.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("the 413 has no Content-Security-Policy")
	}
	if want := `<div role="alert">` + files.MsgTooLarge + `</div>`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("body = %q, want FILE-003's text in the alert div", rec.Body.String())
	}
	if !logged(t, w.logs.String(), http.StatusRequestEntityTooLarge) {
		t.Errorf("no access-log line with status 413 in %q", w.logs.String())
	}
}

// logged reports whether a JSON "request" line has the status; every line
// must be JSON (spec §7).
func logged(t *testing.T, logs string, status int) bool {
	t.Helper()
	found := false
	sc := bufio.NewScanner(strings.NewReader(logs))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Errorf("log line %q isn't JSON: %v", sc.Text(), err)
			continue
		}
		if line["msg"] == "request" && line["status"] == float64(status) {
			found = true
		}
	}
	return found
}

// CD-206, CD-209, CD-195: masters mount through allmasters.Routes.
func TestApp_MastersMounted(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	if _, err := w.app.Masters().ExpenseType.Create(context.Background(), admin, expensetype.Input{Code: "ZEBRA", Name: "Zebra Hire"}); err != nil {
		t.Fatalf("create expense type: %v", err)
	}
	b := w.signedIn(adminEmail)
	r := b.get("/m/expense-type")
	if r.status != http.StatusOK || !strings.Contains(r.pageText(), "Zebra Hire") {
		t.Errorf("GET /m/expense-type = %d, text %q, want 200 with the row", r.status, r.pageText())
	}
	if r := b.get("/pick/" + url.PathEscape(expensetype.DocType) + "?q=ZEBRA"); r.status != http.StatusOK || !strings.Contains(r.pageText(), "Zebra Hire") {
		t.Errorf("GET the expense type picker = %d, text %q, want 200 with the row (CD-195)", r.status, r.pageText())
	}
}

// CD-87, CD-198, CD-209, CD-212: real pages reach every parser.
func TestApp_ValidateParsers(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	b := w.signedIn(adminEmail)
	for _, c := range []struct {
		kind, value string
		status      int
	}{
		{"money", "not a number", http.StatusOK},    // components.NUMParsers
		{"gstin", "27AABCS1429B1Z5", http.StatusOK}, // gstinweb.Parsers
		{"factor6", "not a number", http.StatusOK},  // itemweb.Parsers
		{"no-such-kind", "1", http.StatusNotFound},
	} {
		t.Run(c.kind, func(t *testing.T) {
			r := b.post("/ui/validate/"+c.kind, url.Values{"_id": {"f"}, "_name": {"v"}, "v": {c.value}})
			if r.status != c.status {
				t.Fatalf("POST /ui/validate/%s = %d, want %d", c.kind, r.status, c.status)
			}
			if c.status == http.StatusOK && r.pageText() == "" {
				t.Errorf("POST /ui/validate/%s with a bad value showed no message", c.kind)
			}
		})
	}
}

// CD-87, CD-200, CD-215.
func TestApp_SearchSelfNoDefaultFilters(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	var firstID int64
	for i := range 6 {
		s, err := w.app.Masters().ExpenseType.Create(ctx, admin, expensetype.Input{Code: "ZEB" + strconv.Itoa(i), Name: "Zebra " + strconv.Itoa(i)})
		if err != nil {
			t.Fatalf("create expense type: %v", err)
		}
		if i == 0 {
			firstID = s.ID
			if err := w.app.Masters().ExpenseType.SetDisabled(ctx, admin, s.ID, s.Version, true); err != nil {
				t.Fatalf("disable: %v", err)
			}
		}
	}

	r := w.signedIn(adminEmail).get("/search?q=Zebra")
	if r.status != http.StatusOK {
		t.Fatalf("GET /search = %d, want 200", r.status)
	}
	if !strings.Contains(r.pageText(), "See all 6 Expense Types") {
		t.Errorf("page %q, want 6 expense types counted: a disabled master stays findable (CD-215)", r.pageText())
	}
	if want := "/m/expense-type/" + strconv.FormatInt(firstID, 10); r.doc.Find(`a[href="`+want+`"]`).Length() == 0 {
		t.Errorf("no link to %s: RecordURL must send masters to /m/ (CD-200)", want)
	}
	if r.doc.Find(`a[href^="/m/expense-type?"]`).Length() == 0 {
		t.Error("no See all link under /m/expense-type: ListURL must send masters to /m/")
	}
	if r.doc.Find(`a[href^="/doc/Expense"]`).Length() != 0 {
		t.Error("a master result links under /doc/")
	}
	if r := w.signedIn(deepakEmail).get("/search?q=Zebra"); r.status != http.StatusOK {
		t.Errorf("GET /search as Deepak = %d, want 200: Self, not a right", r.status)
	}
}

// CD-225; a handler that sets its own header wins.
func TestApp_CacheControl(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	const want = "private, no-store"
	priya := w.signedIn(priyaEmail)

	for _, path := range []string{"/approvals", "/account/password", orderPath} {
		if got := priya.get(path).header.Get("Cache-Control"); got != want {
			t.Errorf("GET %s signed in: Cache-Control %q, want %q", path, got, want)
		}
	}
	if got := w.browser().get("/signin").header.Get("Cache-Control"); got == want {
		t.Errorf("GET /signin signed out: Cache-Control %q, want no signed-in header", got)
	}

	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	u := priya.upload("/attachments/"+fixture.Order+"/"+strconv.FormatInt(id, 10), "receipt.jpg", filestest.JPEG(1000), nil)
	if u.status != http.StatusCreated {
		t.Fatalf("upload = %d (%q), want 201", u.status, u.body)
	}
	var meta struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal([]byte(u.body), &meta); err != nil {
		t.Fatalf("upload answer %q: %v", u.body, err)
	}
	// A handler's own header stays: one value on a download, and the hashed
	// assets keep their immutable caching for a signed-in browser.
	d := priya.get("/files/" + strconv.FormatInt(meta.ID, 10) + "?download=1")
	if d.status != http.StatusOK || len(d.header.Values("Cache-Control")) != 1 {
		t.Errorf("download = %d, Cache-Control %q, want 200 and exactly one value", d.status, d.header.Values("Cache-Control"))
	}
	if got := priya.get(ui.CSSURL).header.Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("GET %s signed in: Cache-Control %q, want the static handler's immutable header", ui.CSSURL, got)
	}
}

// Spec §7, CD-198.
func TestApp_HealthzReadsDatabase(t *testing.T) {
	w := newWorld(t)
	b := w.browser()
	if r := b.get("/healthz"); r.status != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", r.status)
	}
	if err := w.app.DB().Close(); err != nil {
		t.Fatalf("close the database: %v", err)
	}
	if r := b.get("/healthz"); r.status != http.StatusServiceUnavailable {
		t.Errorf("GET /healthz with the database closed = %d, want 503", r.status)
	}
}

func TestApp_JSONLogs(t *testing.T) {
	w := newWorld(t)
	rec := httptest.NewRecorder()
	w.app.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if !logged(t, w.logs.String(), http.StatusOK) {
		t.Errorf("Config.LogOutput = %q, want a JSON request line with status 200", w.logs.String())
	}
}

func TestApp_HSTSOnlyWhenSecure(t *testing.T) {
	const header = "Strict-Transport-Security"
	secure := newWorld(t, func(c *app.Config) { c.Secure = true })
	if got := secure.browser().get("/healthz").header.Get(header); !strings.Contains(got, "max-age=") {
		t.Errorf("Secure: %s = %q, want a max-age", header, got)
	}
	plain := newWorld(t)
	if got := plain.browser().get("/healthz").header.Get(header); got != "" {
		t.Errorf("not Secure: %s = %q, want none", header, got)
	}
}

// CD-99, U7.
func TestApp_StaticMounts(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	b := w.browser()
	for _, path := range []string{ui.CSSURL, web.HTMXURL, brand.AppLogoURL, brand.DocumentLogoURL} {
		if r := b.get(path); r.status != http.StatusOK {
			t.Errorf("GET %s signed out = %d, want 200", path, r.status)
		}
	}
}

// CD-28.
func TestApp_EnsureSeries(t *testing.T) {
	w := newWorld(t)
	for dt, want := range map[string]string{
		fixture.Order: "PO/{FY}/{#####}", fixture.Receipt: "GR/{FY}/{#####}", fixture.Claim: "EX/{FY}/{#####}",
	} {
		if got := w.text(`SELECT template FROM naming_series WHERE doctype = ?`, dt); got != want {
			t.Errorf("series of %s = %q, want %q", dt, got, want)
		}
	}
}

// CD-30, CD-05: the print page reaches company's Letterhead and DOC's Info.
func TestApp_PrintUsesLetterheadAndInfo(t *testing.T) {
	w := newWorld(t)
	_, fx, m := w.canonical()
	priya := w.signedIn(priyaEmail)
	id := priya.newOrder(m, "PRJ-METRO", "1000.00")
	r := priya.get("/print/" + fixture.Order + "/" + strconv.FormatInt(id, 10))
	if r.status != http.StatusOK {
		t.Fatalf("GET print page = %d (%q), want 200", r.status, r.pageText())
	}
	if !strings.Contains(r.pageText(), fx.Company.LegalName) {
		t.Errorf("print page text %q, want the company's legal name", r.pageText())
	}
}

// CD-13: MST's directory backs AUTH's team scope. Priya manages Deepak.
func TestApp_DirectoryBacksTeamScope(t *testing.T) {
	w := newWorld(t)
	_, fx, _ := w.canonical()
	var deepakEmp int64
	for _, e := range fx.Employees {
		if e.UserID == deepakID {
			deepakEmp = e.ID
		}
	}
	if deepakEmp == 0 {
		t.Fatal("no employee for Deepak in the seed")
	}
	rec := &auth.RecordRef{DocType: fixture.Claim, ID: 1, CreatedBy: deepakID, EmployeeID: deepakEmp}
	ctx := context.Background()
	if err := w.app.Auth().Can(ctx, w.principal(priyaID), fixture.Claim, "read", rec); err != nil {
		t.Errorf("Priya reading her report's claim: %v", err)
	}
	if err := w.app.Auth().Can(ctx, w.principal(raviID), fixture.Claim, "read", rec); err == nil {
		t.Error("Ravi read a claim of someone who doesn't report to him directly")
	}
}

// FILE hand-off: amend copies attachments and draft delete removes them,
// through the real document routes.
func TestApp_AttachmentsFollowDocument(t *testing.T) {
	w := newWorld(t)
	_, _, m := w.canonical()
	admin := w.signedIn(adminEmail)
	priya := w.signedIn(priyaEmail)
	ravi := w.signedIn(raviEmail)
	attachments := func(id int64) int {
		t.Helper()
		return w.count(`SELECT count(*) FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, fixture.Order, id)
	}
	attach := func(id int64) {
		t.Helper()
		r := priya.upload("/attachments/"+fixture.Order+"/"+strconv.FormatInt(id, 10), "receipt.jpg", filestest.JPEG(1000), nil)
		if r.status != http.StatusCreated {
			t.Fatalf("upload on %d = %d (%q), want 201", id, r.status, r.body)
		}
	}

	t.Run("draft_delete", func(t *testing.T) {
		id := priya.newOrder(m, "PRJ-METRO", "1000.00")
		attach(id)
		priya.act(id, "delete", nil)
		if n := attachments(id); n != 0 {
			t.Errorf("attachments after deleting the draft = %d, want 0", n)
		}
	})

	t.Run("amend", func(t *testing.T) {
		id := priya.newOrder(m, "PRJ-METRO", "94164.00")
		priya.addLine(m, id)
		attach(id)
		admin.purchaseManagerRule()
		priya.act(id, "submit", nil)
		ravi.act(id, "approve", nil)
		ravi.act(id, "cancel", url.Values{"reason": {"Wrong supplier"}})
		ravi.act(id, "amend", nil)
		amended := w.text(`SELECT CAST(id AS TEXT) FROM fixture_order WHERE amended_from_id = ?`, id)
		if n := w.count(`SELECT count(*) FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, fixture.Order, amended); n != 1 {
			t.Errorf("attachments on the amendment = %d, want 1", n)
		}
	})
}

// FILE carry-over: the files prefix answers 404 with FILE's text.
func TestApp_FilesPrefixNotFound(t *testing.T) {
	w := newWorld(t)
	w.firstAdmin()
	b := w.signedIn(adminEmail)
	for _, path := range []string{"/files/999", "/files/abc", "/files/1/extra"} {
		r := b.get(path)
		if r.status != http.StatusNotFound || !strings.Contains(r.pageText(), files.MsgNotFound) {
			t.Errorf("GET %s = %d %q, want 404 %q", path, r.status, r.pageText(), files.MsgNotFound)
		}
	}
}

// namingweb, approval web and their guards.
func TestApp_AreaScreensMounted(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	admin := w.signedIn(adminEmail)
	for _, path := range []string{"/settings/numbering", "/admin/approval-rules", "/admin/approvals/stuck", "/approvals/mine"} {
		if r := admin.get(path); r.status != http.StatusOK {
			t.Errorf("GET %s as Administrator = %d (%q), want 200", path, r.status, r.pageText())
		}
	}
	deepak := w.signedIn(deepakEmail)
	if r := deepak.get("/settings/numbering"); r.status != http.StatusForbidden || r.alert() != naming.MsgForbidden {
		t.Errorf("GET /settings/numbering as Deepak = %d %q, want 403 %q", r.status, r.alert(), naming.MsgForbidden)
	}
	if r := deepak.get("/admin/approval-rules"); r.status != http.StatusForbidden {
		t.Errorf("GET /admin/approval-rules as Deepak = %d, want 403", r.status)
	}
}

// Config.TrustedOrigins reaches the cross-origin check.
func TestApp_TrustedOrigins(t *testing.T) {
	const trusted = "https://erp.vardhan.test"
	w := newWorld(t, func(c *app.Config) { c.TrustedOrigins = []string{trusted} })
	w.firstAdmin()
	b := w.browser()
	b.pageCSRF("/signin")

	// A trusted origin passes the check and meets the missing form token.
	r := b.do(http.MethodPost, "/signin", nil, "", map[string]string{"Origin": trusted})
	if r.status != http.StatusForbidden || r.alert() != auth.ErrCSRF.Msg {
		t.Errorf("POST from a trusted origin = %d %q, want 403 %q", r.status, r.alert(), auth.ErrCSRF.Msg)
	}
	r = b.do(http.MethodPost, "/signin", nil, "", map[string]string{"Origin": "https://evil.test"})
	if r.status != http.StatusForbidden || r.alert() == auth.ErrCSRF.Msg {
		t.Errorf("POST from another origin = %d %q, want the cross-origin refusal", r.status, r.alert())
	}
}

// A restart on the same data dir must not fail on seeding or series.
func TestApp_RestartOnSameDataDir(t *testing.T) {
	first := newWorld(t)
	first.firstAdmin()
	second, err := app.New(context.Background(), app.Config{
		DataDir: first.dir, LogOutput: &bytes.Buffer{},
		Prepare: func(ctx context.Context, d *db.DB) error {
			return db.Migrate(ctx, d, fixture.MigrationsFS, "goose_fixture")
		},
	})
	if err != nil {
		t.Fatalf("second app.New: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// Hourly jobs. (*App).Run's ticker has no seam and a test may not sleep for
// an hour, so this pins the call sites in the source; the floors stop a
// rename from leaving it green with nothing scanned.
func TestApp_RunJobsHourly(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]int{}
	selectors := map[string]int{}
	fset := token.NewFileSet()
	scanned := 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path) //nolint:gosec // G304: a path from the source walk
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		scanned++
		ast.Inspect(f, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.CallExpr:
				if s, ok := v.Fun.(*ast.SelectorExpr); ok {
					calls[s.Sel.Name]++
				}
			case *ast.SelectorExpr:
				selectors[v.Sel.Name]++
			}
			return true
		})
	}
	if scanned == 0 {
		t.Fatal("no non-test source in internal/app")
	}
	for _, name := range []string{"GC", "Sweep", "SweepLockouts", "NewTicker"} {
		if calls[name] == 0 {
			t.Errorf("internal/app never calls .%s", name)
		}
	}
	for _, name := range []string{"GCGrace", "Hour"} {
		if selectors[name] == 0 {
			t.Errorf("internal/app never uses .%s", name)
		}
	}
}
