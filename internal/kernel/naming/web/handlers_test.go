package namingweb

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Fixture DocTypes and series, as in the naming package's Task 6 harness.
const (
	testPO  = "fixture.Order"
	testGRN = "fixture.Receipt"
	testEXP = "fixture.Claim"
)

var testSeries = []struct{ doctype, label, tpl string }{
	{testPO, "Purchase Order", "PO/{FY}/{#####}"},
	{testGRN, "GRN", "GR/{FY}/{#####}"},
	{testEXP, "Expense Claim", "EX/{FY}/{#####}"},
}

// Fixture users (00-fixtures.md).
var (
	testAnil  = auth.Principal{ID: 1, Name: "Anil Kapoor", Roles: []string{"Administrator"}}
	testPriya = auth.Principal{ID: 4, Name: "Priya Shah", Roles: []string{"Purchase User", "Employee"}}
)

const (
	msgForbiddenText = "You don't have permission to change document numbering. Ask an Administrator."
	msgNotFoundText  = "This record doesn't exist or you don't have access to it." // CD-76
	testCSRF         = "nam-18-csrf-token"                                         //nolint:gosec // G101 false positive: a fixed test value for the shell's CSRF field
	testUserHeader   = "X-Nam18-User"
)

// testNow is 2026-10-24T05:00Z (24-10-2026 10:30 IST), FY 26-27.
var testNow = time.Date(2026, time.October, 24, 5, 0, 0, 0, time.UTC)

type screenEnv struct {
	t      *testing.T
	nm     *naming.Namer
	srv    *httptest.Server
	client *http.Client
}

type testResponse struct {
	code     int
	location string
	doc      *goquery.Document
	raw      string
}

