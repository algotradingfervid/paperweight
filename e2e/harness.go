//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/dom"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	// password is every fixture user's password (00-fixtures.md).
	password = "Correct-Horse-42"

	// stepDeadline bounds one browser step, so a request that never answers
	// fails one test fast (engineering lessons §10).
	stepDeadline = 10 * time.Second
	waitEvery    = 25 * time.Millisecond
	pageLogLimit = 800
	logTail      = 2000

	fixturePick = "/pick/fixture."
	cspHeader   = "Content-Security-Policy"
	printPrefix = "/print/"
)

// Harness is one real App on a temp dir behind a random port: the canonical
// seed, the fixture DocTypes, their print views and the fixture clock.
type Harness struct {
	t     *testing.T
	App   *app.App
	URL   string
	Fx    testkit.Fixtures
	M     fixture.Masters
	Clock *clock.Fixed
	logs  *lockedBuffer

	delayPath atomic.Pointer[string]
	delay     atomic.Int64

	mu         sync.Mutex
	cspReports []string
}

// lockedBuffer is a log sink safe to read while the server writes to it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) tail() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return string(l.b.Bytes()[max(l.b.Len()-logTail, 0):])
}

// Start builds the App. Prepare seeds, because testkit.Seed has to run before
// SeedDefaults; Seed writes no passwords, restrictions or approval rules, so
// they come here. A failing test prints the end of the server log.
func Start(t *testing.T) *Harness {
	t.Helper()
	h := start(t, true)
	h.restrict()
	h.approvalRules()
	return h
}

// StartEmpty is an install nobody has set up: the fixture DocTypes' tables, no
// rows, no users.
func StartEmpty(t *testing.T) *Harness {
	t.Helper()
	return start(t, false)
}

func start(t *testing.T, seed bool) *Harness {
	t.Helper()
	printtest.RegisterFixtureViews(print.Default)
	h := &Harness{t: t, Clock: testkit.Clock(), logs: &lockedBuffer{}}
	a, err := app.New(context.Background(), app.Config{
		DataDir: t.TempDir(), Clock: h.Clock, LogOutput: h.logs,
		Prepare: func(ctx context.Context, d *db.DB) error {
			if err := db.Migrate(ctx, d, fixture.MigrationsFS, "goose_fixture"); err != nil {
				return err
			}
			if !seed {
				return nil
			}
			h.Fx = testkit.Seed(t, d)
			h.M = fixture.SeedMasters(t, d)
			return setPasswords(ctx, d)
		},
	})
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	h.App = a
	srv := httptest.NewServer(h.handler(a.Handler()))
	h.URL = srv.URL
	t.Cleanup(func() {
		srv.Close()
		if err := a.Close(); err != nil {
			t.Errorf("app.Close: %v", err)
		}
		if t.Failed() {
			t.Logf("end of the server log:\n%s", h.logs.tail())
		}
	})
	return h
}

