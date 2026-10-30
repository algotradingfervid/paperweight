package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const (
	testPassword = "Correct-Horse-42"
	orderPath    = "/doc/" + fixture.Order

	// The canonical users' ids, in creation order (00-fixtures.md).
	adminID  = auth.UserID(1)
	raviID   = auth.UserID(3)
	priyaID  = auth.UserID(4)
	sureshID = auth.UserID(5)
	deepakID = auth.UserID(7)

	adminEmail  = "admin@vardhan.test"
	raviEmail   = "ravi@vardhan.test"
	priyaEmail  = "priya@vardhan.test"
	sureshEmail = "suresh@vardhan.test"
	deepakEmail = "deepak@vardhan.test"
	nobodyEmail = "nobody@vardhan.test"

	msgSessionExpired = "Your session has expired. Sign in again."
	msgSignInRequired = "Sign in to continue."
	msgInboxEmpty     = "Nothing is waiting for your approval."
)

func init() { printtest.RegisterFixtureViews(print.Default) }

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

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// world is one real App on a temp dir behind httptest: real auth.Service and
// services, fixture DocTypes through Config.Prepare.
type world struct {
	t    *testing.T
	app  *app.App
	srv  *httptest.Server
	logs *lockedBuffer
	dir  string
}

// newWorld starts an App with an empty database. A TLS test server backs a
// Secure config, because the session cookie is then Secure.
func newWorld(t *testing.T, mods ...func(*app.Config)) *world {
	t.Helper()
	logs := &lockedBuffer{}
	cfg := app.Config{
		DataDir: t.TempDir(), Addr: "127.0.0.1:0", Clock: testkit.Clock(), LogOutput: logs,
		Prepare: func(ctx context.Context, d *db.DB) error {
			if err := db.Migrate(ctx, d, fixture.MigrationsFS, "goose_fixture"); err != nil {
				return err
			}
			return migrateGRN(ctx, d)
		},
	}
	for _, m := range mods {
		m(&cfg)
	}
	a, err := app.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	srv := httptest.NewUnstartedServer(a.Handler())
	if cfg.Secure {
		srv.StartTLS()
	} else {
		srv.Start()
	}
	t.Cleanup(func() {
		srv.Close()
		if err := a.Close(); err != nil {
			t.Errorf("app.Close: %v", err)
		}
	})
	return &world{t: t, app: a, srv: srv, logs: logs, dir: cfg.DataDir}
}

// routes is the real router behind Handler() for chi.Walk.
func (w *world) routes() chi.Routes {
	w.t.Helper()
	r, ok := w.app.Handler().(chi.Routes)
	if !ok {
		w.t.Fatalf("Handler() is %T, want a chi.Routes so the router can be walked", w.app.Handler())
	}
	return r
}

// canonicalUser is a row of 00-fixtures.md's users table.
type canonicalUser struct {
	email, name string
	roles       []string
}

var canonicalUsers = []canonicalUser{
	{"meena@vardhan.test", "Meena Iyer", []string{"Director"}},
	{raviEmail, "Ravi Kulkarni", []string{"Purchase Manager", "Employee"}},
	{priyaEmail, "Priya Shah", []string{"Purchase User", "Employee"}},
	{sureshEmail, "Suresh Patil", []string{"Stores", "Employee"}},
	{"kavya@vardhan.test", "Kavya Rao", []string{"Masters Manager", "Employee"}},
	{deepakEmail, "Deepak Nair", []string{"Employee"}},
	{"old@vardhan.test", "Former Staff", []string{"Employee"}},
}

// firstAdmin creates Anil through AUTH's own bootstrap and returns him.
func (w *world) firstAdmin() auth.Principal {
	w.t.Helper()
	ctx := context.Background()
	id, err := w.app.Auth().CreateFirstAdmin(ctx, adminEmail, "Anil Kapoor", testPassword)
	if err != nil {
		w.t.Fatalf("CreateFirstAdmin: %v", err)
	}
	return w.principal(id)
}

