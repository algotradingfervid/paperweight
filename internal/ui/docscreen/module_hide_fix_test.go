package docscreen_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-1b review fixes (CD-256 (1), (10), (11)).

// newPost's 422 redraw: an engine refusal of a field the module hides shows
// as plain text, with no link to the input that isn't there. The refusal is
// CD-178's: a Required SiteFields field left empty by a user restricted on
// sites.
func TestModule_HideNewPostErrors(t *testing.T) {
	e := m01bSiteEnv(t)
	m01bMount(e, m01bSite, docscreen.Module{Hide: m01bHide("site_id")})
	const msg = "Site is required."
	rec := e.post(t, "/doc/"+m01bSite+"/new", url.Values{"supplier_id": {itoa(e.fx.M.Party["SHREE"])}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST /new without the hidden Required site = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if m01bNamed(page, "site_id") != 0 {
		t.Errorf("the redraw renders the hidden site_id")
	}
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, msg) {
		t.Errorf("error summary = %q, want %q", got, msg)
	}
	if links := summaryLinks(page); slices.Contains(links, "#"+components.FieldID("site_id")) {
		t.Errorf("error summary links %q, want no link to the hidden site", links)
	}
	wantNoDeadLinks(t, rec)
}

// m01bSite is a test DocType whose site_id is Required and in SiteFields, so
// a site-restricted user's draft without it is refused (CD-178).
const m01bSite = "docscreentest.M01bSite"

var m01bSiteOnce sync.Once

// m01bSiteEnv is the Hooked harness with the Hooker role given every right
// on m01bSite on the real AUTH service, acting as the site-restricted user.
func m01bSiteEnv(t *testing.T) *hookedEnv {
	t.Helper()
	m01bSiteOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m01bSite, Label: "Site Order", PluralLabel: "Site Orders", Table: "m01b_site", LinesTable: "m01b_site_line",
			Submittable: true, Hooks: scopeHooks{}, SiteFields: []string{"site_id"},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: fixture.Party},
				{Name: "site_id", Label: "Site", Type: doctype.Link, Link: "Site", Required: true},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Positive: true, OnLine: true},
			},
		})
	})
	e := newHookedEnv(t)
	e.fx.Exec(t, `CREATE TABLE m01b_site (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		supplier_id INTEGER, site_id INTEGER)`)
	e.fx.Exec(t, `CREATE TABLE m01b_site_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		item_id INTEGER, qty INTEGER)`)
	e.fx.Naming.Prefix[m01bSite] = "MS"
	ctx := t.Context()
	a, ok := e.d.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Deps.Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	all := map[auth.Right]auth.Scope{}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "delete"} {
		all[r] = auth.ScopeAll
	}
	if err := a.SetRights(ctx, e.fx.Admin, "Hooker", m01bSite, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	p, _, err := a.LoadPrincipal(ctx, e.restricted.ID)
	if err != nil {
		t.Fatalf("LoadPrincipal: %v", err)
	}
	e.restricted, e.who = p, p
	return e
}

// A line POST by a user who can't read the draft gets the same answer as a
// step POST, writes nothing, and no hook sees the draft.
func TestModule_LinePostNonReader(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	hide := func(docscreen.Ctx, doctype.Field) bool {
		mu.Lock()
		calls++
		mu.Unlock()
		return false
	}
	e, id := m01bHooked(t, docscreen.Module{Hide: hide})
	base := m01bURL(id)
	version := m01bVersion(t, e, id)
	e.who = e.restricted
	step := e.post(t, base+"/edit/terms", url.Values{"version": {version}, "remarks": {"typed"}})
	if step.Code != http.StatusNotFound {
		t.Fatalf("step POST as a non-reader = %d, want 404", step.Code)
	}
	want := norm(uitest.Doc(t, step).Find("main").Text())
	for _, path := range []string{"/lines/1", "/lines/new"} {
		line := e.post(t, base+path, e.m01bLine1(version))
		if got := norm(uitest.Doc(t, line).Find("main").Text()); line.Code != step.Code || got != want {
			t.Errorf("POST %s as a non-reader = %d %q, want the step POST's %d %q", path, line.Code, got, step.Code, want)
		}
	}
	if lines := m01bRecord(t, e, id).Lines; len(lines) != 2 || lines[0]["qty"] != "10.000" {
		t.Errorf("stored lines = %q after a non-reader's posts, want the two seeded", lines)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 0 {
		t.Errorf("Hide was called %d times for a draft the user can't read, want 0", calls)
	}
}

// The grid row answers (a save and a refusal) use the module's LineCard, as
// the cards do.
func TestModule_GridLineCard(t *testing.T) {
	const amount = "M01B-AMT"
	card := func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
		c := l.Card
		c.Amount = amount
		return c
	}
	e, id := m01bHooked(t, docscreen.Module{LineCard: card})
	base := m01bURL(id)
	saved := m01bOK(t, e.post(t, base+"/lines/1", e.m01bLine1(m01bVersion(t, e, id)), "HX-Request", "true"), "grid row save")
	if !strings.Contains(norm(saved.Text()), amount) {
		t.Errorf("the grid row save's answer has no %s: %q", amount, norm(saved.Text()))
	}
	form := e.m01bLine1(m01bVersion(t, e, id))
	form.Set("qty", "abc")
	rec := e.post(t, base+"/lines/1", form, "HX-Request", "true")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("grid post with qty abc = %d, want 422", rec.Code)
	}
	if got := norm(uitest.Doc(t, rec).Text()); !strings.Contains(got, amount) {
		t.Errorf("the refused grid row has no %s: %q", amount, got)
	}
}

// CD-256 (10): on the desktop grid Hide's Deps carries each row's stored
// line inputs, so a stored line link keeps its label.
func TestModule_GridHideLineDeps(t *testing.T) {
	var mu sync.Mutex
	labelled := map[string]bool{}
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if f.Name != "project_id" || c.View == nil || c.Req.Method != http.MethodGet {
			return false
		}
		for _, l := range c.View.Lines {
			var b strings.Builder
			if err := docscreen.FieldInput(f, "m01b-probe", l.Inputs["project_id"], "", c.Deps).Render(c.Req.Context(), &b); err != nil {
				t.Errorf("FieldInput: %v", err)
			}
			for _, code := range []string{"PRJ-METRO", "PRJ-TECHPARK"} {
				if strings.Contains(b.String(), code) {
					mu.Lock()
					labelled[code] = true
					mu.Unlock()
				}
			}
		}
		return false
	}
	e, id := m01bHooked(t, docscreen.Module{Hide: hide})
	m01bOK(t, e.get(t, m01bURL(id)+"/edit/items"), "GET items")
	mu.Lock()
	defer mu.Unlock()
	for _, code := range []string{"PRJ-METRO", "PRJ-TECHPARK"} {
		if !labelled[code] {
			t.Errorf("no grid row's Hide Deps labels %s: Deps must carry the row's stored line inputs", code)
		}
	}
}
