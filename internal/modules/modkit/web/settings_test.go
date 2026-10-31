package modkitweb_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	modkitweb "github.com/vardhan/internaltools/internal/modules/modkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// The page (M0-7 AD rulings): GET and POST /settings/business, titled
// "Business settings", with the fields locked_up_to (dd-mm-yyyy),
// default_po_terms and the page's version (CD-94).
const (
	settingsPath = "/settings/business"
	pageTitle    = "Business settings"

	msgAdminOnly = "Only an administrator can change business settings."
	msgStale     = "Someone else changed Business settings after you opened it. Reload it and make your changes again."

	adminEmail  = "admin@vardhan.test"
	deepakEmail = "deepak@vardhan.test"

	testCSRF   = "m07-csrf-token" //nolint:gosec // G101 false positive: a fixed test value for the shell's CSRF field
	userHeader = "X-M07-User"

	terms = "Payment within 30 days of the invoice."
)

var mar31 = types.Date{Year: 2026, Month: 3, Day: 31}

type screen struct {
	t      *testing.T
	db     *db.DB
	kit    *modkit.Kit
	rec    *auditfake.Recorder
	srv    *httptest.Server
	client *http.Client
}

type page struct {
	code     int
	location string
	doc      *goquery.Document
}

// newScreen mounts modkitweb.Routes on the canonical fixtures with the real
// auth.Service. Requests carry the principal of the fixture user named in
// userHeader (auth.WithPrincipal), a shell with testCSRF, and an scs session
// reachable by web.Flash (CD-80, CD-150). No guard: the routes are mounted
// behind the signed-in guard only (M0-6), so the page itself refuses
// non-Administrators.
func newScreen(t *testing.T) *screen {
	t.Helper()
	d := testkit.NewDB(t)
	testkit.Seed(t, d)
	a := auth.New(auth.Deps{DB: d, Clock: testkit.Clock()})
	rec := auditfake.New()
	deps := modkitweb.Deps{DB: d, Auth: a, Audit: rec, Clock: testkit.Clock()}

	r := chi.NewRouter()
	modkitweb.Routes(deps)(r)
	sm := scs.New()
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ctx := web.WithSessions(req.Context(), sm)
		if email := req.Header.Get(userHeader); email != "" {
			ctx = auth.WithPrincipal(ctx, testkit.Principal(t, email))
		}
		ctx = ui.WithShell(ctx, ui.Shell{CSRF: testCSRF, SignedIn: true})
		r.ServeHTTP(w, req.WithContext(ctx))
	}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &screen{
		t: t, db: d, rec: rec, srv: srv,
		kit: modkit.New(modkit.Deps{DB: d, Auth: a, Audit: auditfake.New(), Clock: testkit.Clock()}),
		client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (s *screen) do(email, method string, form url.Values) page {
	s.t.Helper()
	var body io.Reader = strings.NewReader("")
	if form != nil {
		form.Set("csrf_token", testCSRF)
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(context.Background(), method, s.srv.URL+settingsPath, body)
	if err != nil {
		s.t.Fatal(err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	req.Header.Set(userHeader, email)
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s as %s: %v", method, settingsPath, email, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		s.t.Fatal(err)
	}
	return page{code: resp.StatusCode, location: resp.Header.Get("Location"), doc: doc}
}

func (s *screen) get(email string) page {
	s.t.Helper()
	return s.do(email, http.MethodGet, nil)
}

func (s *screen) post(email string, form url.Values) page {
	s.t.Helper()
	return s.do(email, http.MethodPost, form)
}

// settings reads the stored settings as the Administrator.
func (s *screen) settings() modkit.Settings {
	s.t.Helper()
	got, err := s.kit.GetSettings(context.Background(), testkit.Principal(s.t, adminEmail))
	if err != nil {
		s.t.Fatalf("GetSettings: %v", err)
	}
	return got
}

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

func (p page) text() string { return norm(p.doc.Text()) }

// value is the posted value a form field shows: an input's value attribute or
// a textarea's text.
func (p page) value(t *testing.T, name string) string {
	t.Helper()
	sel := p.doc.Find(`[name="` + name + `"]`)
	if sel.Length() != 1 {
		t.Errorf("%d fields named %q, want 1", sel.Length(), name)
		return ""
	}
	if goquery.NodeName(sel) == "textarea" {
		return sel.Text()
	}
	v, _ := sel.Attr("value")
	return v
}

func wantStatus(t *testing.T, call string, p page, want int) {
	t.Helper()
	if p.code != want {
		t.Fatalf("%s: status %d, want %d; page text %q", call, p.code, want, p.text())
	}
}

func wantFields(t *testing.T, call string, p page, want map[string]string) {
	t.Helper()
	for name, v := range want {
		if got := p.value(t, name); got != v {
			t.Errorf("%s: field %s = %q, want %q", call, name, got, v)
		}
	}
}

func form(version, lockedUpTo, poTerms string) url.Values {
	return url.Values{"version": {version}, "locked_up_to": {lockedUpTo}, "default_po_terms": {poTerms}}
}

// PO-039 on the page: a non-Administrator gets 403 with the settings text on
// GET and POST, and the POST changes nothing.
func TestSettingsPage_NonAdminRefused(t *testing.T) {
	s := newScreen(t)

	for _, p := range []struct {
		call string
		resp page
	}{
		{"GET as Deepak", s.get(deepakEmail)},
		{"POST as Deepak", s.post(deepakEmail, form("1", "31-03-2026", terms))},
	} {
		wantStatus(t, p.call, p.resp, http.StatusForbidden)
		if got := norm(p.resp.doc.Find(`[role="alert"]`).Text()); got != msgAdminOnly {
			t.Errorf("%s: alert %q, want %q", p.call, got, msgAdminOnly)
		}
	}
	if got := s.settings(); got != (modkit.Settings{Version: 1}) {
		t.Errorf("settings after the refused POST = %+v, want the seeded row at version 1", got)
	}
	if n := len(s.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after the refused POST, want 0", n)
	}
}

// The Administrator's page shows the stored values and version; a save
// redirects back to the page with a success flash, is stored, and is audited
// through Deps.Audit.
func TestSettingsPage_Save(t *testing.T) {
	s := newScreen(t)

	open := s.get(adminEmail)
	wantStatus(t, "GET as Administrator", open, http.StatusOK)
	if got := norm(open.doc.Find("h1").First().Text()); got != pageTitle {
		t.Errorf("page heading %q, want %q", got, pageTitle)
	}
	wantFields(t, "new page", open, map[string]string{"version": "1", "locked_up_to": "", "default_po_terms": ""})
	if got, _ := open.doc.Find(`form input[name="csrf_token"]`).Attr("value"); got != testCSRF {
		t.Errorf("form csrf_token = %q, want the shell's %q", got, testCSRF)
	}

	saved := s.post(adminEmail, form("1", "31-03-2026", terms))
	wantStatus(t, "POST as Administrator", saved, http.StatusSeeOther)
	if saved.location != settingsPath {
		t.Errorf("Location = %q, want %q", saved.location, settingsPath)
	}
	want := modkit.Settings{LockedUpTo: sql.Null[types.Date]{V: mar31, Valid: true}, DefaultPOTerms: terms, Version: 2}
	if got := s.settings(); got != want {
		t.Errorf("stored settings %+v, want %+v", got, want)
	}
	if n := len(s.rec.Entries()); n != 1 {
		t.Errorf("%d audit entries after the save, want 1", n)
	}

	back := s.get(adminEmail)
	wantStatus(t, "GET after the save", back, http.StatusOK)
	if got := norm(back.doc.Find("main .banner--success").Text()); got == "" {
		t.Errorf("no success flash on the page after the save; page text %q", back.text())
	}
	wantFields(t, "page after the save", back, map[string]string{"version": "2", "locked_up_to": "31-03-2026", "default_po_terms": terms})
	if n := s.get(adminEmail).doc.Find("main .banner--success").Length(); n != 0 {
		t.Errorf("success flash shown again on the next GET (%d)", n)
	}
}

// A save from a page opened before someone else saved is 409 with DOC's
// stale text; the form keeps what was typed and nothing is stored (CD-94).
func TestSettingsPage_StaleKeepsTypedValues(t *testing.T) {
	s := newScreen(t)
	wantStatus(t, "first tab's save", s.post(adminEmail, form("1", "31-03-2026", terms)), http.StatusSeeOther)
	stored := s.settings()

	const typedTerms = "Typed in the second tab."
	stale := s.post(adminEmail, form("1", "30-06-2026", typedTerms))
	wantStatus(t, "second tab's save from version 1", stale, http.StatusConflict)
	if !strings.Contains(stale.text(), msgStale) {
		t.Errorf("409 page text %q does not contain %q", stale.text(), msgStale)
	}
	wantFields(t, "409 page", stale, map[string]string{"locked_up_to": "30-06-2026", "default_po_terms": typedTerms})
	if got := s.settings(); got != stored {
		t.Errorf("stored settings after the stale save %+v, want the first tab's %+v", got, stored)
	}
	if n := len(s.rec.Entries()); n != 1 {
		t.Errorf("%d audit entries, want 1 (the first tab's save only)", n)
	}
}

// CD-146, CD-183: a save whose version is missing or not a number is 400 with
// AUTH's errBadForm text (as company's single-record form, via mstkit/web),
// and stores and audits nothing.
func TestSettingsPage_MissingOrBadVersionIs400(t *testing.T) {
	s := newScreen(t)
	missing := form("", "31-03-2026", terms)
	missing.Del("version")
	for _, c := range []struct {
		name string
		form url.Values
	}{
		{"missing version", missing},
		{"non-numeric version", form("abc", "31-03-2026", terms)},
	} {
		resp := s.post(adminEmail, c.form)
		wantStatus(t, "POST with "+c.name, resp, http.StatusBadRequest)
		if got := norm(resp.doc.Find(`[role="alert"]`).Text()); got != auth.ErrBadForm.Error() {
			t.Errorf("POST with %s: alert %q, want %q", c.name, got, auth.ErrBadForm.Error())
		}
	}
	if got := s.settings(); got != (modkit.Settings{Version: 1}) {
		t.Errorf("settings after the bad POSTs = %+v, want the seeded row at version 1", got)
	}
	if n := len(s.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after the bad POSTs, want 0", n)
	}
}

// A date the date parser refuses is 422 with its message on the field (in the
// error summary, linked to the input), the typed values kept, and nothing
// stored or audited.
func TestSettingsPage_BadDateIs422(t *testing.T) {
	const (
		badDate    = "31-13-2026"
		msgBadDate = "31-13-2026 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025." // types.ParseDate
	)
	s := newScreen(t)
	resp := s.post(adminEmail, form("1", badDate, terms))
	wantStatus(t, "POST with locked_up_to "+badDate, resp, http.StatusUnprocessableEntity)
	id := components.FieldID("locked_up_to")
	link := resp.doc.Find(`.error-summary a[href="#` + id + `"]`)
	if link.Length() != 1 || norm(link.Text()) != msgBadDate {
		t.Errorf("error summary links to #%s: %d, text %q; want 1 with %q", id, link.Length(), norm(link.Text()), msgBadDate)
	}
	if got, _ := resp.doc.Find(`input[name="locked_up_to"]`).Attr("id"); got != id {
		t.Errorf("locked_up_to input id = %q, want %q (the error summary's target)", got, id)
	}
	wantFields(t, "422 page", resp, map[string]string{"version": "1", "locked_up_to": badDate, "default_po_terms": terms})
	if got := s.settings(); got != (modkit.Settings{Version: 1}) {
		t.Errorf("settings after the bad date = %+v, want the seeded row at version 1", got)
	}
	if n := len(s.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after the bad date, want 0", n)
	}
}

// AD ruling (company's unchanged-save rule, CD-208): posting the form with
// the stored values redirects with the success flash and writes nothing: the
// version stays and no audit entry is added.
func TestSettingsPage_UnchangedSaveWritesNothing(t *testing.T) {
	s := newScreen(t)
	wantStatus(t, "first save", s.post(adminEmail, form("1", "31-03-2026", terms)), http.StatusSeeOther)
	s.get(adminEmail) // takes the first save's flash

	again := s.post(adminEmail, form("2", "31-03-2026", terms))
	wantStatus(t, "unchanged save at version 2", again, http.StatusSeeOther)
	if again.location != settingsPath {
		t.Errorf("Location = %q, want %q", again.location, settingsPath)
	}
	back := s.get(adminEmail)
	if got := norm(back.doc.Find("main .banner--success").Text()); got == "" {
		t.Errorf("no success flash after the unchanged save; page text %q", back.text())
	}
	want := modkit.Settings{LockedUpTo: sql.Null[types.Date]{V: mar31, Valid: true}, DefaultPOTerms: terms, Version: 2}
	if got := s.settings(); got != want {
		t.Errorf("stored settings after the unchanged save %+v, want %+v (version unchanged)", got, want)
	}
	if n := len(s.rec.Entries()); n != 1 {
		t.Errorf("%d audit entries, want 1 (the first save only)", n)
	}
}

// The right is checked before the form is read: a non-Administrator posting a
// bad date or no version gets the 403 with the settings refusal, never the
// 422 or 400, and nothing is stored or audited.
func TestSettingsPage_NonAdminRefusedBeforeFormChecks(t *testing.T) {
	s := newScreen(t)
	noVersion := form("", "31-03-2026", terms)
	noVersion.Del("version")
	for _, c := range []struct {
		name string
		form url.Values
	}{
		{"bad date", form("1", "31-13-2026", terms)},
		{"missing version", noVersion},
	} {
		resp := s.post(deepakEmail, c.form)
		wantStatus(t, "POST as Deepak with "+c.name, resp, http.StatusForbidden)
		if got := norm(resp.doc.Find(`[role="alert"]`).Text()); got != msgAdminOnly {
			t.Errorf("POST as Deepak with %s: alert %q, want %q", c.name, got, msgAdminOnly)
		}
	}
	if got := s.settings(); got != (modkit.Settings{Version: 1}) {
		t.Errorf("settings after the refused POSTs = %+v, want the seeded row at version 1", got)
	}
	if n := len(s.rec.Entries()); n != 0 {
		t.Errorf("%d audit entries after the refused POSTs, want 0", n)
	}
}