// setPasswords gives every fixture user the fixture password, with no forced
// change at the first sign-in.
func setPasswords(ctx context.Context, d *db.DB) error {
	hash, err := auth.Hash(password)
	if err != nil {
		return err
	}
	return d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE users SET password_hash = ?, must_change_password = 0`, hash)
		return err
	})
}

// restrict applies 00-fixtures.md's restrictions. The fixture DocTypes link to
// the stand-in projects, so Priya's id is the stand-in PRJ-METRO's.
func (h *Harness) restrict() {
	h.t.Helper()
	admin := h.Principal("admin@vardhan.test")
	ctx := context.Background()
	if err := h.App.Auth().ChangeRestrictions(ctx, admin, h.Principal("priya@vardhan.test").ID, auth.DimProject,
		[]int64{h.M.Project["PRJ-METRO"]}, nil, nil); err != nil {
		h.t.Fatalf("restrict Priya: %v", err)
	}
	var sites []int64
	for _, s := range h.Fx.Sites {
		if s.Code == "PUNE-WH" || s.Code == "METRO-SITE" {
			sites = append(sites, s.ID)
		}
	}
	if err := h.App.Auth().ChangeRestrictions(ctx, admin, h.Principal("suresh@vardhan.test").ID, auth.DimSite, sites, nil, nil); err != nil {
		h.t.Fatalf("restrict Suresh: %v", err)
	}
}

// approvalRules creates 00-fixtures.md's rules on the registered DocTypes.
func (h *Harness) approvalRules() {
	h.t.Helper()
	admin := h.Principal("admin@vardhan.test")
	ctx := auth.WithPrincipal(context.Background(), admin) // the app's role directory reads it, as under the rule form
	for _, r := range h.Fx.ApprovalRules {
		if _, ok := doctype.Lookup(r.DocType); !ok {
			continue // "Purchase Order" and "Expense Claim" arrive with the real DocTypes
		}
		in := approval.RuleInput{DocType: r.DocType, FromAmount: r.FromAmount, FallbackRole: r.FallbackRole, AllowSelfApproval: r.SelfApproval}
		if role, ok := strings.CutPrefix(r.Approver, "role "); ok {
			in.ApproverType, in.Role = approval.ApproverRole, role
		} else {
			in.ApproverType = approval.ApproverManager
		}
		if _, err := h.App.Approvals().CreateRule(ctx, admin, in); err != nil {
			h.t.Fatalf("approval rule %+v: %v", r, err)
		}
	}
}

// handler adds what the app can't serve for test-only DocTypes: the stand-in
// masters' pickers, since only real masters have /pick routes; an optional
// delay on one path, to hold a save in flight; and the CSP reports' endpoint.
func (h *Harness) handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == screens.CSPReportPath {
			body, _ := io.ReadAll(r.Body) // a short read is still a report
			h.mu.Lock()
			h.cspReports = append(h.cspReports, string(body))
			h.mu.Unlock()
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if name, ok := strings.CutPrefix(r.URL.Path, fixturePick); ok {
			h.pick(w, r, strings.ToLower(name))
			return
		}
		if p := h.delayPath.Load(); p != nil && r.Method == http.MethodPost && strings.Contains(r.URL.Path, *p) {
			time.Sleep(time.Duration(h.delay.Load()))
		}
		if strings.HasPrefix(r.URL.Path, printPrefix) {
			next.ServeHTTP(w, r) // TestE2E_PrintNoExternal pins print's own policy; the browser still reports
			return
		}
		next.ServeHTTP(&reportCSP{ResponseWriter: w}, r)
	})
}

// DelayPosts holds every POST whose path contains path for d.
func (h *Harness) DelayPosts(path string, d time.Duration) {
	h.delay.Store(int64(d))
	h.delayPath.Store(&path)
}

// pick answers a stand-in master's picker search like mstkit's.
func (h *Harness) pick(w http.ResponseWriter, r *http.Request, name string) {
	var table string
	switch name {
	case "party":
		table = "fixture_party"
	case "item":
		table = "fixture_item"
	case "project":
		table = "fixture_project"
	default:
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query().Get("q")
	like := "%" + q + "%"
	var rows []components.PickRow
	rs, err := h.App.DB().SQL().QueryContext(r.Context(),
		`SELECT id, code, name FROM `+table+` WHERE code LIKE ? OR name LIKE ? ORDER BY name`, like, like) //nolint:gosec // G202: table is one of the three names above
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer func() { _ = rs.Close() }()
	for rs.Next() {
		var row components.PickRow
		if err := rs.Scan(&row.ID, &row.Code, &row.Name); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rows = append(rows, row)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = components.PickerResults(r.Header.Get("HX-Target"), rows, q).Render(r.Context(), w) // the client is gone
}

// Principal is what AUTH loads for the fixture user with this email.
func (h *Harness) Principal(email string) auth.Principal {
	h.t.Helper()
	for _, u := range h.Fx.Users {
		if u.Email == email {
			p, _, err := h.App.Auth().LoadPrincipal(context.Background(), auth.UserID(u.ID))
			if err != nil {
				h.t.Fatalf("LoadPrincipal(%s): %v", email, err)
			}
			return p
		}
	}
	h.t.Fatalf("no fixture user %s", email)
	return auth.Principal{}
}

// Do runs a document action through the real engine, for a test's Given.
func (h *Harness) Do(email, dt string, id int64, a doctype.Action, in doc.ActionInput) doc.Result {
	h.t.Helper()
	res, err := h.App.Engine().Do(context.Background(), h.Principal(email), dt, id, a, in)
	if err != nil {
		h.t.Fatalf("%s %s %d as %s: %v", a, dt, id, email, err)
	}
	return res
}

// Scalar reads one value from the database.
func Scalar[T any](h *Harness, q string, args ...any) T {
	h.t.Helper()
	var v T
	if err := h.App.DB().SQL().QueryRowContext(context.Background(), q, args...).Scan(&v); err != nil {
		h.t.Fatalf("query %q %v: %v", q, args, err)
	}
	return v
}

// Session is one user's own browser at one width.
type Session struct {
	t     *testing.T
	h     *Harness
	ctx   context.Context
	width int64
	Email string
}

// SignIn starts a browser at width and signs in through the real form.
func (h *Harness) SignIn(email string, width int64) *Session {
	h.t.Helper()
	s := h.Browser(email, width)
	s.Open("/signin")
	s.Fill("email", email)
	s.Fill("password", password)
	s.Press("Sign in")
	s.Until("the signed-in landing page", `location.pathname === "/approvals"`)
	return s
}

// Browser starts a browser at width with no session.
func (h *Harness) Browser(email string, width int64) *Session {
	h.t.Helper()
	return &Session{t: h.t, h: h, ctx: uibrowser.New(h.t), width: width, Email: email}
}

// Open loads a path at the session's width.
func (s *Session) Open(path string) {
	s.t.Helper()
	if err := uibrowser.At(s.ctx, s.width, s.h.URL+path); err != nil {
		s.t.Fatal(err)
	}
}

// Do runs steps under one step deadline.
func (s *Session) Do(what string, steps ...chromedp.Action[chromedp.Void]) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepDeadline)
	defer cancel()
	if err := chromedp.Do(ctx, steps...); err != nil {
		s.t.Fatalf("%s: %v\n%s", what, err, s.page())
	}
}

// Eval is expr's value on the current page, under one step deadline.
func Eval[T any](s *Session, expr string, opts ...chromedp.EvaluateOption) T {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepDeadline)
	defer cancel()
	v, err := chromedp.Run(ctx, chromedp.Evaluate[T](expr, opts...))
	if err != nil {
		s.t.Fatalf("evaluate %s: %v", expr, err)
	}
	return v
}

// page describes the current page for a failure message; it never fails.
func (s *Session) page() string {
	ctx, cancel := context.WithTimeout(s.ctx, time.Second)
	defer cancel()
	text, err := chromedp.Run(ctx, chromedp.Evaluate[string](`location.href + "\n" + document.body.innerText`))
	if err != nil {
		return "(no page: " + err.Error() + ")"
	}
	return text[:min(len(text), pageLogLimit)]
}

// Until waits for expr, a boolean expression, to be true. It evaluates again
// and again rather than using chromedp.Poll, which can't span a navigation.
func (s *Session) Until(what, expr string) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(s.ctx, stepDeadline)
	defer cancel()
	for {
		ok, err := chromedp.Run(ctx, chromedp.Evaluate[bool](expr))
		if err == nil && ok {
			return
		}
		select {
		case <-ctx.Done():
			s.t.Fatalf("waiting for %s: %v (last error %v)\n%s", what, ctx.Err(), err, s.page())
		case <-time.After(waitEvery):
		}
	}
}

// WaitText waits until the page shows want.
func (s *Session) WaitText(want string) {
	s.t.Helper()
	s.Until(strconv.Quote(want), `document.body.innerText.replace(/\s+/g, " ").includes(`+strconv.Quote(want)+`)`)
}

// WaitGone waits until the page no longer shows text.
func (s *Session) WaitGone(text string) {
	s.t.Helper()
	s.Until("no "+strconv.Quote(text), `!document.body.innerText.replace(/\s+/g, " ").includes(`+strconv.Quote(text)+`)`)
}

// Shows reports whether the page shows text now.
func (s *Session) Shows(text string) bool {
	s.t.Helper()
	return Eval[bool](s, `document.body.innerText.replace(/\s+/g, " ").includes(`+strconv.Quote(text)+`)`)
}

// Fill types into the labelled input of a form field.
func (s *Session) Fill(field, value string) {
	s.t.Helper()
	s.FillID(components.FieldID(field), value)
}

// FillID replaces the text of the input with this id, as a user does.
func (s *Session) FillID(id, value string) {
	s.t.Helper()
	s.Do("fill "+id, chromedp.Evaluate[chromedp.Void](`(() => { const e = document.getElementById("`+id+`"); e.focus(); e.select(); })()`), chromedp.KeyEvent(value))
}

// Pick searches a Picker and chooses the first row.
func (s *Session) Pick(field, query string) {
	s.t.Helper()
	list := components.FieldID(field) + "-list"
	input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
	option := chromedp.CSS("#" + list + ` [role=option]:not([aria-disabled=true])`)
	s.Do("pick "+field, chromedp.Click(input), chromedp.SendKeys(input, query))
	s.Until("the "+field+" row", `document.querySelector("#`+list+` [role=option] .picker__code")?.textContent === `+strconv.Quote(query))
	s.Do("choose "+field, chromedp.Click(option))
	s.Until(field+" chosen", `document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-value]").value !== ""`)
}

