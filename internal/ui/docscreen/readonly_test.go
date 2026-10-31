package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// G1 (CD-263, amending design-system §8.3): a draft's page is its check
// page only for a user with write on the record. Anyone else who can read
// it gets the read-only document page; every step and line GET answers 303
// there; their POSTs keep DOC's 403, which comes before the version check
// (CD-259).

// m15HookedSteps are Hooked's wizard slugs (TestModule_HookedShape).
var m15HookedSteps = []string{"details", "terms", "items", "check"}

// m15HookedDenied is AUTH's refusal of a save on Hooked to a user with read
// only (auth/messages.go DeniedError).
const m15HookedDenied = "You don't have permission to change Hooked Order. Ask your administrator for a role that allows it."

// m15Env is the Hooked harness on the real engine and real AUTH with one
// draft on TECH-SITE created by the open user, and a reader whose only
// right on Hooked is read (all). The restricted user (TECH-SITE, write all)
// is a writer who isn't the draft's creator.
type m15Env struct {
	*hookedEnv
	reader auth.Principal
	id     int64
}

func m15NewEnv(t *testing.T) *m15Env {
	t.Helper()
	e := newHookedEnv(t)
	a, ok := e.d.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("hookedEnv's Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	ctx := t.Context()
	if err := a.CreateRole(ctx, e.fx.Admin, "M15 Reader"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(ctx, e.fx.Admin, "M15 Reader", docscreentest.Hooked, map[auth.Right]auth.Scope{"read": auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	reader := m15User(t, a, e.fx.Admin, "m15-reader@vardhan.test", "M15 Reader")
	res, err := e.draft(t, e.open, "TECH-SITE")
	if err != nil {
		t.Fatalf("saving the Hooked draft: %v", err)
	}
	return &m15Env{hookedEnv: e, reader: reader, id: res.ID}
}

// m15User creates a user with roles on the real AUTH and loads them.
func m15User(t *testing.T, a *auth.Auth, admin auth.Principal, email string, roles ...string) auth.Principal {
	t.Helper()
	ctx := t.Context()
	id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: scopePassword, Roles: roles})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", email, err)
	}
	p, _, err := a.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", email, err)
	}
	return p
}

func m15DocURL(dt string, id int64) string { return "/doc/" + dt + "/" + strconv.FormatInt(id, 10) }

// m15Redirect fails unless rec is a 303 to exactly want.
func m15Redirect(t *testing.T, rec *httptest.ResponseRecorder, want, what string) {
	t.Helper()
	if rec.Code != http.StatusSeeOther {
		t.Errorf("%s = %d, want 303 to %s; body %.300s", what, rec.Code, want, rec.Body)
		return
	}
	if got := rec.Header().Get("Location"); got != want {
		t.Errorf("%s redirects to %q, want %q", what, got, want)
	}
}

// m15ReadOnly fails unless rec is draft id's read-only document page: a 200
// with the document layout, the Draft badge, its Details and its lines,
// and nothing that edits it (no wizard, step form, Change link or line
// action). wantLines is the number of line cards.
func m15ReadOnly(t *testing.T, rec *httptest.ResponseRecorder, docURL string, wantLines int, what string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Errorf("%s = %d (Location %q), want 200 with the read-only document page, not the check page", what, rec.Code, rec.Header().Get("Location"))
		return
	}
	page := uitest.Doc(t, rec)
	for sel, why := range map[string]string{
		"h1#page-title":                       "the document page's title",
		".doc-layout":                         "the document layout",
		".doc-state .badge--draft":            "the Draft state badge",
		".doc-layout .lines .lines-card":      "the line cards",
		`.doc-layout .card-title`:             "the Details card",
		`.doc-layout dl.summary .summary-row`: "the Details pairs",
	} {
		if page.Find(sel).Length() == 0 {
			t.Errorf("%s has no %s (%s)", what, sel, why)
		}
	}
	if got := page.Find(".doc-layout .lines .lines-card").Length(); got != wantLines {
		t.Errorf("%s shows %d line cards, want %d", what, got, wantLines)
	}
	if got := norm(page.Find(".doc-state .badge--draft").Text()); got != "Draft" {
		t.Errorf("%s state badge reads %q, want Draft", what, got)
	}
	for sel, why := range map[string]string{
		"#wizard":                              "the wizard",
		".stepper":                             "the stepper",
		"#wizard-form":                         "the step form",
		"[form=wizard-form]":                   "a control of the step form",
		`a[href*="/edit/"]`:                    "a link to an edit step",
		`form[action*="/edit/"]`:               "a form posting to an edit step",
		`[hx-post*="/edit/"]`:                  "an htmx post to an edit step",
		`a[href*="/lines/"]`:                   "a line action link",
		`form[action*="/lines/"]`:              "a form posting a line",
		`[hx-post*="/lines/"]`:                 "an htmx post of a line",
		`[hx-get*="/lines/"]`:                  "an htmx line request",
		`a[href="` + docURL + `/delete"]`:      "Delete draft (the reader has no delete right)",
		`form[action="` + docURL + `/submit"]`: "a Submit form (the reader has no submit right)",
	} {
		if n := page.Find(sel).Length(); n != 0 {
			t.Errorf("%s has %d %s (%s), want none on a reader's draft page", what, n, sel, why)
		}
	}
	page.Find("main a, main button").Each(func(_ int, s *goquery.Selection) {
		if text := norm(s.Text()); strings.HasPrefix(text, "Change") || text == "Add an item" || strings.HasPrefix(text, "Remove") {
			t.Errorf("%s has a %q control, want no edit controls", what, text)
		}
	})
}

