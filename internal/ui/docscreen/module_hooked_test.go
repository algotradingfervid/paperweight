package docscreen_test

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	sitemaster "github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// hookedEnv is the docscreen harness for docscreentest.Hooked on a fixture
// database, the real engine and the real AUTH service (CD-125): an open
// user, and a user restricted to TECH-SITE, both with read, create, write,
// submit and delete on Hooked.
type hookedEnv struct {
	*scopeEnv
	engine           doc.Engine
	open, restricted auth.Principal
	site             map[string]int64 // fixture site id by code
}

func newHookedEnv(t *testing.T) *hookedEnv {
	t.Helper()
	docscreentest.RegisterHooked()
	h := fixturetest.New(t)
	for _, q := range docscreentest.HookedSchema {
		h.Exec(t, q)
	}
	h.Naming.Prefix = map[string]string{docscreentest.Hooked: "HK"}
	site := map[string]int64{}
	for _, s := range h.F.Sites {
		site[s.Code] = s.ID
	}
	if site["TECH-SITE"] == 0 || site["METRO-SITE"] == 0 {
		t.Fatalf("fixture sites = %v, want TECH-SITE and METRO-SITE seeded", site)
	}
	ctx := t.Context()
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	admin := h.Admin
	if err := a.CreateRole(ctx, admin, "Hooker"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	all := map[auth.Right]auth.Scope{}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "delete"} {
		all[r] = auth.ScopeAll
	}
	if err := a.SetRights(ctx, admin, "Hooker", docscreentest.Hooked, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	load := func(email string) auth.Principal {
		t.Helper()
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: scopePassword, Roles: []string{"Hooker"}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		if email == "hooked-restricted@vardhan.test" {
			if err := a.ChangeRestrictions(ctx, admin, id, auth.DimSite, []int64{site["TECH-SITE"]}, nil, nil); err != nil {
				t.Fatalf("ChangeRestrictions: %v", err)
			}
		}
		p, _, err := a.LoadPrincipal(ctx, id)
		if err != nil {
			t.Fatalf("LoadPrincipal(%s): %v", email, err)
		}
		return p
	}
	open, restricted := load("hooked-open@vardhan.test"), load("hooked-restricted@vardhan.test")
	engine := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	return &hookedEnv{scopeEnv: scopeMount(t, h, engine, a, open), engine: engine, open: open, restricted: restricted, site: site}
}

// draft saves a new Hooked draft as p on site code, with two lines.
func (e *hookedEnv) draft(t *testing.T, p auth.Principal, siteCode string) (doc.Result, error) {
	t.Helper()
	m := e.fx.M
	return e.engine.Do(t.Context(), p, docscreentest.Hooked, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": itoa(m.Party["SHREE"]), "site_id": itoa(e.site[siteCode]), "ref": "HK-REF", "remarks": "hooked remarks",
		},
		Lines: []map[string]string{
			{"project_id": itoa(m.Project["PRJ-METRO"]), "item_id": itoa(m.Item["CEMENT-OPC"]), "qty": "10.000", "note": "first"},
			{"project_id": itoa(m.Project["PRJ-TECHPARK"]), "item_id": itoa(m.Item["WIRE-18G"]), "qty": "5.000", "note": "second"},
		},
	})
}