// Press clicks the visible button or link with exactly this text. The same
// link can sit in the hidden phone navigation too, so script finds it. It clicks
// by position because chromedp's own node tree goes stale on a page restored
// from the back/forward cache, and a Click on it then never ends.
func (s *Session) Press(label string) {
	s.t.Helper()
	find := `(() => {
  const m = [...document.querySelectorAll("button, a")].find((e) =>
    e.checkVisibility() && !e.disabled && e.textContent.replace(/\s+/g, " ").trim() === ` + strconv.Quote(label) + `);
  if (!m) return [];
  m.scrollIntoView({block: "center", behavior: "instant"});
  const r = m.getBoundingClientRect();
  return [r.x + r.width / 2, r.y + r.height / 2];
})()`
	s.Until("a button "+strconv.Quote(label), `(`+find+`).length === 2`)
	at := Eval[[]float64](s, find)
	if len(at) != 2 {
		s.t.Fatalf("button %q went away\n%s", label, s.page())
	}
	s.Do("press "+label, chromedp.MouseClickXY(at[0], at[1]))
}

// SetFile picks path on the file input sel selects, as a user's choice does
// (the input's change event fires). Like a user it waits for the page to
// settle (htmx processes swapped-in forms when the swap settles) and brings
// the control into view first. It finds the input by script, because
// chromedp's own node tree is stale after an htmx swap (see Press).
func (s *Session) SetFile(sel, path string) {
	s.t.Helper()
	s.Until("the page to settle", `document.readyState === "complete" && !document.querySelector(".htmx-request, .htmx-swapping, .htmx-settling")`)
	s.Do("set a file on "+sel, chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		find := `(() => { const e = document.querySelector(` + strconv.Quote(sel) + `); e?.scrollIntoView({block: "center", behavior: "instant"}); return e; })()`
		res, err := chromedp.Call(ctx, runtime.Evaluate, runtime.EvaluateParams{Expression: find})
		if err != nil {
			return err
		}
		if res.Result == nil || res.Result.ObjectID == "" {
			return fmt.Errorf("no element %s", sel)
		}
		_, err = chromedp.Call(ctx, dom.SetFileInputFiles, dom.SetFileInputFilesParams{Files: []string{path}, ObjectID: res.Result.ObjectID})
		return err
	}))
}