// m15CheckPage fails unless a GET of docURL as the current user is today's
// 303 to the check step, and that check step is a 200 with the step form.
func m15CheckPage(t *testing.T, e *scopeEnv, docURL, what string) {
	t.Helper()
	m15Redirect(t, e.get(t, docURL), docURL+"/edit/check", what+": GET "+docURL)
	rec := e.get(t, docURL+"/edit/check")
	if rec.Code != http.StatusOK {
		t.Errorf("%s: GET /edit/check = %d, want 200; body %.300s", what, rec.Code, rec.Body)
		return
	}
	if uitest.Doc(t, rec).Find("#wizard #wizard-form").Length() != 1 {
		t.Errorf("%s: the check step has no #wizard-form, want the writer's check page", what)
	}
}

// HOOK-019: a reader without write opening a draft gets the read-only
// document page; a writer, also one who didn't create the draft, still
// lands on the check page (HOOK-001's page-draft-redirect golden stays).
func TestDraft_ReaderGetsDocumentPage(t *testing.T) {
	e := m15NewEnv(t)
	u := m15DocURL(docscreentest.Hooked, e.id)

	e.who = e.reader
	m15ReadOnly(t, e.get(t, u), u, 2, "reader GET "+u)
	// htmx swaps the same page's main region.
	m15ReadOnly(t, e.get(t, u, "HX-Request", "true"), u, 2, "reader htmx GET "+u)

	e.who = e.open
	m15CheckPage(t, e.scopeEnv, u, "the draft's creator (write all)")
	// Rights, not created_by: a writer who didn't create the draft.
	e.who = e.restricted
	m15CheckPage(t, e.scopeEnv, u, "a writer who isn't the creator")
}

// HOOK-020: every step GET (each slug, check too) and every line GET (a
// line, a new line, remove) answers a reader 303 to the document page; a
// writer still gets each one. The reader's POSTs to the same URLs keep
// DOC's 403 with AUTH's text even with a stale version (rights before
// version, CD-259), and write nothing.
func TestDraft_ReaderStepAndLineURLsRedirect(t *testing.T) {
	e := m15NewEnv(t)
	u := m15DocURL(docscreentest.Hooked, e.id)
	gets := []string{"/lines/new", "/lines/1", "/lines/2", "/lines/1/remove"}
	for _, s := range m15HookedSteps {
		gets = append(gets, "/edit/"+s, "/edit/"+s+"?return=check")
	}
	for _, g := range gets {
		e.who = e.reader
		m15Redirect(t, e.get(t, u+g), u, "reader GET "+g)
		m15Redirect(t, e.get(t, u+g, "HX-Request", "true"), u, "reader htmx GET "+g)
		e.who = e.restricted
		if rec := e.get(t, u+g); rec.Code != http.StatusOK {
			t.Errorf("writer GET %s = %d (Location %q), want 200 as today", g, rec.Code, rec.Header().Get("Location"))
		}
	}

	e.who = e.reader
	before := m02Rec(t, e.hookedEnv, e.id)
	audits := len(e.fx.Audit(t, docscreentest.Hooked, e.id))
	stale := strconv.Itoa(before.Version + 5)
	posts := map[string]url.Values{
		"/edit/details":          {"supplier_id": {itoa(e.fx.M.Party["SHREE"])}, "site_id": {itoa(e.site["TECH-SITE"])}},
		"/edit/terms":            {"ref": {"M15-REF"}, "remarks": {"m15 remarks"}},
		"/edit/items":            {},
		"/lines/new":             {"project_id": {itoa(e.fx.M.Project["PRJ-METRO"])}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"3.000"}, "note": {"m15 new"}},
		"/lines/1":               {"project_id": {itoa(e.fx.M.Project["PRJ-METRO"])}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"4.000"}, "note": {"m15 edit"}},
		"/lines/1/remove":        {},
		"/lines/1/move?dir=down": {},
	}
	for path, form := range posts {
		for _, version := range []string{strconv.Itoa(before.Version), stale} {
			f := url.Values{}
			for k, v := range form {
				f[k] = v
			}
			f.Set("version", version)
			what := "reader POST " + path + " (version " + version + ")"
			rec := e.post(t, u+path, f)
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s = %d (Location %q), want DOC's 403", what, rec.Code, rec.Header().Get("Location"))
				continue
			}
			if text := norm(uitest.Doc(t, rec).Find("main").Text()); !strings.Contains(text, m15HookedDenied) {
				t.Errorf("%s main lacks AUTH's %q; main %.500s", what, m15HookedDenied, text)
			}
		}
	}
	if got := m02Rec(t, e.hookedEnv, e.id); !reflect.DeepEqual(got, before) {
		t.Errorf("the reader's POSTs changed the draft: %+v, want %+v", got, before)
	}
	if got := len(e.fx.Audit(t, docscreentest.Hooked, e.id)); got != audits {
		t.Errorf("the reader's POSTs wrote %d audit entries, want none", got-audits)
	}
}