// newScreenEnv mounts the screen on a real Namer over a migrated test DB,
// seeded with the three fixture series. Requests carry the principal named
// by testUserHeader (auth.WithPrincipal), a shell with testCSRF, and an scs
// session reachable by web.Flash (CD-80, CD-150). The guard is a stub that
// lets Administrators through (authfake HasRole) and refuses others with msg.
func newScreenEnv(t *testing.T, now time.Time) *screenEnv {
	t.Helper()
	ctx := context.Background()
	roles := authfake.New()
	roles.AddRole(testAnil.ID, "Administrator")
	roles.AddRole(testPriya.ID, "Purchase User")
	roles.AddRole(testPriya.ID, "Employee")
	clk := clock.NewFixed(now)
	nm := naming.New(testkit.NewDB(t), clk, roles, auditfake.New())
	for _, s := range testSeries {
		if err := nm.EnsureSeries(ctx, s.doctype, s.label, s.tpl); err != nil {
			t.Fatalf("EnsureSeries(%s, %q, %q): %v", s.doctype, s.label, s.tpl, err)
		}
	}

	guard := Guard(func(msg string, h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.PrincipalFrom(r.Context())
			if ok {
				isAdmin, err := roles.HasRole(r.Context(), p.ID, "Administrator")
				if err != nil {
					web.ErrorFrom(w, r, err)
					return
				}
				if isAdmin {
					h.ServeHTTP(w, r)
					return
				}
			}
			web.Error(w, r, http.StatusForbidden, msg)
		})
	})

	r := chi.NewRouter()
	Mount(r, Deps{Svc: nm, Admin: nm, Clock: clk}, guard)

	users := map[string]auth.Principal{"anil": testAnil, "priya": testPriya}
	sm := scs.New()
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		rctx := web.WithSessions(req.Context(), sm)
		if p, ok := users[req.Header.Get(testUserHeader)]; ok {
			rctx = auth.WithPrincipal(rctx, p)
		}
		rctx = ui.WithShell(rctx, ui.Shell{CSRF: testCSRF, SignedIn: true})
		r.ServeHTTP(w, req.WithContext(rctx))
	}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &screenEnv{
		t:   t,
		nm:  nm,
		srv: srv,
		client: &http.Client{
			Jar:           jar,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// do sends one request as user ("anil" or "priya"). A non-nil form is posted
// form-encoded; hx adds HX-Request: true.
func (e *screenEnv) do(user, method, path string, form url.Values, hx bool) testResponse {
	e.t.Helper()
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(context.Background(), method, e.srv.URL+path, body)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	req.Header.Set(testUserHeader, user)
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s as %s: %v", method, path, user, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		e.t.Fatalf("%s %s as %s: read body: %v", method, path, user, err)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		e.t.Fatalf("%s %s as %s: parse body: %v", method, path, user, err)
	}
	return testResponse{code: resp.StatusCode, location: resp.Header.Get("Location"), doc: doc, raw: string(raw)}
}

func (e *screenEnv) get(user, path string) testResponse {
	e.t.Helper()
	return e.do(user, http.MethodGet, path, nil, false)
}

func (e *screenEnv) post(user, path string, form url.Values) testResponse {
	e.t.Helper()
	return e.do(user, http.MethodPost, path, form, false)
}

// series returns the stored SeriesInfo for dt, read through Admin.List.
func (e *screenEnv) series(dt string) naming.SeriesInfo {
	e.t.Helper()
	list, err := e.nm.List(context.Background())
	if err != nil {
		e.t.Fatalf("List: %v", err)
	}
	for _, s := range list {
		if s.DocType == dt {
			return s
		}
	}
	e.t.Fatalf("List has no series for %s", dt)
	return naming.SeriesInfo{}
}

func (e *screenEnv) presetPO(last int64) {
	e.t.Helper()
	if err := e.nm.SetCounter(context.Background(), testAnil, testPO, "PO/26-27/", last); err != nil {
		e.t.Fatalf("SetCounter(anil, PO, PO/26-27/, %d): %v", last, err)
	}
}

// testNorm collapses runs of whitespace so text split across elements compares.
func testNorm(s string) string { return strings.Join(strings.Fields(s), " ") }

func testPageText(r testResponse) string { return testNorm(r.doc.Text()) }

func wantStatus(t *testing.T, call string, r testResponse, want int) {
	t.Helper()
	if r.code != want {
		t.Fatalf("%s: status %d, want %d; page text %q", call, r.code, want, testPageText(r))
	}
}

// wantAlert checks the testResponse's role="alert" text contains msg.
func wantAlert(t *testing.T, call string, r testResponse, msg string) {
	t.Helper()
	if got := testNorm(r.doc.Find(`[role="alert"]`).Text()); !strings.Contains(got, msg) {
		t.Errorf("%s: alert text %q does not contain %q", call, got, msg)
	}
}

// wantFieldError checks the 422 re-render: an ErrorSummary link to
// #FieldID(field) with msg, and that id is the input named field, holding
// the posted value.
func wantFieldError(t *testing.T, call string, r testResponse, field, msg, posted string) {
	t.Helper()
	id := components.FieldID(field)
	link := r.doc.Find(`.error-summary a[href="#` + id + `"]`)
	if link.Length() != 1 {
		t.Errorf("%s: %d ErrorSummary links to #%s, want 1; summary text %q",
			call, link.Length(), id, testNorm(r.doc.Find(".error-summary").Text()))
	} else if got := testNorm(link.Text()); got != msg {
		t.Errorf("%s: ErrorSummary link text %q, want %q", call, got, msg)
	}
	in := r.doc.Find(`input[name="` + field + `"]`)
	if in.Length() != 1 {
		t.Errorf("%s: %d inputs named %q, want 1", call, in.Length(), field)
		return
	}
	if got, _ := in.Attr("id"); got != id {
		t.Errorf("%s: input %q id = %q, want %q (the ErrorSummary target)", call, field, got, id)
	}
	if got, _ := in.Attr("value"); got != posted {
		t.Errorf("%s: input %q value = %q, want the posted %q", call, field, got, posted)
	}
	if got := r.doc.Find("title").Text(); !strings.HasPrefix(got, "Error: ") {
		t.Errorf("%s: page title %q, want the \"Error: \" prefix (design §7 ErrorSummary)", call, got)
	}
}

func testEditPath(dt string) string { return "/settings/numbering/" + dt }

// TestNumberingScreen_List supports NAM-051 (its e2e is WP3's,
// TestE2E_NamingSettingsList).
func TestNumberingScreen_List(t *testing.T) {
	t.Run("rows_in_label_order", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		e.presetPO(7)
		r := e.get("anil", "/settings/numbering")
		wantStatus(t, "admin GET /settings/numbering", r, http.StatusOK)

		rows := []struct{ dt, text string }{
			{testEXP, "Expense Claim · EX/{FY}/{#####} · Next: EX/26-27/00001"},
			{testGRN, "GRN · GR/{FY}/{#####} · Next: GR/26-27/00001"},
			{testPO, "Purchase Order · PO/{FY}/{#####} · Next: PO/26-27/00008"},
		}
		txt := testPageText(r)
		prev := -1
		for _, row := range rows {
			i := strings.Index(txt, row.text)
			if i < 0 {
				t.Errorf("list text missing %q; page text %q", row.text, txt)
				continue
			}
			if i < prev {
				t.Errorf("row %q out of label order", row.text)
			}
			prev = i
			a := r.doc.Find(`a[href="` + testEditPath(row.dt) + `"]`)
			if a.Length() == 0 {
				t.Errorf("no link to %s for row %q", testEditPath(row.dt), row.text)
				continue
			}
			item := a.First().Closest("li")
			if item.Length() == 0 {
				item = a.First()
			}
			if got := testNorm(item.Text()); !strings.Contains(got, row.text) {
				t.Errorf("row linking to %s has text %q, want it to contain %q", testEditPath(row.dt), got, row.text)
			}
		}
		if strings.Contains(r.raw, "<table") {
			t.Errorf("list page contains a <table> (design §7 DataList)")
		}
	})

	// "Today" is the clock's date in Asia/Kolkata: 2027-03-31T20:00Z is
	// 01-04-2027 01:30 IST, so FY 27-28.
	t.Run("today_is_IST", func(t *testing.T) {
		e := newScreenEnv(t, time.Date(2027, time.March, 31, 20, 0, 0, 0, time.UTC))
		r := e.get("anil", "/settings/numbering")
		wantStatus(t, "admin GET /settings/numbering at 2027-03-31T20:00Z", r, http.StatusOK)
		const want = "Purchase Order · PO/{FY}/{#####} · Next: PO/27-28/00001"
		if txt := testPageText(r); !strings.Contains(txt, want) {
			t.Errorf("list text missing %q (IST date); page text %q", want, txt)
		}
	})

	t.Run("non_admin_refused", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		r := e.get("priya", "/settings/numbering")
		wantStatus(t, "priya GET /settings/numbering", r, http.StatusForbidden)
		wantAlert(t, "priya GET /settings/numbering", r, msgForbiddenText)
	})
}

