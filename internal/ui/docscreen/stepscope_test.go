package docscreen_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// CD-205 through the HTTP path on the real engine: step 1 holds a Link, step
// 2 a Link and a text field, then the items and check steps.

const (
	scopeType     = "docscreentest.StepScope"
	scopePassword = "Correct-Horse-42"

	scopeSupplierMsg = "Supplier RAJKOT Rajkot Castings is disabled. Pick an active supplier, or ask an admin to enable it."
	scopeProjectMsg  = "Project PRJ-METRO Pune Metro Depot is disabled. Pick an active project, or ask an admin to enable it."
	scopeItemMsg     = "Line 1: Item CEMENT-OPC OPC Cement 53 Grade is disabled. Pick an active item, or ask an admin to enable it."
	scopeRefLongMsg  = "Reference can be at most 12 characters."
	scopeQtyMsg      = "Line 1: Quantity must be more than 0."
)

type scopeHooks struct{ authtest.NopHooks }

func (scopeHooks) Date(context.Context, *db.Tx, int64) (types.Date, error) {
	return types.Date{Year: 2026, Month: time.October, Day: 24}, nil
}

var scopeOnce sync.Once

func registerScope() {
	scopeOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: scopeType, Label: "Step Scope", PluralLabel: "Step Scopes", Table: "stepscope", LinesTable: "stepscope_line",
			Submittable: true, Hooks: scopeHooks{}, ProjectFields: []string{"project_id"},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: fixture.Party, Required: true},
				{Name: "project_id", Label: "Project", Type: doctype.Link, Link: fixture.Project, Step: "delivery"},
				{Name: "ref", Label: "Reference", Type: doctype.Text, MaxLen: 12, Step: "delivery"},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, Required: true, OnLine: true},
				{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Positive: true, OnLine: true},
			},
		})
	})
}

// scopeEnv is the docscreen harness on a fixture database and the real
// engine, with one draft: RAJKOT on step 1, PRJ-METRO and "old" on step 2,
// line 1 CEMENT-OPC and line 2 WIRE-18G.
type scopeEnv struct {
	*docEnv
	fx  *fixturetest.H
	who auth.Principal
	id  int64
}

func scopeTables(t *testing.T, h *fixturetest.H) {
	t.Helper()
	h.Exec(t, `CREATE TABLE stepscope (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		supplier_id INTEGER, project_id INTEGER, ref TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE stepscope_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		item_id INTEGER, note TEXT NOT NULL DEFAULT '', qty INTEGER)`)
}

// scopeMount points a docscreen harness at h and engine.
func scopeMount(t *testing.T, h *fixturetest.H, engine doc.Engine, a auth.Service, who auth.Principal) *scopeEnv {
	t.Helper()
	e := newFormEnv(t, allRights...)
	se := &scopeEnv{docEnv: e, fx: h, who: who}
	e.d.DB, e.d.Engine, e.d.Auth = h.DB, engine, a
	e.d.Principal = func(*http.Request) (auth.Principal, bool) { return se.who, true }
	e.remount()
	return se
}

func newScopeEnv(t *testing.T) *scopeEnv {
	t.Helper()
	registerScope()
	h := fixturetest.New(t)
	scopeTables(t, h)
	h.Naming.Prefix = map[string]string{scopeType: "SS"}
	se := scopeMount(t, h, h.Engine, h.Auth, h.Admin)
	res := h.MustDo(t, context.Background(), h.Admin, scopeType, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": itoa(h.M.Party["RAJKOT"]), "project_id": itoa(h.M.Project["PRJ-METRO"]), "ref": "old",
		},
		Lines: []map[string]string{
			{"item_id": itoa(h.M.Item["CEMENT-OPC"]), "note": "first", "qty": "10.000"},
			{"item_id": itoa(h.M.Item["WIRE-18G"]), "note": "second", "qty": "5.000"},
		},
	})
	se.id = res.ID
	return se
}