// m15OrderOwnOnly is AUTH's refusal of a change to an order the user
// didn't create, with write (own) (auth/messages.go DeniedError).
const m15OrderOwnOnly = "You can only change Purchase Orders you created."

// m15OrderEnv is the docscreen harness on fixture.Order with the real AUTH
// (00-fixtures' default rights matrix, SeedDefaults) and the real engine,
// with one draft created by Priya (Purchase User).
type m15OrderEnv struct {
	*scopeEnv
	a  *auth.Auth
	id int64
}

func m15NewOrderEnv(t *testing.T) *m15OrderEnv {
	t.Helper()
	ctx := t.Context()
	h := fixturetest.New(t)
	deps := doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files, Approval: h.Approvals}
	// AUTH needs an InfoSource before the engine on AUTH exists (nsNew).
	info := doc.NewEngine(deps)
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info,
	})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	deps.Auth = a
	h.Engine = doc.NewEngine(deps)
	res := h.NewOrder(t, m15Load(t, a, h.Priya.ID), fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "350.0000"})
	return &m15OrderEnv{scopeEnv: scopeMount(t, h, h.Engine, a, h.Priya), a: a, id: res.ID}
}

func m15Load(t *testing.T, a *auth.Auth, id auth.UserID) auth.Principal {
	t.Helper()
	p, _, err := a.LoadPrincipal(t.Context(), id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%d): %v", id, err)
	}
	return p
}

// HOOK-021: on the real auth.Service and fixture.Order, the owner (Purchase
// User, write) and an Administrator get the check page; the Purchase
// Manager and the Director (read, no write) get the read-only page, and a
// step or line GET sends them there. Own scope: with Purchase User's write
// set to own (CD-246), a second Purchase User who isn't the owner gets the
// read-only page while the owner keeps the check page.
func TestDraft_ReaderRealAuth(t *testing.T) {
	e := m15NewOrderEnv(t)
	u := m15DocURL(fixture.Order, e.id)
	priya := m15Load(t, e.a, e.fx.Priya.ID)

	e.who = priya
	m15CheckPage(t, e.scopeEnv, u, "Priya, the owner (Purchase User, write all)")
	e.who = m15Load(t, e.a, e.fx.Admin.ID)
	m15CheckPage(t, e.scopeEnv, u, "an Administrator (CD-263)")

	for _, r := range []struct {
		name string
		id   auth.UserID
	}{{"Ravi (Purchase Manager, read without write)", e.fx.Ravi.ID}, {"Meena (Director, read without write)", e.fx.Meena.ID}} {
		e.who = m15Load(t, e.a, r.id)
		m15ReadOnly(t, e.get(t, u), u, 1, r.name+": GET "+u)
		m15Redirect(t, e.get(t, u+"/edit/check"), u, r.name+": GET /edit/check")
		m15Redirect(t, e.get(t, u+"/lines/1"), u, r.name+": GET /lines/1")
	}

	if err := e.a.SetRights(t.Context(), e.fx.Admin, "Purchase User", fixture.Order, map[auth.Right]auth.Scope{
		"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeOwn, "submit": auth.ScopeOwn,
		"delete": auth.ScopeOwn, "print": auth.ScopeAll, "export": auth.ScopeAll,
	}); err != nil {
		t.Fatalf("SetRights(Purchase User, write own): %v", err)
	}
	other := m15User(t, e.a, e.fx.Admin, "m15-buyer@vardhan.test", "Purchase User")
	e.who = other
	m15ReadOnly(t, e.get(t, u), u, 1, "a second Purchase User (write own, not the owner): GET "+u)
	m15Redirect(t, e.get(t, u+"/edit/details"), u, "a second Purchase User: GET /edit/details")
	m15Redirect(t, e.get(t, u+"/lines/new"), u, "a second Purchase User: GET /lines/new")
	// Their POST keeps DOC's 403 with AUTH's own-scope text.
	rec := e.post(t, u+"/edit/details", url.Values{"remarks": {"m15 not mine"}, "version": {"1"}})
	if rec.Code != http.StatusForbidden {
		t.Errorf("a second Purchase User: POST /edit/details = %d, want DOC's 403", rec.Code)
	} else if text := norm(uitest.Doc(t, rec).Find("main").Text()); !strings.Contains(text, m15OrderOwnOnly) {
		t.Errorf("a second Purchase User: POST /edit/details main lacks AUTH's %q; main %.500s", m15OrderOwnOnly, text)
	}
	e.who = m15Load(t, e.a, e.fx.Priya.ID)
	m15CheckPage(t, e.scopeEnv, u, "Priya, the owner (write own)")
}