// Back is the browser's Back button. chromedp.NavigateBack waits for a load
// event, and going back over an htmx step fires none when the entry is kept.
func (s *Session) Back() {
	s.t.Helper()
	s.Do("browser Back", chromedp.Evaluate[chromedp.Void](`history.back()`))
}

// Tab presses the Tab key.
func (s *Session) Tab() {
	s.t.Helper()
	s.Do("press Tab", chromedp.KeyEvent(kb.Tab))
}

// Keys types text into whatever has focus.
func (s *Session) Keys(text string) {
	s.t.Helper()
	s.Do("type "+text, chromedp.KeyEvent(text))
}

var docID = regexp.MustCompile(`^/doc/[^/]+/([0-9]+)(/|$)`)

// DocID is the document id in the current URL.
func (s *Session) DocID() int64 {
	s.t.Helper()
	path := Eval[string](s, `location.pathname`)
	m := docID.FindStringSubmatch(path)
	if m == nil {
		s.t.Fatalf("no document id in %s", path)
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// DocPath is one document's page.
func DocPath(dt string, id int64) string {
	return fmt.Sprintf("/doc/%s/%d", dt, id)
}

// SecondTab is a new tab in the same browser, so it shares s's sign-in.
func (s *Session) SecondTab() *Session {
	s.t.Helper()
	return s.tab()
}

// tab is a session on a tab of s's browser, by default a new one. A tab lives
// as long as the context of its first run, so that run gets no timeout
// context; it is waited for up to stepDeadline instead (CD-248).
func (s *Session) tab(opts ...chromedp.ContextOption) *Session {
	s.t.Helper()
	ctx, cancel := chromedp.NewContext(s.ctx, opts...)
	s.t.Cleanup(cancel)
	opened := make(chan error, 1)
	go func() { opened <- chromedp.Do(ctx) }()
	select {
	case err := <-opened:
		if err != nil {
			s.t.Fatalf("open a tab: %v", err)
		}
	case <-time.After(stepDeadline):
		cancel()
		s.t.Fatalf("open a tab: no answer in %s", stepDeadline)
	}
	return &Session{t: s.t, h: s.h, ctx: ctx, width: s.width, Email: s.Email}
}

// Front brings the tab to the front: a tab behind another takes no key events.
func (s *Session) Front() {
	s.t.Helper()
	s.Do("bring the tab to the front", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, page.BringToFront, cdp.Empty{})
		return err
	}))
}