// TestNumberingScreen_Preview supports NAM-062.
func TestNumberingScreen_Preview(t *testing.T) {
	e := newScreenEnv(t, testNow)
	e.presetPO(7)

	path := "/settings/numbering/preview?" + url.Values{"tpl": {"PO/{FY}/{#####}"}}.Encode()
	r := e.do("anil", http.MethodGet, path, nil, true)
	wantStatus(t, "GET preview PO/{FY}/{#####}", r, http.StatusOK)
	const want = "PO/26-27/00008, PO/26-27/00009, PO/26-27/00010"
	if txt := testPageText(r); !strings.Contains(txt, want) {
		t.Errorf("preview text %q does not contain %q", txt, want)
	}
	if n := r.doc.Find("nav, main").Length(); n != 0 {
		t.Errorf("preview is a full page (%d nav/main elements), want an htmx fragment", n)
	}
	if got := e.series(testPO).Counters["PO/26-27/"]; got != 7 {
		t.Errorf("PO/26-27/ counter after preview = %d, want 7", got)
	}

	path = "/settings/numbering/preview?" + url.Values{"tpl": {"PO {FY}/{##}"}}.Encode()
	r = e.do("anil", http.MethodGet, path, nil, true)
	wantStatus(t, "GET preview PO {FY}/{##}", r, http.StatusUnprocessableEntity)
	const msg = "Series can use only letters, digits, '-' and '/'."
	if txt := testPageText(r); !strings.Contains(txt, msg) {
		t.Errorf("preview error text %q does not contain %q", txt, msg)
	}
	if got := e.series(testPO).Counters["PO/26-27/"]; got != 7 {
		t.Errorf("PO/26-27/ counter after refused preview = %d, want 7", got)
	}
}