func (w *world) principal(id auth.UserID) auth.Principal {
	w.t.Helper()
	p, _, err := w.app.Auth().LoadPrincipal(context.Background(), id)
	if err != nil {
		w.t.Fatalf("LoadPrincipal(%d): %v", id, err)
	}
	return p
}

// seedUsers creates the 8 canonical users, ids in fixture order. They go
// through AUTH's admin API because these tests sign in with a password, which
// testkit.Seed doesn't write.
func (w *world) seedUsers() auth.Principal {
	w.t.Helper()
	ctx := context.Background()
	admin := w.firstAdmin()
	for _, u := range canonicalUsers {
		if _, _, err := w.app.Auth().CreateUser(ctx, admin, auth.NewUser{Email: u.email, Name: u.name, Password: testPassword, Roles: u.roles}); err != nil {
			w.t.Fatalf("CreateUser(%s): %v", u.email, err)
		}
	}
	if err := w.app.Auth().DisableUserByEmail(ctx, "old@vardhan.test", "left"); err != nil {
		w.t.Fatalf("disable old@vardhan.test: %v", err)
	}
	return admin
}

// canonical is a world's seed: users, the testkit masters with employees
// linked to the users, the fixture stand-in masters and Priya's restriction
// to PRJ-METRO. It returns the admin, the seed and the stand-in masters' ids.
func (w *world) canonical() (auth.Principal, testkit.Fixtures, fixture.Masters) {
	w.t.Helper()
	admin := w.seedUsers()
	d := w.app.DB()
	fx := testkit.SeedMasters(w.t, d)
	testkit.LinkEmployees(w.t, d, fx)
	m := fixture.SeedMasters(w.t, d)
	err := w.app.Auth().ChangeRestrictions(context.Background(), admin, priyaID, auth.DimProject, []int64{m.Project["PRJ-METRO"]}, nil, nil)
	if err != nil {
		w.t.Fatalf("restrict Priya: %v", err)
	}
	return admin, fx, m
}

// count runs a SELECT count(*).
func (w *world) count(q string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.app.DB().SQL().QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		w.t.Fatalf("query %q: %v", q, err)
	}
	return n
}

func (w *world) text(q string, args ...any) string {
	w.t.Helper()
	var s string
	if err := w.app.DB().SQL().QueryRowContext(context.Background(), q, args...).Scan(&s); err != nil {
		w.t.Fatalf("query %q: %v", q, err)
	}
	return s
}

// response is one answer, its body parsed for page tests (CD-148).
type response struct {
	status int
	header http.Header
	body   string
	doc    *goquery.Document
}

func (r *response) location() string { return r.header.Get("Location") }

// pageText is the page's text with runs of white space collapsed.
func (r *response) pageText() string {
	return strings.Join(strings.Fields(r.doc.Text()), " ")
}

func (r *response) alert() string {
	return strings.TrimSpace(r.doc.Find(`[role="alert"]`).First().Text())
}

// browser is a client with a cookie jar that doesn't follow redirects.
type browser struct {
	w    *world
	c    *http.Client
	csrf string
}

func (w *world) browser() *browser {
	w.t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		w.t.Fatal(err)
	}
	c := *w.srv.Client()
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &browser{w: w, c: &c}
}