// newScopeRealEnv is the harness over the real AUTH service: an open user
// and a user restricted to PRJ-TECHPARK, both with read, create, write and
// submit on the DocType. It starts as the open user with no draft.
func newScopeRealEnv(t *testing.T) (se *scopeEnv, open, restricted auth.Principal) {
	t.Helper()
	registerScope()
	h := fixturetest.New(t)
	scopeTables(t, h)
	h.Naming.Prefix = map[string]string{scopeType: "SS"}
	ctx := t.Context()
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	adminID, err := a.CreateFirstAdmin(ctx, "scope-admin@vardhan.test", "Scope Admin", scopePassword)
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	load := func(u auth.UserID) auth.Principal {
		t.Helper()
		p, _, err := a.LoadPrincipal(ctx, u)
		if err != nil {
			t.Fatalf("LoadPrincipal(%d): %v", u, err)
		}
		return p
	}
	admin := load(adminID)
	if err := a.CreateRole(ctx, admin, "Scoper"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	all := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll, "submit": auth.ScopeAll}
	if err := a.SetRights(ctx, admin, "Scoper", scopeType, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	user := func(email string) auth.UserID {
		t.Helper()
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: scopePassword, Roles: []string{"Scoper"}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return id
	}
	openID, restrictedID := user("scope-open@vardhan.test"), user("scope-restricted@vardhan.test")
	if err := a.ChangeRestrictions(ctx, admin, restrictedID, auth.DimProject, []int64{h.M.Project["PRJ-TECHPARK"]}, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
	engine := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	open, restricted = load(openID), load(restrictedID)
	return scopeMount(t, h, engine, a, open), open, restricted
}

func (e *scopeEnv) docURL() string { return "/doc/" + scopeType + "/" + itoa(e.id) }

func (e *scopeEnv) stepURL(slug string) string { return e.docURL() + "/edit/" + slug }

func (e *scopeEnv) record(t *testing.T) doc.Record {
	t.Helper()
	return e.fx.Record(t, scopeType, e.id)
}

// disable disables a seeded master: "party", "project" or "item".
func (e *scopeEnv) disable(t *testing.T, kind, code string) {
	t.Helper()
	fixture.SetDisabled(t, e.fx.DB, "fixture_"+kind, code, true)
}

func (e *scopeEnv) enable(t *testing.T, kind, code string) {
	t.Helper()
	fixture.SetDisabled(t, e.fx.DB, "fixture_"+kind, code, false)
}

// deliveryForm is step 2's form at version v.
func (e *scopeEnv) deliveryForm(v int, ref string) url.Values {
	return url.Values{
		"version": {strconv.Itoa(v)}, "project_id": {itoa(e.fx.M.Project["PRJ-METRO"])}, "ref": {ref},
	}
}

// pageVersion is the version a wizard page posts.
func pageVersion(t *testing.T, rec *httptest.ResponseRecorder) int {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
	}
	raw, ok := uitest.Doc(t, rec).Find("#doc-version").Attr("value")
	if !ok {
		t.Fatalf("no #doc-version on the page; body %.300s", rec.Body)
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("#doc-version = %q: %v", raw, err)
	}
	return v
}

// wizardText is the wizard's page text.
func wizardText(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return norm(uitest.Doc(t, rec).Find("#wizard").Text())
}

// summaryLinks are the hrefs of the error summary's links.
func summaryLinks(page *goquery.Document) []string {
	return page.Find(".error-summary a").Map(func(_ int, s *goquery.Selection) string {
		h, _ := s.Attr("href")
		return h
	})
}

// wantHeading fails unless the page shows step head.
func wantHeading(t *testing.T, rec *httptest.ResponseRecorder, head string) {
	t.Helper()
	if got := norm(uitest.Doc(t, rec).Find("h1#step-title").Text()); got != head {
		t.Errorf("heading = %q, want %q", got, head)
	}
}

// A problem on step 1 doesn't stop Next, Back or Save and finish later on
// step 2: the user moves and the typed value is stored, with and without
// htmx. An unchanged form moves too, without a write.
func TestStepScope_MovesPastAnotherStepsProblem(t *testing.T) {
	cases := []struct {
		name     string
		nav      string
		ret      string
		to       string // last path element, "" for the document page
		typed    bool
		wantHead string
	}{
		{name: "next", to: "items", typed: true, wantHead: "Items"},
		{name: "back", nav: "back", to: "details", typed: true, wantHead: "Details"},
		{name: "finish later", nav: "finish", typed: true},
		{name: "return to check", ret: "check", to: "check", typed: true, wantHead: "Check and submit"},
		{name: "next unchanged", to: "items", wantHead: "Items"},
		{name: "back unchanged", nav: "back", to: "details", wantHead: "Details"},
		{name: "finish later unchanged", nav: "finish"},
	}
	for _, c := range cases {
		for _, hx := range []bool{false, true} {
			t.Run(c.name+map[bool]string{false: "/plain", true: "/htmx"}[hx], func(t *testing.T) {
				e := newScopeEnv(t)
				e.disable(t, "party", "RAJKOT")
				ref, version := "old", 1
				if c.typed {
					ref, version = "typed", 2
				}
				form := e.deliveryForm(1, ref)
				if c.nav != "" {
					form.Set("nav", c.nav)
				}
				if c.ret != "" {
					form.Set("return", c.ret)
				}
				want := e.docURL()
				if c.to != "" {
					want = e.stepURL(c.to)
				}
				var rec *httptest.ResponseRecorder
				switch {
				case !hx:
					rec = e.post(t, e.stepURL("delivery"), form)
					if got := location(t, rec); got != want {
						t.Errorf("303 to %q, want %q", got, want)
					}
				case c.nav == "finish":
					rec = e.post(t, e.stepURL("delivery"), form, "HX-Request", "true")
					if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != want {
						t.Errorf("status %d, HX-Redirect %q; want 200 to %q", rec.Code, rec.Header().Get("HX-Redirect"), want)
					}
				default:
					rec = e.post(t, e.stepURL("delivery"), form, "HX-Request", "true")
					if rec.Code != http.StatusOK {
						t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
					}
					if got := pathOf(t, rec.Header().Get("HX-Push-Url")); got != want {
						t.Errorf("HX-Push-Url = %q, want %q", got, want)
					}
					wantHeading(t, rec, c.wantHead)
				}
				stored := e.record(t)
				if stored.Values["ref"] != ref || stored.Version != version {
					t.Errorf("stored ref %q at version %d, want %q at %d", stored.Values["ref"], stored.Version, ref, version)
				}
				if len(stored.Lines) != 2 {
					t.Errorf("stored lines = %d, want the 2 kept", len(stored.Lines))
				}
			})
		}
	}
}

// The items step's form names no header field, so a Lines step mustn't fall
// back to an unscoped save: a header problem doesn't stop it, and neither does
// a stored line problem, since posting the step changes no line.
func TestStepScope_ItemsStepMovesPastStoredProblems(t *testing.T) {
	for _, c := range []struct {
		name, nav, to string
	}{
		{"next", "", "check"},
		{"back", "back", "delivery"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newScopeEnv(t)
			e.disable(t, "party", "RAJKOT")
			e.disable(t, "item", "CEMENT-OPC")
			form := url.Values{"version": {"1"}}
			if c.nav != "" {
				form.Set("nav", c.nav)
			}
			rec := e.post(t, e.stepURL("items"), form)
			if got, want := location(t, rec), e.stepURL(c.to); got != want {
				t.Errorf("303 to %q, want %q", got, want)
			}
		})
	}
}