// Upload posts one file to path from the page, as a multipart fetch with the
// page's CSRF header, and returns the status.
func (s *Session) Upload(path, name string, data []byte) int {
	s.t.Helper()
	return Eval[int](s, `(async () => {
  const bytes = Uint8Array.from(atob("`+base64.StdEncoding.EncodeToString(data)+`"), (c) => c.charCodeAt(0));
  const body = new FormData();
  body.append("file", new File([bytes], `+strconv.Quote(name)+`));
  const headers = JSON.parse(document.body.getAttribute("hx-headers"));
  return (await fetch(`+strconv.Quote(path)+`, {method: "POST", body, headers})).status;
})()`, chromedp.EvalAwaitPromise)
}

// reportCSP adds report-uri to the policy of every response but print's, as an
// e2e build does (plan Task 5). It works at the moment the header goes out because
// platform/web sets the policy itself.
type reportCSP struct {
	http.ResponseWriter
	done bool
}

func (w *reportCSP) add() {
	if h := w.Header(); !w.done && h.Get(cspHeader) != "" {
		h.Set(cspHeader, h.Get(cspHeader)+"; report-uri "+screens.CSPReportPath)
	}
	w.done = true
}

func (w *reportCSP) WriteHeader(code int) {
	w.add()
	w.ResponseWriter.WriteHeader(code)
}

func (w *reportCSP) Write(b []byte) (int, error) {
	w.add()
	return w.ResponseWriter.Write(b)
}

func (w *reportCSP) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// CSPReports returns the reports received so far.
func (h *Harness) CSPReports() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.cspReports)
}