func (b *browser) do(method, path string, body io.Reader, contentType string, headers map[string]string) *response {
	b.w.t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, b.w.srv.URL+path, body)
	if err != nil {
		b.w.t.Fatalf("%s %s: %v", method, path, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.c.Do(req)
	if err != nil {
		b.w.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		b.w.t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		b.w.t.Fatalf("%s %s: parse body: %v", method, path, err)
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: string(raw), doc: doc}
}

func (b *browser) get(path string) *response {
	b.w.t.Helper()
	return b.do(http.MethodGet, path, nil, "", nil)
}

// post sends form with the browser's CSRF token.
func (b *browser) post(path string, form url.Values) *response {
	b.w.t.Helper()
	return b.postHeaders(path, form, nil)
}

func (b *browser) postHeaders(path string, form url.Values, headers map[string]string) *response {
	b.w.t.Helper()
	f := url.Values{}
	for k, v := range form {
		f[k] = v
	}
	if b.csrf != "" {
		f.Set("csrf_token", b.csrf)
	}
	return b.do(http.MethodPost, path, strings.NewReader(f.Encode()), "application/x-www-form-urlencoded", headers)
}

// upload posts one "file" part plus fields, with the CSRF token in its header.
func (b *browser) upload(path, name string, data []byte, fields map[string]string) *response {
	b.w.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			b.w.t.Fatal(err)
		}
	}
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		b.w.t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		b.w.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		b.w.t.Fatal(err)
	}
	return b.do(http.MethodPost, path, &buf, mw.FormDataContentType(), map[string]string{"X-CSRF-Token": b.csrf})
}

// pageCSRF reads the token a page's POST forms send, never the page-level
// hidden input outside them: a POST form without a token fails the test.
func (b *browser) pageCSRF(path string) string {
	b.w.t.Helper()
	r := b.get(path)
	forms := r.doc.Find(`form[method="post"]`)
	if forms.Length() == 0 {
		b.w.t.Fatalf("GET %s (status %d): no POST form in the page", path, r.status)
	}
	forms.Each(func(_ int, f *goquery.Selection) {
		if f.Find(`input[name="csrf_token"]`).AttrOr("value", "") == "" {
			b.w.t.Fatalf("GET %s: form %q has no csrf_token", path, f.AttrOr("action", ""))
		}
	})
	return r.doc.Find(`form input[name="csrf_token"]`).First().AttrOr("value", "")
}

// trySignIn posts the real sign-in form and returns the answer.
func (b *browser) trySignIn(email string, headers map[string]string) *response {
	b.w.t.Helper()
	b.csrf = b.pageCSRF("/signin")
	return b.postHeaders("/signin", url.Values{"email": {email}, "password": {testPassword}}, headers)
}

// signIn signs in through POST /signin and picks up the rotated CSRF token.
func (b *browser) signIn(email string) *response {
	b.w.t.Helper()
	r := b.trySignIn(email, nil)
	if r.status != http.StatusSeeOther {
		b.w.t.Fatalf("POST /signin %s = %d (%q), want 303", email, r.status, r.alert())
	}
	b.csrf = b.pageCSRF("/account/password")
	return r
}

func (w *world) signedIn(email string) *browser {
	w.t.Helper()
	b := w.browser()
	b.signIn(email)
	return b
}

var orderIDInPath = regexp.MustCompile(`^` + regexp.QuoteMeta(orderPath) + `/([0-9]+)/`)

// newOrder creates a draft through the real form and returns its id.
func (b *browser) newOrder(m fixture.Masters, project, amount string) int64 {
	b.w.t.Helper()
	r := b.post(orderPath+"/new", url.Values{
		"supplier_id": {strconv.FormatInt(m.Party["SHREE"], 10)},
		"project_id":  {strconv.FormatInt(m.Project[project], 10)},
		"order_date":  {"24-10-2026"}, "amount": {amount}, "priority": {"normal"}, "discount_pct": {"0"},
	})
	if r.status != http.StatusSeeOther {
		b.w.t.Fatalf("POST %s/new = %d (%q), want 303", orderPath, r.status, r.pageText())
	}
	got := orderIDInPath.FindStringSubmatch(r.location())
	if got == nil {
		b.w.t.Fatalf("POST %s/new redirected to %q, want the new draft's step", orderPath, r.location())
	}
	id, err := strconv.ParseInt(got[1], 10, 64)
	if err != nil {
		b.w.t.Fatal(err)
	}
	return id
}

func (w *world) version(id int64) string {
	w.t.Helper()
	return w.text(`SELECT CAST(version AS TEXT) FROM fixture_order WHERE id = ?`, id)
}