// The two-master deadlock of CD-205: masters on step 1 and step 2 are both
// disabled, the user fixes them one at a time in either order, and Submit
// passes. Everything after the masters are disabled is HTTP, on real AUTH.
func TestStepScope_DeadlockGone(t *testing.T) {
	for _, order := range []string{"later step first", "first step first"} {
		t.Run(order, func(t *testing.T) {
			e, _, _ := newScopeRealEnv(t)
			m := e.fx.M

			rec := e.post(t, "/doc/"+scopeType+"/new", url.Values{"supplier_id": {itoa(m.Party["RAJKOT"])}})
			next := location(t, rec)
			idPart, ok := strings.CutPrefix(next, "/doc/"+scopeType+"/")
			if !ok {
				t.Fatalf("create went to %q", next)
			}
			id, err := strconv.ParseInt(strings.TrimSuffix(idPart, "/edit/delivery"), 10, 64)
			if err != nil {
				t.Fatalf("id in %q: %v", next, err)
			}
			e.id = id

			v := pageVersion(t, e.get(t, e.stepURL("delivery")))
			rec = e.post(t, e.stepURL("delivery"), e.deliveryForm(v, "old"))
			if got := location(t, rec); got != e.stepURL("items") {
				t.Fatalf("step 2 went to %q", got)
			}
			v = pageVersion(t, e.get(t, e.stepURL("items")))
			rec = e.post(t, e.docURL()+"/lines/new", url.Values{
				"version": {strconv.Itoa(v)}, "item_id": {itoa(m.Item["CEMENT-OPC"])}, "note": {"n"}, "qty": {"10"},
			})
			if got := location(t, rec); got != e.stepURL("items") {
				t.Fatalf("new line went to %q", got)
			}

			e.disable(t, "party", "RAJKOT")
			e.disable(t, "project", "PRJ-METRO")

			fixDelivery := func() {
				t.Helper()
				v := pageVersion(t, e.get(t, e.stepURL("delivery")))
				form := e.deliveryForm(v, "new")
				form.Set("project_id", itoa(m.Project["PRJ-TECHPARK"]))
				rec := e.post(t, e.stepURL("delivery"), form)
				if rec.Code != http.StatusSeeOther {
					t.Fatalf("fixing step 2: status %d, want 303; %.300s", rec.Code, rec.Body)
				}
				got := e.record(t)
				if got.Values["project_id"] != itoa(m.Project["PRJ-TECHPARK"]) || got.Values["ref"] != "new" {
					t.Fatalf("after fixing step 2: stored %q", got.Values)
				}
			}
			fixDetails := func() {
				t.Helper()
				v := pageVersion(t, e.get(t, e.stepURL("details")))
				rec := e.post(t, e.stepURL("details"), url.Values{"version": {strconv.Itoa(v)}, "supplier_id": {itoa(m.Party["SHREE"])}})
				if rec.Code != http.StatusSeeOther {
					t.Fatalf("fixing step 1: status %d, want 303; %.300s", rec.Code, rec.Body)
				}
				if got := e.record(t).Values["supplier_id"]; got != itoa(m.Party["SHREE"]) {
					t.Fatalf("after fixing step 1: stored supplier %q", got)
				}
			}
			if order == "later step first" {
				fixDelivery()
				fixDetails()
			} else {
				fixDetails()
				fixDelivery()
			}

			check := e.get(t, e.stepURL("check"))
			if text := wizardText(t, check); strings.Contains(text, "is disabled") {
				t.Errorf("check page after both fixes still shows a disabled master: %q", text)
			}
			rec = e.post(t, e.docURL()+"/submit", url.Values{"version": {strconv.Itoa(pageVersion(t, check))}})
			if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
				t.Fatalf("submit: status %d, want a success; %.300s", rec.Code, rec.Body)
			}
			if got := e.fx.Info(t, scopeType, e.id); got.State != doctype.StateApproved {
				t.Errorf("state after submit = %q, want approved", got.State)
			}
		})
	}
}