// M0-1: docscreentest.Hooked has the shape later tasks test on (the status
// file of the M0-1 test-writer lists it): wizard steps details, terms, items,
// check; site_id on details, a Link to the Site master (so its display value
// differs from its raw id, hooks spec §4.3) and the SiteFields; a line whose
// first Link is a project, not the item.
func TestModule_HookedShape(t *testing.T) {
	docscreentest.RegisterHooked()
	docscreentest.RegisterHooked() // a second call is a no-op
	if docscreentest.Hooked != "docscreentest.Hooked" {
		t.Fatalf("Hooked = %q, want docscreentest.Hooked", docscreentest.Hooked)
	}
	dt, ok := doctype.Lookup(docscreentest.Hooked)
	if !ok {
		t.Fatal("docscreentest.Hooked is not registered")
	}
	if !dt.Submittable || dt.LinesTable == "" || dt.Hooks == nil {
		t.Errorf("Submittable %v, LinesTable %q, Hooks %v; want a submittable DocType with lines and hooks", dt.Submittable, dt.LinesTable, dt.Hooks)
	}
	if !slices.Equal(dt.SiteFields, []string{"site_id"}) {
		t.Errorf("SiteFields = %q, want [site_id]", dt.SiteFields)
	}
	steps, err := docscreen.FormSteps(dt)
	if err != nil {
		t.Fatalf("FormSteps: %v", err)
	}
	var slugs []string
	fields := map[string][]string{}
	for _, s := range steps {
		slugs = append(slugs, s.Slug)
		fields[s.Slug] = s.Fields
	}
	if want := []string{"details", "terms", "items", "check"}; !slices.Equal(slugs, want) {
		t.Fatalf("steps = %q, want %q", slugs, want)
	}
	if want := []string{"supplier_id", "site_id"}; !slices.Equal(fields["details"], want) {
		t.Errorf("details fields = %q, want %q", fields["details"], want)
	}
	if f, ok := dt.Field("site_id"); !ok || f.Type != doctype.Link || f.Link != sitemaster.DocType {
		t.Errorf("site_id = %+v, want a Link to %q", f, sitemaster.DocType)
	}
	if want := []string{"ref", "remarks"}; !slices.Equal(fields["terms"], want) {
		t.Errorf("terms fields = %q, want %q", fields["terms"], want)
	}
	var lineNames, lineLinks []string
	for _, f := range dt.LineFields() {
		lineNames = append(lineNames, f.Name)
		if f.Type == doctype.Link {
			lineLinks = append(lineLinks, f.Link)
		}
	}
	if want := []string{"project_id", "item_id", "qty", "note"}; !slices.Equal(lineNames, want) {
		t.Errorf("line fields = %q, want %q", lineNames, want)
	}
	if want := []string{fixture.Project, fixture.Item}; !slices.Equal(lineLinks, want) {
		t.Errorf("line links = %q, want %q: the first Link is not the item", lineLinks, want)
	}
}

// M0-1: Hooked runs on the real engine and real AUTH: a draft saved with its
// lines in one call renders every wizard step, its line page and, once
// submitted, its document page; site_id shows the site's code, not its id; a
// site-restricted user gets a 404 for the open user's draft on another site,
// AUTH's ValueError (ErrForbidden) creating one there, and may create and
// open a draft on their own site.
func TestModule_HookedReal(t *testing.T) {
	e := newHookedEnv(t)
	res, err := e.draft(t, e.open, "METRO-SITE")
	if err != nil {
		t.Fatalf("saving a Hooked draft: %v", err)
	}
	rec := e.fx.Record(t, docscreentest.Hooked, res.ID)
	if len(rec.Lines) != 2 || rec.Values["ref"] != "HK-REF" || rec.Values["site_id"] != itoa(e.site["METRO-SITE"]) {
		t.Fatalf("stored record = %+v, want ref HK-REF on METRO-SITE with two lines", rec)
	}
	base := "/doc/" + docscreentest.Hooked + "/" + itoa(res.ID)
	for _, path := range []string{"/edit/details", "/edit/terms", "/edit/items", "/edit/check", "/lines/1", "/lines/new"} {
		r := e.get(t, base+path)
		if r.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200; body %.300s", path, r.Code, r.Body)
		}
	}
	if body := e.get(t, base+"/edit/terms").Body.String(); !strings.Contains(body, "HK-REF") {
		t.Errorf("terms step does not show the stored ref HK-REF")
	}
	v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, res.ID)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if got := v.Display["site_id"]; got == itoa(e.site["METRO-SITE"]) || !strings.Contains(got, "METRO-SITE") {
		t.Errorf("site_id display = %q, want the site's code METRO-SITE, not its id %d", got, e.site["METRO-SITE"])
	}

	e.who = e.restricted
	for _, path := range []string{"/edit/details", ""} {
		if r := e.get(t, base+path); r.Code != http.StatusNotFound || strings.Contains(r.Body.String(), "HK-REF") {
			t.Errorf("restricted user GET %q on a METRO-SITE draft = %d, want 404 without its values", base+path, r.Code)
		}
	}
	_, err = e.draft(t, e.restricted, "METRO-SITE")
	var ve *auth.ValueError
	if !errors.As(err, &ve) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("restricted user creating on METRO-SITE: err = %v (%T), want AUTH's *auth.ValueError, ErrForbidden", err, err)
	}
	own, err := e.draft(t, e.restricted, "TECH-SITE")
	if err != nil {
		t.Fatalf("restricted user saving a TECH-SITE draft: %v", err)
	}
	if r := e.get(t, "/doc/"+docscreentest.Hooked+"/"+itoa(own.ID)+"/edit/details"); r.Code != http.StatusOK {
		t.Errorf("restricted user's own TECH-SITE draft = %d, want 200", r.Code)
	}

	e.who = e.open
	sub, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("submitting the Hooked draft: %v", err)
	}
	if r := e.get(t, base); r.Code != http.StatusOK {
		t.Errorf("document page in state %s = %d, want 200; body %.300s", sub.State, r.Code, r.Body)
	}
}