func TestNumberingScreen_Save(t *testing.T) {
	const saved = "Numbering for Purchase Order saved."

	t.Run("template_saved", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		r := e.post("anil", testEditPath(testPO)+"/template", url.Values{"template": {"PR/{FY}/{#####}"}, "csrf_token": {testCSRF}})
		wantStatus(t, "admin POST template PR/{FY}/{#####}", r, http.StatusSeeOther)
		if r.location != testEditPath(testPO) {
			t.Errorf("Location = %q, want %q", r.location, testEditPath(testPO))
		}
		if got := e.series(testPO).Template; got != "PR/{FY}/{#####}" {
			t.Errorf("PO template = %q, want %q", got, "PR/{FY}/{#####}")
		}

		page := e.get("anil", testEditPath(testPO))
		wantStatus(t, "admin GET edit page after save", page, http.StatusOK)
		if got := testNorm(page.doc.Find("main .banner--success").Text()); !strings.Contains(got, saved) {
			t.Errorf("success banner %q does not contain %q", got, saved)
		}
		if got, _ := page.doc.Find(`input[name="template"]`).Attr("value"); got != "PR/{FY}/{#####}" {
			t.Errorf("Series field value = %q, want the saved %q", got, "PR/{FY}/{#####}")
		}
		if page.doc.Find(`[hx-get^="/settings/numbering/preview"]`).Length() == 0 {
			t.Errorf("edit page has no htmx preview region fed by /settings/numbering/preview")
		}
		forms := page.doc.Find(`form[method="post"], form:not([method])`)
		if forms.Length() < 2 {
			t.Errorf("edit page has %d POST forms, want the series and counter forms", forms.Length())
		}
		forms.Each(func(_ int, f *goquery.Selection) {
			if got, _ := f.Find(`input[name="csrf_token"]`).Attr("value"); got != testCSRF {
				t.Errorf("form %q csrf_token = %q, want the shell's %q", f.AttrOr("action", ""), got, testCSRF)
			}
		})
		if strings.Contains(page.raw, "<table") {
			t.Errorf("edit page contains a <table>")
		}

		again := e.get("anil", testEditPath(testPO))
		if n := again.doc.Find("main .banner--success").Length(); n != 0 {
			t.Errorf("success banner shown again on the second GET (%d)", n)
		}
	})

	t.Run("template_invalid", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		r := e.post("anil", testEditPath(testPO)+"/template", url.Values{"template": {"PO/{FY}/{##}"}, "csrf_token": {testCSRF}})
		wantStatus(t, "admin POST template PO/{FY}/{##}", r, http.StatusUnprocessableEntity)
		wantFieldError(t, "admin POST template PO/{FY}/{##}", r, "template",
			"The number part must have 3 to 6 digits.", "PO/{FY}/{##}")
		if got := e.series(testPO).Template; got != "PO/{FY}/{#####}" {
			t.Errorf("PO template = %q, want unchanged %q", got, "PO/{FY}/{#####}")
		}
	})

	t.Run("counter_invalid", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		e.presetPO(7)
		r := e.post("anil", testEditPath(testPO)+"/counter", url.Values{"prefix": {"PO/26-27/"}, "last": {"12a"}, "csrf_token": {testCSRF}})
		wantStatus(t, "admin POST counter last=12a", r, http.StatusUnprocessableEntity)
		wantFieldError(t, "admin POST counter last=12a", r, "last",
			"Last number used must be a whole number from 0 to 99999.", "12a")
		if got, _ := r.doc.Find(`input[name="prefix"]`).Attr("value"); got != "PO/26-27/" {
			t.Errorf("Prefix field value = %q, want the posted %q", got, "PO/26-27/")
		}
		if got := e.series(testPO).Counters["PO/26-27/"]; got != 7 {
			t.Errorf("PO/26-27/ counter = %d, want unchanged 7", got)
		}
	})

	t.Run("counter_saved", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		page := e.get("anil", testEditPath(testPO))
		wantStatus(t, "admin GET edit page", page, http.StatusOK)
		if got, _ := page.doc.Find(`input[name="prefix"]`).Attr("value"); got != "PO/26-27/" {
			t.Errorf("Prefix field value = %q, want today's prefix %q", got, "PO/26-27/")
		}

		r := e.post("anil", testEditPath(testPO)+"/counter", url.Values{"prefix": {"PO/26-27/"}, "last": {"12"}, "csrf_token": {testCSRF}})
		wantStatus(t, "admin POST counter last=12", r, http.StatusSeeOther)
		if r.location != testEditPath(testPO) {
			t.Errorf("Location = %q, want %q", r.location, testEditPath(testPO))
		}
		if got := e.series(testPO).Counters["PO/26-27/"]; got != 12 {
			t.Errorf("PO/26-27/ counter = %d, want 12", got)
		}
		next := e.get("anil", testEditPath(testPO))
		if got := testNorm(next.doc.Find("main .banner--success").Text()); !strings.Contains(got, saved) {
			t.Errorf("success banner %q does not contain %q", got, saved)
		}
	})

	t.Run("non_admin_refused", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		r := e.post("priya", testEditPath(testPO)+"/template", url.Values{"template": {"PR/{FY}/{#####}"}, "csrf_token": {testCSRF}})
		wantStatus(t, "priya POST template", r, http.StatusForbidden)
		wantAlert(t, "priya POST template", r, msgForbiddenText)
		if got := e.series(testPO).Template; got != "PO/{FY}/{#####}" {
			t.Errorf("PO template after refused POST = %q, want unchanged", got)
		}

		// Every route declares the Admin requirement.
		for _, c := range []struct{ method, path string }{
			{http.MethodGet, testEditPath(testPO)},
			{http.MethodGet, "/settings/numbering/preview?" + url.Values{"tpl": {"PO/{FY}/{#####}"}}.Encode()},
			{http.MethodPost, testEditPath(testPO) + "/counter"},
		} {
			var form url.Values
			if c.method == http.MethodPost {
				form = url.Values{"prefix": {"PO/26-27/"}, "last": {"12"}, "csrf_token": {testCSRF}}
			}
			r := e.do("priya", c.method, c.path, form, false)
			call := "priya " + c.method + " " + c.path
			wantStatus(t, call, r, http.StatusForbidden)
			wantAlert(t, call, r, msgForbiddenText)
		}
	})

	t.Run("unknown_doctype", func(t *testing.T) {
		e := newScreenEnv(t, testNow)
		r := e.get("anil", testEditPath("fixture.Unknown"))
		wantStatus(t, "admin GET /settings/numbering/fixture.Unknown", r, http.StatusNotFound)
		wantAlert(t, "admin GET /settings/numbering/fixture.Unknown", r, msgNotFoundText)
	})
}