// An error on the current step's own fields is still a 422: its error shows,
// the typed values stay, nothing is stored, and another step's problem isn't
// in that error summary.
func TestStepScope_OwnStepErrorStill422(t *testing.T) {
	cases := []struct {
		name    string
		nav     string
		disable bool // PRJ-METRO, the step's own stored link
		ref     string
		link    string
		msg     string
	}{
		{name: "typed error", ref: "much-too-long-a-reference", link: "#f-ref", msg: scopeRefLongMsg},
		{name: "typed error on Back", nav: "back", ref: "much-too-long-a-reference", link: "#f-ref", msg: scopeRefLongMsg},
		{name: "own disabled master", disable: true, ref: "typed", link: "#f-project_id", msg: scopeProjectMsg},
		{name: "own disabled master on Back", nav: "back", disable: true, ref: "typed", link: "#f-project_id", msg: scopeProjectMsg},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newScopeEnv(t)
			e.disable(t, "party", "RAJKOT")
			if c.disable {
				e.disable(t, "project", "PRJ-METRO")
			}
			form := e.deliveryForm(1, c.ref)
			if c.nav != "" {
				form.Set("nav", c.nav)
			}
			rec := e.post(t, e.stepURL("delivery"), form)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
			}
			wantHeading(t, rec, "Delivery")
			page := uitest.Doc(t, rec)
			link := page.Find(`.error-summary a[href="` + c.link + `"]`)
			if link.Length() != 1 || norm(link.Text()) != c.msg {
				t.Errorf("error summary link to %s = %q (%d links), want %q", c.link, norm(link.Text()), link.Length(), c.msg)
			}
			if got := summaryLinks(page); !slices.Equal(got, []string{c.link}) {
				t.Errorf("error summary links = %q, want only %s (step 1's error has no field here)", got, c.link)
			}
			if v, _ := page.Find(`#wizard input[name="ref"]`).Attr("value"); v != c.ref {
				t.Errorf("ref = %q, want the typed %q", v, c.ref)
			}
			stored := e.record(t)
			if stored.Values["ref"] != "old" || stored.Version != 1 {
				t.Errorf("stored ref %q at version %d, want old at 1", stored.Values["ref"], stored.Version)
			}
		})
	}
}

// Stored problems show on their own step and on the Check page with DOC's
// text, on a stepper GET too; a step never shows another step's problem, and
// they leave once the master is usable again.
func TestStepScope_ProblemsShowWhereTheyBelong(t *testing.T) {
	e := newScopeEnv(t)
	e.disable(t, "party", "RAJKOT")
	e.disable(t, "project", "PRJ-METRO")
	e.disable(t, "item", "CEMENT-OPC")

	steps := []struct {
		slug    string
		own     string
		foreign []string
		noLinks []string
	}{
		{"details", scopeSupplierMsg, []string{scopeProjectMsg, scopeItemMsg}, []string{"#f-project_id"}},
		{"delivery", scopeProjectMsg, []string{scopeSupplierMsg, scopeItemMsg}, []string{"#f-supplier_id"}},
		{"items", scopeItemMsg, []string{scopeSupplierMsg, scopeProjectMsg}, []string{"#f-supplier_id", "#f-project_id"}},
	}
	for _, s := range steps {
		for _, hx := range []bool{false, true} {
			t.Run(s.slug+map[bool]string{false: "/plain", true: "/htmx"}[hx], func(t *testing.T) {
				var hdr []string
				if hx {
					hdr = []string{"HX-Request", "true"}
				}
				rec := e.get(t, e.stepURL(s.slug), hdr...)
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
				}
				text := wizardText(t, rec)
				if !strings.Contains(text, s.own) {
					t.Errorf("step %s doesn't show its own problem %q; wizard text: %q", s.slug, s.own, text)
				}
				for _, msg := range s.foreign {
					if strings.Contains(text, msg) {
						t.Errorf("step %s shows another step's problem %q", s.slug, msg)
					}
				}
				links := summaryLinks(uitest.Doc(t, rec))
				for _, dead := range s.noLinks {
					if slices.Contains(links, dead) {
						t.Errorf("step %s's error summary links %s, a field of another step", s.slug, dead)
					}
				}
			})
		}
	}

	t.Run("check page", func(t *testing.T) {
		rec := e.get(t, e.stepURL("check"))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
		}
		page := uitest.Doc(t, rec)
		for _, c := range []struct{ section, msg string }{
			{"Details", scopeSupplierMsg},
			{"Delivery", scopeProjectMsg},
			{"Items", scopeItemMsg},
		} {
			sec := checkSection(page.Selection, c.section)
			if got := strings.Count(norm(sec.Text()), c.msg); got != 1 {
				t.Errorf("section %s shows %q %d times, want once; text %q", c.section, c.msg, got, norm(sec.Text()))
			}
			if got := norm(sec.Find(".check-status").Text()); got != "Needs attention" {
				t.Errorf("section %s status = %q, want Needs attention", c.section, got)
			}
		}
	})

	t.Run("gone once usable", func(t *testing.T) {
		e.enable(t, "party", "RAJKOT")
		e.enable(t, "project", "PRJ-METRO")
		e.enable(t, "item", "CEMENT-OPC")
		for _, slug := range []string{"details", "delivery", "items", "check"} {
			if text := wizardText(t, e.get(t, e.stepURL(slug))); strings.Contains(text, "is disabled") {
				t.Errorf("step %s still shows a problem after the masters are enabled: %q", slug, text)
			}
		}
	})
}

// The step an htmx save lands on shows that step's stored problem.
func TestStepScope_HTMXMoveShowsTheNextStepsProblem(t *testing.T) {
	e := newScopeEnv(t)
	e.disable(t, "party", "RAJKOT")
	form := e.deliveryForm(1, "typed")
	form.Set("nav", "back")
	rec := e.post(t, e.stepURL("delivery"), form, "HX-Request", "true")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
	}
	if text := wizardText(t, rec); !strings.Contains(text, scopeSupplierMsg) {
		t.Errorf("Details after Back doesn't show %q; wizard text: %q", scopeSupplierMsg, text)
	}
}