func (w *world) state(id int64) string {
	w.t.Helper()
	return w.text(`SELECT state FROM fixture_order WHERE id = ?`, id)
}

func orderURL(id int64) string { return orderPath + "/" + strconv.FormatInt(id, 10) }

// addLine adds the reference PO's first line.
func (b *browser) addLine(m fixture.Masters, id int64) {
	b.w.t.Helper()
	r := b.post(orderURL(id)+"/lines/new", url.Values{
		"version": {b.w.version(id)}, "item_id": {strconv.FormatInt(m.Item["MS-ANGLE-50"], 10)},
		"qty": {"1200.000"}, "rate": {"62.5000"}, "uom": {"kg"},
	})
	if r.status >= http.StatusBadRequest {
		b.w.t.Fatalf("POST line on order %d = %d (%q)", id, r.status, r.pageText())
	}
}

// act posts a document action with the current version.
func (b *browser) act(id int64, action string, extra url.Values) *response {
	b.w.t.Helper()
	f := url.Values{"version": {b.w.version(id)}}
	for k, v := range extra {
		f[k] = v
	}
	r := b.post(orderURL(id)+"/"+action, f)
	if r.status >= http.StatusBadRequest {
		b.w.t.Fatalf("POST %s on order %d = %d (%q)", action, id, r.status, r.pageText())
	}
	return r
}

// purchaseManagerRule adds the seed's ₹50,000 rule through its real screen.
func (b *browser) purchaseManagerRule() {
	b.w.t.Helper()
	r := b.post("/admin/approval-rules", url.Values{
		"doctype": {fixture.Order}, "from_amount": {"50000.00"}, "approver_type": {"role"}, "role": {"Purchase Manager"},
	})
	if r.status >= http.StatusBadRequest {
		b.w.t.Fatalf("POST /admin/approval-rules = %d (%q)", r.status, r.pageText())
	}
}

// pendingOrder is a ₹94,164.00 order by Priya, submitted and waiting for a
// Purchase Manager.
func (w *world) pendingOrder(admin, priya *browser, m fixture.Masters) int64 {
	w.t.Helper()
	admin.purchaseManagerRule()
	id := priya.newOrder(m, "PRJ-METRO", "94164.00")
	priya.addLine(m, id)
	priya.act(id, "submit", nil)
	if got := w.state(id); got != "pending_approval" {
		w.t.Fatalf("order %d state after submit = %q, want pending_approval", id, got)
	}
	return id
}

var errRollback = errors.New("roll back")

// refusal is what the real service says for in, with nothing kept: the text a
// page must show, read from the producing package rather than copied.
func (w *world) refusal(fn func(tx *db.Tx) error) []string {
	w.t.Helper()
	var got error
	err := w.app.DB().Write(context.Background(), func(tx *db.Tx) error {
		got = fn(tx)
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		w.t.Fatalf("rolled-back write: %v", err)
	}
	if got == nil {
		w.t.Fatal("the service accepted the input, want a refusal")
	}
	var he doctype.HTTPError
	if errors.As(got, &he) && len(he.FieldErrors()) > 0 {
		var msgs []string
		for _, f := range he.FieldErrors() {
			msgs = append(msgs, f.Message)
		}
		return msgs
	}
	return []string{got.Error()}
}

var routeParam = regexp.MustCompile(`\{([^}:]+)(:[^}]*)?\}`)

// fillRoute turns a walked pattern into an escaped request path.
func fillRoute(route string, fill func(param string) string) string {
	p := routeParam.ReplaceAllStringFunc(route, func(m string) string {
		return fill(routeParam.FindStringSubmatch(m)[1])
	})
	p = strings.ReplaceAll(p, "*", "x")
	return (&url.URL{Path: p}).EscapedPath()
}

// isPublic is the plan's exception list for TestApp_AllRoutesRequireSignIn.
func isPublic(route string) bool {
	return route == "/signin" || route == "/setup" || route == "/healthz" || strings.HasPrefix(route, "/static/")
}