// The view's own Required list and DOC's Problems meet on the Check page: a
// message shows once, whichever of them supplies it.
func TestStepScope_RequiredShowsOnceBesideProblems(t *testing.T) {
	const msg = "Supplier is required."
	e := newScopeEnv(t)
	in := e.fx.In(e.fx.Load(t, scopeType, e.id))
	in.Values["supplier_id"] = ""
	e.fx.MustDo(t, t.Context(), e.who, scopeType, e.id, doc.ActionSave, in)
	e.disable(t, "project", "PRJ-METRO")

	page := uitest.Doc(t, e.get(t, e.stepURL("check")))
	sec := checkSection(page.Selection, "Details")
	if got := strings.Count(norm(sec.Text()), msg); got != 1 {
		t.Errorf("Details shows %q %d times, want once; text %q", msg, got, norm(sec.Text()))
	}
	if text := norm(checkSection(page.Selection, "Delivery").Text()); !strings.Contains(text, scopeProjectMsg) {
		t.Errorf("Delivery doesn't show %q; text %q", scopeProjectMsg, text)
	}
}

// Submit stays the gate: from Check it refuses a disabled master with DOC's
// text on the first failing step, and that step's summary holds only its own
// errors, so no link is dead. The document stays a draft.
func TestStepScope_SubmitRefusesWithDOCsText(t *testing.T) {
	cases := []struct {
		name  string
		bad   func(t *testing.T, e *scopeEnv)
		head  string
		msg   string
		other string
	}{
		{"one master on step 2", func(t *testing.T, e *scopeEnv) { e.disable(t, "project", "PRJ-METRO") },
			"Delivery", scopeProjectMsg, ""},
		{"masters on both steps", func(t *testing.T, e *scopeEnv) {
			e.disable(t, "party", "RAJKOT")
			e.disable(t, "project", "PRJ-METRO")
		}, "Details", scopeSupplierMsg, scopeProjectMsg},
		{"line item", func(t *testing.T, e *scopeEnv) { e.disable(t, "item", "CEMENT-OPC") },
			"Items", scopeItemMsg, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newScopeEnv(t)
			c.bad(t, e)
			rec := e.post(t, e.docURL()+"/submit", url.Values{"version": {"1"}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
			}
			wantHeading(t, rec, c.head)
			page := uitest.Doc(t, rec)
			items := page.Find(".error-summary li").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
			if !slices.Equal(items, []string{c.msg}) {
				t.Errorf("error summary = %q, want only [%q]", items, c.msg)
			}
			if c.other != "" && strings.Contains(wizardText(t, rec), c.other) {
				t.Errorf("step %s shows another step's %q", c.head, c.other)
			}
			if got := e.fx.Info(t, scopeType, e.id); got.State != doctype.StateDraft {
				t.Errorf("state = %q, want draft", got.State)
			}
		})
	}
}

// A header problem doesn't stop a line save, on the line page and the grid.
func TestStepScope_LinePostPastAHeaderProblem(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "grid"}[hx], func(t *testing.T) {
			e := newScopeEnv(t)
			e.disable(t, "party", "RAJKOT")
			form := url.Values{"version": {"1"}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "note": {"edited"}, "qty": {"12"}}
			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.post(t, e.docURL()+"/lines/1", form, hdr...)
			if hx {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
				}
			} else if got := location(t, rec); got != e.stepURL("items") {
				t.Errorf("303 to %q, want the items step", got)
			}
			stored := e.record(t)
			if stored.Lines[0]["note"] != "edited" || stored.Version != 2 {
				t.Errorf("stored note %q at version %d, want edited at 2", stored.Lines[0]["note"], stored.Version)
			}
		})
	}
	t.Run("new line", func(t *testing.T) {
		e := newScopeEnv(t)
		e.disable(t, "party", "RAJKOT")
		rec := e.post(t, e.docURL()+"/lines/new", url.Values{
			"version": {"1"}, "item_id": {itoa(e.fx.M.Item["BOLT-M12"])}, "note": {"added"}, "qty": {"1"},
		})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Errorf("303 to %q, want the items step", got)
		}
		if stored := e.record(t); len(stored.Lines) != 3 || stored.Lines[2]["note"] != "added" {
			t.Errorf("stored lines = %q, want a third with note added", stored.Lines)
		}
	})
}

// A bad line can be fixed while the header is bad too: editing its row to a
// usable item saves, and the header's problem stays for its own step.
func TestStepScope_LinePostFixesItsOwnBadRow(t *testing.T) {
	e := newScopeEnv(t)
	e.disable(t, "party", "RAJKOT")
	e.disable(t, "item", "CEMENT-OPC")
	rec := e.post(t, e.docURL()+"/lines/1", url.Values{
		"version": {"1"}, "item_id": {itoa(e.fx.M.Item["BOLT-M12"])}, "note": {"edited"}, "qty": {"10"},
	})
	if got := location(t, rec); got != e.stepURL("items") {
		t.Errorf("303 to %q, want the items step", got)
	}
	stored := e.record(t)
	if stored.Lines[0]["item_id"] != itoa(e.fx.M.Item["BOLT-M12"]) || stored.Version != 2 {
		t.Errorf("stored line 1 %q at version %d, want BOLT-M12 at 2", stored.Lines[0], stored.Version)
	}
	if text := wizardText(t, e.get(t, e.stepURL("details"))); !strings.Contains(text, scopeSupplierMsg) {
		t.Errorf("Details no longer shows %q; text %q", scopeSupplierMsg, text)
	}
}

// Moves and removals work while the header is bad.
func TestStepScope_ReorderPastAHeaderProblem(t *testing.T) {
	t.Run("move", func(t *testing.T) {
		e := newScopeEnv(t)
		e.disable(t, "party", "RAJKOT")
		before := e.record(t)
		rec := e.post(t, e.docURL()+"/lines/2/move?dir=up", url.Values{"version": {"1"}})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Errorf("303 to %q, want the items step", got)
		}
		after := e.record(t)
		if after.Lines[0]["id"] != before.Lines[1]["id"] || after.Lines[1]["id"] != before.Lines[0]["id"] || after.Version != 2 {
			t.Errorf("after the move: lines %q at version %d, want the two swapped at 2", after.Lines, after.Version)
		}
	})
	t.Run("remove", func(t *testing.T) {
		e := newScopeEnv(t)
		e.disable(t, "party", "RAJKOT")
		rec := e.post(t, e.docURL()+"/lines/2/remove", url.Values{"version": {"1"}})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Errorf("303 to %q, want the items step", got)
		}
		if after := e.record(t); len(after.Lines) != 1 || after.Version != 2 {
			t.Errorf("after the removal: %d lines at version %d, want 1 at 2", len(after.Lines), after.Version)
		}
	})
	t.Run("removing the bad line", func(t *testing.T) {
		e := newScopeEnv(t)
		e.disable(t, "party", "RAJKOT")
		e.disable(t, "item", "CEMENT-OPC")
		rec := e.post(t, e.docURL()+"/lines/1/remove", url.Values{"version": {"1"}})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Errorf("303 to %q, want the items step", got)
		}
		if after := e.record(t); len(after.Lines) != 1 || after.Lines[0]["note"] != "second" {
			t.Errorf("after the removal: lines %q, want only the second", after.Lines)
		}
	})
}

// A stored line problem doesn't stop an edit to another row, on the line page
// and the grid, and stays on the items step afterwards.
func TestStepScope_LinePostPastAnotherLinesProblem(t *testing.T) {
	for _, hx := range []bool{false, true} {
		t.Run(map[bool]string{false: "page", true: "grid"}[hx], func(t *testing.T) {
			e := newScopeEnv(t)
			e.disable(t, "item", "CEMENT-OPC")
			form := url.Values{"version": {"1"}, "item_id": {itoa(e.fx.M.Item["WIRE-18G"])}, "note": {"edited"}, "qty": {"5"}}
			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.post(t, e.docURL()+"/lines/2", form, hdr...)
			if hx {
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
				}
			} else if got := location(t, rec); got != e.stepURL("items") {
				t.Errorf("303 to %q, want the items step", got)
			}
			stored := e.record(t)
			if stored.Lines[1]["note"] != "edited" || stored.Version != 2 {
				t.Errorf("stored note %q at version %d, want edited at 2", stored.Lines[1]["note"], stored.Version)
			}
			if text := wizardText(t, e.get(t, e.stepURL("items"))); !strings.Contains(text, scopeItemMsg) {
				t.Errorf("items step doesn't show %q; text %q", scopeItemMsg, text)
			}
		})
	}
}

// Two disabled line items and a disabled header master are no deadlock: moves
// save, each row is fixed on its own in either order, the header is fixed on
// its step, and Submit passes.
func TestStepScope_TwoDisabledLinesNoDeadlock(t *testing.T) {
	for _, order := range [][2]int{{1, 2}, {2, 1}} {
		t.Run("line "+strconv.Itoa(order[0])+" first", func(t *testing.T) {
			e := newScopeEnv(t)
			m := e.fx.M
			e.disable(t, "party", "RAJKOT")
			e.disable(t, "item", "CEMENT-OPC")
			e.disable(t, "item", "WIRE-18G")

			v := pageVersion(t, e.get(t, e.stepURL("items")))
			rec := e.post(t, e.docURL()+"/lines/2/move?dir=up", url.Values{"version": {strconv.Itoa(v)}})
			if got := location(t, rec); got != e.stepURL("items") {
				t.Fatalf("move went to %q", got)
			}
			if got := e.record(t).Lines[0]["note"]; got != "second" {
				t.Errorf("after the move line 1 is %q, want second", got)
			}

			fix := func(no int, item string) {
				t.Helper()
				v := pageVersion(t, e.get(t, e.stepURL("items")))
				line := e.record(t).Lines[no-1]
				rec := e.post(t, e.docURL()+"/lines/"+strconv.Itoa(no), url.Values{
					"version": {strconv.Itoa(v)}, "item_id": {itoa(m.Item[item])}, "note": {line["note"]}, "qty": {line["qty"]},
				})
				if rec.Code != http.StatusSeeOther {
					t.Fatalf("fixing line %d: status %d, want 303; %.300s", no, rec.Code, rec.Body)
				}
				if got := e.record(t).Lines[no-1]["item_id"]; got != itoa(m.Item[item]) {
					t.Fatalf("after fixing line %d: stored item %q", no, got)
				}
			}
			fix(order[0], "BOLT-M12")
			fix(order[1], "MS-ANGLE-50")

			v = pageVersion(t, e.get(t, e.stepURL("details")))
			rec = e.post(t, e.stepURL("details"), url.Values{"version": {strconv.Itoa(v)}, "supplier_id": {itoa(m.Party["SHREE"])}})
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("fixing the supplier: status %d, want 303; %.300s", rec.Code, rec.Body)
			}
			check := e.get(t, e.stepURL("check"))
			if text := wizardText(t, check); strings.Contains(text, "is disabled") {
				t.Errorf("check page after every fix still shows a disabled master: %q", text)
			}
			rec = e.post(t, e.docURL()+"/submit", url.Values{"version": {strconv.Itoa(pageVersion(t, check))}})
			if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
				t.Fatalf("submit: status %d, want a success; %.300s", rec.Code, rec.Body)
			}
			if got := e.fx.Info(t, scopeType, e.id); got.State != doctype.StateApproved {
				t.Errorf("state after submit = %q, want approved", got.State)
			}
		})
	}
	t.Run("removals", func(t *testing.T) {
		e := newScopeEnv(t)
		e.disable(t, "item", "CEMENT-OPC")
		e.disable(t, "item", "WIRE-18G")
		rec := e.post(t, e.docURL()+"/lines/1/remove", url.Values{"version": {"1"}})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Fatalf("first removal went to %q", got)
		}
		left := e.get(t, e.stepURL("items"))
		const msg = "Line 1: Item WIRE-18G Binding Wire 18G is disabled. Pick an active item, or ask an admin to enable it."
		if text := wizardText(t, left); !strings.Contains(text, msg) {
			t.Errorf("items step doesn't show %q; text %q", msg, text)
		}
		rec = e.post(t, e.docURL()+"/lines/1/remove", url.Values{"version": {strconv.Itoa(pageVersion(t, left))}})
		if got := location(t, rec); got != e.stepURL("items") {
			t.Fatalf("second removal went to %q", got)
		}
		if got := e.record(t).Lines; len(got) != 0 {
			t.Errorf("stored lines = %q, want none", got)
		}
	})
}

// A move saves past a stored problem on either line, not only the header's.
func TestStepScope_ReorderPastALineProblem(t *testing.T) {
	e := newScopeEnv(t)
	e.disable(t, "item", "CEMENT-OPC")
	before := e.record(t)
	rec := e.post(t, e.docURL()+"/lines/1/move?dir=down", url.Values{"version": {"1"}})
	if got := location(t, rec); got != e.stepURL("items") {
		t.Errorf("303 to %q, want the items step", got)
	}
	after := e.record(t)
	if after.Lines[0]["id"] != before.Lines[1]["id"] || after.Lines[1]["id"] != before.Lines[0]["id"] || after.Version != 2 {
		t.Errorf("after the move: lines %q at version %d, want the two swapped at 2", after.Lines, after.Version)
	}
	const msg = "Line 2: Item CEMENT-OPC OPC Cement 53 Grade is disabled. Pick an active item, or ask an admin to enable it."
	if text := wizardText(t, e.get(t, e.stepURL("items"))); !strings.Contains(text, msg) {
		t.Errorf("items step doesn't show the renumbered %q; text %q", msg, text)
	}
}

// The changed row's own error refuses with its text and the typed values,
// nothing stored, on the line page and the grid.
func TestStepScope_LinePostOwnErrorRefuses(t *testing.T) {
	cases := []struct {
		name string
		qty  string
		msg  string
		item string // disabled item of line 1
	}{
		{name: "kept disabled item", qty: "12", msg: scopeItemMsg, item: "CEMENT-OPC"},
		{name: "typed quantity", qty: "0", msg: scopeQtyMsg},
	}
	for _, c := range cases {
		for _, hx := range []bool{false, true} {
			t.Run(c.name+map[bool]string{false: "/page", true: "/grid"}[hx], func(t *testing.T) {
				e := newScopeEnv(t)
				e.disable(t, "party", "RAJKOT")
				if c.item != "" {
					e.disable(t, "item", c.item)
				}
				form := url.Values{"version": {"1"}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "note": {"typed note"}, "qty": {c.qty}}
				var hdr []string
				if hx {
					hdr = []string{"HX-Request", "true"}
				}
				rec := e.post(t, e.docURL()+"/lines/1", form, hdr...)
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
				}
				page := uitest.Doc(t, rec)
				if text := norm(page.Text()); !strings.Contains(text, c.msg) {
					t.Errorf("page doesn't show %q; text %.500q", c.msg, text)
				}
				if v, _ := page.Find(`input[name="note"]`).Attr("value"); v != "typed note" {
					t.Errorf("note = %q, want the typed value", v)
				}
				stored := e.record(t)
				if stored.Lines[0]["note"] != "first" || stored.Version != 1 {
					t.Errorf("stored note %q at version %d, want first at 1", stored.Lines[0]["note"], stored.Version)
				}
			})
		}
	}
}

// A stale step or line save is 409 as ever (CD-94), and keeps the typed text.
func TestStepScope_StaleSaveIs409(t *testing.T) {
	e := newScopeEnv(t)
	e.disable(t, "party", "RAJKOT")
	if rec := e.post(t, e.stepURL("delivery"), e.deliveryForm(1, "first")); rec.Code != http.StatusSeeOther {
		t.Fatalf("first save: status %d; body %.300s", rec.Code, rec.Body)
	}
	rec := e.post(t, e.stepURL("delivery"), e.deliveryForm(1, "late"))
	if rec.Code != http.StatusConflict {
		t.Fatalf("stale save: status = %d, want 409; body %.300s", rec.Code, rec.Body)
	}
	if v, _ := uitest.Doc(t, rec).Find(`#wizard input[name="ref"]`).Attr("value"); v != "late" {
		t.Errorf("ref = %q, want the typed value kept", v)
	}
	if got := e.record(t).Values["ref"]; got != "first" {
		t.Errorf("stored ref = %q, want first", got)
	}
	line := url.Values{"version": {"1"}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "note": {"late"}, "qty": {"10"}}
	if rec := e.post(t, e.docURL()+"/lines/1", line); rec.Code != http.StatusConflict {
		t.Errorf("stale line save: status = %d, want 409", rec.Code)
	}
}

// A record the user can't read stays hidden on real AUTH: no step, nor the
// Check page, shows its problems, and a POST writes nothing.
func TestStepScope_HiddenRecordShowsNoProblems(t *testing.T) {
	e, open, restricted := newScopeRealEnv(t)
	m := e.fx.M
	res, err := e.d.Engine.Do(t.Context(), open, scopeType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": itoa(m.Party["RAJKOT"]), "project_id": itoa(m.Project["PRJ-METRO"]), "ref": "old",
	}})
	if err != nil {
		t.Fatalf("create the draft: %v", err)
	}
	e.id = res.ID
	e.disable(t, "party", "RAJKOT")

	// The open user sees the problem, so its absence below is the hiding.
	if text := wizardText(t, e.get(t, e.stepURL("details"))); !strings.Contains(text, scopeSupplierMsg) {
		t.Errorf("the open user doesn't see %q; text %q", scopeSupplierMsg, text)
	}

	e.who = restricted
	for _, slug := range []string{"details", "delivery", "check"} {
		rec := e.get(t, e.stepURL(slug))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", slug, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "is disabled") {
			t.Errorf("GET %s shows a hidden record's problem", slug)
		}
	}
	if rec := e.post(t, e.stepURL("delivery"), e.deliveryForm(1, "typed")); rec.Code != http.StatusNotFound {
		t.Errorf("POST: status = %d, want 404", rec.Code)
	}
	if got := e.record(t); got.Values["ref"] != "old" || got.Version != 1 {
		t.Errorf("stored ref %q at version %d, want old at 1", got.Values["ref"], got.Version)
	}
}

// stepPost and the line handlers tell DOC their scope; create and submit
// don't. Exact names, on the recording fake.
func TestStepScope_HandlersSetOnly(t *testing.T) {
	ok := doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}
	t.Run("step fields", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1", "deliver_to": "Gate 1"}})
		e.docs.SetResult(shipType, 60, doctype.ActionSave, doc.Result{ID: 60, State: doctype.StateDraft, Version: 4}, nil)
		rec := e.post(t, "/doc/"+shipType+"/60/edit/delivery", url.Values{"deliver_on": {"24-10-2026"}, "deliver_to": {"Gate 2"}})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status = %d, want 303; body %.300s", rec.Code, rec.Body)
		}
		calls := e.docs.Calls()
		if len(calls) != 1 {
			t.Fatalf("engine calls = %+v, want one", calls)
		}
		got := slices.Sorted(slices.Values(calls[0].Input.Only))
		if !slices.Equal(got, []string{"deliver_on", "deliver_to"}) {
			t.Errorf("Only = %q, want the step's fields", got)
		}
	})
	for _, c := range []struct {
		name, url string
		form      url.Values
	}{
		{"line page", doc42 + "/lines/2", url.Values{"version": {"6"}, "item_id": {"4"}, "qty": {"5"}, "rate": {"10"}}},
		{"new line", doc42 + "/lines/new", url.Values{"version": {"6"}, "item_id": {"5"}, "qty": {"5"}, "rate": {"10"}}},
		{"move", doc42 + "/lines/2/move?dir=up", url.Values{"version": {"6"}}},
		{"remove", doc42 + "/lines/2/remove", url.Values{"version": {"6"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLinesEnv(t, threeLines())
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
			e.post(t, c.url, c.form)
			if got := oneSave(t, e).Only; !slices.Equal(got, []string{"lines"}) {
				t.Errorf("Only = %q, want [lines]", got)
			}
		})
	}
	t.Run("create", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.az.Grant("Buyer", shipType, "create", auth.ScopeAll)
		e.docs.SetResult(shipType, 0, doctype.ActionSave, doc.Result{ID: 60, State: doctype.StateDraft, Version: 1}, nil)
		e.post(t, "/doc/"+shipType+"/new", url.Values{"ref": {"SHP-1"}})
		calls := e.docs.Calls()
		if len(calls) != 1 || calls[0].Input.Only != nil {
			t.Errorf("engine calls = %+v, want one create with a nil Only", calls)
		}
	})
	t.Run("submit", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{ID: 42, State: doctype.StateApproved, Version: 4}, nil)
		e.post(t, doc42+"/submit", nil)
		calls := e.docs.Calls()
		if len(calls) != 1 || calls[0].Input.Only != nil {
			t.Errorf("engine calls = %+v, want one submit with a nil Only", calls)
		}
	})
}

// An unknown name in Only is a caller bug: Classify can't place it, so the
// handlers answer 500 through web.ErrorFrom, never a page with a message.
func TestStepScope_UnknownOnlyNameIs500ByClassify(t *testing.T) {
	e := newScopeEnv(t)
	in := e.fx.In(e.fx.Load(t, scopeType, e.id))
	in.Values["ref"] = "typed"
	in.Only = []string{"nope"}
	_, err := e.fx.Engine.Do(t.Context(), e.who, scopeType, e.id, doctype.ActionSave, in)
	if err == nil {
		t.Fatal("save with an unknown Only name succeeded")
	}
	if pr, ok := docscreen.Classify(err); ok {
		t.Errorf("Classify(%v) = %+v, true; want false (500)", err, pr)
	}
}
