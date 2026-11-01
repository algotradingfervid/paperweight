package docscreen_test

import (
	"context"
	"html"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-2 (hooks spec §4.1; CD-256 (2), (3), (4), (8), (10), (12)): Step.Body,
// Step.Save, the first step, StepLabels and StepState.Defaults. Every test
// runs on docscreentest.Hooked on a fixture database, the real engine and the
// real AUTH service (CD-125). The engine is wrapped by m02Spy, which forwards
// every call and records each Do, so a test sees exactly what docscreen asked
// the engine to write.

// m02Typed is what a test types into the Body's own input (≤ 12 characters,
// Hooked's ref MaxLen).
const m02Typed = "M02-TYPED"

// m02Call is one Engine.Do call docscreen made.
type m02Call struct {
	ID     int64
	Action doc.Action
	In     doc.ActionInput
}

// m02Spy is the real engine, recording each Do.
type m02Spy struct {
	doc.Engine
	mu    sync.Mutex
	calls []m02Call
}

func (s *m02Spy) Do(ctx context.Context, p auth.Principal, name string, id int64, a doc.Action, in doc.ActionInput) (doc.Result, error) {
	s.mu.Lock()
	s.calls = append(s.calls, m02Call{ID: id, Action: a, In: in})
	s.mu.Unlock()
	return s.Engine.Do(ctx, p, name, id, a, in)
}

// take returns the calls since the last take and forgets them.
func (s *m02Spy) take() []m02Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.calls
	s.calls = nil
	return out
}

// m02BodyCall is what one Body call saw.
type m02BodyCall struct {
	Method, Path string
	View         bool
	State        docscreen.StepState
}

// m02SaveCall is what one Save call saw.
type m02SaveCall struct {
	Method, Path string
	View         *docscreen.DocView
	Principal    auth.UserID
}

// m02Log records the hooks' calls.
type m02Log struct {
	mu     sync.Mutex
	bodies []m02BodyCall
	saves  []m02SaveCall
}

func (l *m02Log) sawBody(c docscreen.Ctx, s docscreen.StepState) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bodies = append(l.bodies, m02BodyCall{Method: c.Req.Method, Path: c.Req.URL.Path, View: c.View != nil, State: s})
}

func (l *m02Log) sawSave(c docscreen.Ctx) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.saves = append(l.saves, m02SaveCall{Method: c.Req.Method, Path: c.Req.URL.Path, View: c.View, Principal: c.Principal.ID})
}

func (l *m02Log) bodyCalls() []m02BodyCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.bodies)
}

func (l *m02Log) saveCalls() []m02SaveCall {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.saves)
}

// m02Defaults reads StepState.Defaults (CD-256 (4)) by name, so this file
// compiles before the field exists; ok is false while it doesn't.
func m02Defaults(s docscreen.StepState) (map[string]string, bool) {
	f := reflect.ValueOf(s).FieldByName("Defaults")
	if !f.IsValid() {
		return nil, false
	}
	m, ok := f.Interface().(map[string]string)
	return m, ok
}

// m02Body is a custom step body: #m02-body with data-step, data-posted (the
// StepState's Posted) and data-site (Defaults["site_id"]), one input
// m02_ref showing the typed value on a redraw and the stored ref otherwise,
// and #m02-errors listing StepState.Errors.
func (l *m02Log) m02Body(slug string) func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) {
	return func(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
		l.sawBody(c, s)
		value := ""
		switch {
		case s.Posted:
			value = c.Req.PostFormValue("m02_ref")
		case c.View != nil:
			value = c.View.Inputs["ref"]
		}
		defaults, _ := m02Defaults(s)
		var b strings.Builder
		b.WriteString(`<div id="m02-body" data-step="` + slug + `" data-posted="` + strconv.FormatBool(s.Posted) + `" data-site="` + html.EscapeString(defaults["site_id"]) + `">`)
		b.WriteString(`<label for="m02-ref">M02 reference</label><input id="m02-ref" name="m02_ref" value="` + html.EscapeString(value) + `"/>`)
		b.WriteString(`<ul id="m02-errors">`)
		for _, e := range s.Errors {
			b.WriteString(`<li>` + html.EscapeString(e.Message) + `</li>`)
		}
		b.WriteString(`</ul></div>`)
		out := b.String()
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, out)
			return err
		}), nil
	}
}

// m02Env is the Hooked harness with the engine behind m02Spy and one draft
// by the open user on METRO-SITE (ref HK-REF, remarks "hooked remarks"; line
// 1 PRJ-METRO, CEMENT-OPC, 10, "first"; line 2 PRJ-TECHPARK, WIRE-18G, 5,
// "second"). It has no module yet: m02Mount gives it one.
func m02Env(t *testing.T) (*hookedEnv, *m02Spy, int64) {
	t.Helper()
	e := newHookedEnv(t)
	res, err := e.draft(t, e.open, "METRO-SITE")
	if err != nil {
		t.Fatalf("saving the Hooked draft: %v", err)
	}
	spy := &m02Spy{Engine: e.d.Engine}
	e.d.Engine = spy
	e.remount()
	return e, spy, res.ID
}

// m02Mount gives Hooked the module m and rebuilds the router.
func m02Mount(e *hookedEnv, m docscreen.Module) {
	e.d.Modules = map[string]docscreen.Module{docscreentest.Hooked: m}
	e.remount()
}

func m02URL(id int64) string { return "/doc/" + docscreentest.Hooked + "/" + itoa(id) }

const m02NewURL = "/doc/" + docscreentest.Hooked + "/new"

func m02Rec(t *testing.T, e *hookedEnv, id int64) doc.Record {
	t.Helper()
	return e.fx.Record(t, docscreentest.Hooked, id)
}

func m02Version(t *testing.T, e *hookedEnv, id int64) string {
	t.Helper()
	return strconv.Itoa(m02Rec(t, e, id).Version)
}

// m02Page fails unless rec has status code and returns its page.
func m02Page(t *testing.T, rec *httptest.ResponseRecorder, code int, what string) *goquery.Document {
	t.Helper()
	if rec.Code != code {
		t.Fatalf("%s = %d, want %d; body %.500s", what, rec.Code, code, rec.Body)
	}
	return uitest.Doc(t, rec)
}

// m02Named counts the elements posting name.
func m02Named(sel *goquery.Selection, name string) int {
	return sel.Find(`[name="` + name + `"]`).Length()
}

// m02Count is the number of Hooked documents stored.
func m02Count(t *testing.T, e *hookedEnv) int {
	t.Helper()
	var n int
	err := e.fx.DB.Read(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM docscreentest_hooked`).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting Hooked documents: %v", err)
	}
	return n
}

// m02Sorted is a sorted copy of names.
func m02Sorted(names []string) []string {
	out := slices.Clone(names)
	slices.Sort(out)
	return out
}

// m02WantUnchanged fails unless Hooked id still equals before and has no new
// audit entry.
func m02WantUnchanged(t *testing.T, e *hookedEnv, id int64, before doc.Record, audits int, what string) {
	t.Helper()
	if got := m02Rec(t, e, id); !reflect.DeepEqual(got, before) {
		t.Errorf("%s wrote the draft: %+v, want %+v unchanged", what, got, before)
	}
	if got := len(e.fx.Audit(t, docscreentest.Hooked, id)); got != audits {
		t.Errorf("%s: %d audit entries, want %d (nothing written)", what, got, audits)
	}
}

// m02BodyOf is the custom body in sel, failing unless there is exactly one.
func m02BodyOf(t *testing.T, sel *goquery.Selection, what string) *goquery.Selection {
	t.Helper()
	b := sel.Find("#m02-body")
	if b.Length() != 1 {
		t.Fatalf("%s has %d #m02-body, want the custom Body once", what, b.Length())
	}
	return b
}

// m02Attr is sel's attribute name, or "<none>".
func m02Attr(sel *goquery.Selection, name string) string {
	v, ok := sel.Attr(name)
	if !ok {
		return "<none>"
	}
	return v
}

// m02Typing is a custom step post typing value into the Body's input.
func m02Typing(version, value string) url.Values {
	return url.Values{"version": {version}, "m02_ref": {value}}
}

// HOOK-002: a custom Body replaces the step's inputs inside the step form, in
// the full page and the htmx frame; the Stepper, Bar, #doc-version, Back,
// Save and finish later, return=check and the error summary stay docscreen's.
// The first step has no Back. On the Items step Body also replaces M.After,
// so its inputs are in the form and the generic lines region is gone.
func TestModule_StepBody(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m02Log{}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
		"details": {Body: log.m02Body("details")},
		"terms": {Body: log.m02Body("terms"), Save: func(c docscreen.Ctx) (docscreen.Save, error) {
			return docscreen.Save{Errors: []doctype.FieldError{{Field: "ref", Message: "M02 reference is not right."}}}, nil
		}},
	}})
	base := m02URL(id)
	version := m02Version(t, e, id)

	wantFrame := func(t *testing.T, page *goquery.Document, what string) {
		t.Helper()
		form := stepForm(page, base+"/edit/terms")
		if form.Length() != 1 {
			t.Fatalf("%s: no step form posting to %s/edit/terms", what, base)
		}
		body := m02BodyOf(t, form, what+" step form")
		if got := m02Attr(body, "data-step"); got != "terms" {
			t.Errorf("%s: Body rendered for step %q, want terms", what, got)
		}
		if got := m02Attr(form.Find("#m02-ref"), "value"); got != "HK-REF" {
			t.Errorf("%s: Body input = %q, want the stored ref HK-REF (c.View on a GET)", what, got)
		}
		for _, name := range []string{"ref", "remarks"} {
			if n := m02Named(page.Selection, name); n != 0 {
				t.Errorf("%s: %d generic %s inputs, want none: Body replaces the step's inputs", what, n, name)
			}
		}
		if got := stepCount(page.Selection); got != "Step 2 of 4" {
			t.Errorf("%s: stepper = %q, want Step 2 of 4", what, got)
		}
		if got := norm(page.Find("h2#step-title").Text()); got != "Terms" {
			t.Errorf("%s: heading = %q, want Terms", what, got)
		}
		if got := m02Attr(form.Find("#doc-version"), "value"); got != version {
			t.Errorf("%s: #doc-version in the step form = %q, want %s", what, got, version)
		}
		if n := form.Find(`button[name="nav"][value="back"]`).Length(); n != 1 {
			t.Errorf("%s: %d Back buttons in the step form, want 1", what, n)
		}
		if n := form.Find(`button[name="nav"][value="finish"]`).Length(); n != 1 {
			t.Errorf("%s: %d Save and finish later buttons in the step form, want 1", what, n)
		}
		if got := norm(form.Find(`button[type="submit"]:not([name])`).Text()); got != "Save and continue" {
			t.Errorf("%s: primary button = %q, want Save and continue", what, got)
		}
		if strings.Contains(norm(page.Find("#wizard").Text()), "Nothing needed here") {
			t.Errorf("%s: a Body step shows Nothing needed here", what)
		}
	}

	t.Run("full page", func(t *testing.T) {
		page := m02Page(t, e.get(t, base+"/edit/terms"), http.StatusOK, "GET terms")
		if page.Find("html head title").Length() != 1 || page.Find("main #wizard").Length() != 1 {
			t.Fatalf("GET terms is not the full page with #wizard in main")
		}
		wantFrame(t, page, "full page")
		if n := m02Named(page.Selection, "return"); n != 0 {
			t.Errorf("a plain GET has %d return inputs, want none", n)
		}
	})
	t.Run("htmx frame", func(t *testing.T) {
		rec := e.get(t, base+"/edit/terms", "HX-Request", "true")
		page := m02Page(t, rec, http.StatusOK, "htmx GET terms")
		if got := rootIDs(t, rec); !slices.Equal(got, []string{"wizard"}) {
			t.Fatalf("htmx GET terms roots = %q, want only #wizard", got)
		}
		wantFrame(t, page, "htmx frame")
	})
	t.Run("return=check", func(t *testing.T) {
		page := m02Page(t, e.get(t, base+"/edit/terms?return=check"), http.StatusOK, "GET terms?return=check")
		form := stepForm(page, base+"/edit/terms")
		if got := m02Attr(form.Find(`input[type="hidden"][name="return"]`), "value"); got != "check" {
			t.Errorf("return input in the step form = %q, want check", got)
		}
		m02BodyOf(t, form, "the return=check step form")
	})
	t.Run("error summary", func(t *testing.T) {
		rec := e.post(t, base+"/edit/terms", m02Typing(version, "typed"))
		page := m02Page(t, rec, http.StatusUnprocessableEntity, "POST terms with Save.Errors")
		if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, "M02 reference is not right.") {
			t.Errorf("error summary = %q, want the Save error", got)
		}
		m02BodyOf(t, stepForm(page, base+"/edit/terms"), "the 422 step form")
	})
	t.Run("first step has no Back", func(t *testing.T) {
		page := m02Page(t, e.get(t, base+"/edit/details"), http.StatusOK, "GET details")
		form := stepForm(page, base+"/edit/details")
		if got := m02Attr(m02BodyOf(t, form, "details step form"), "data-step"); got != "details" {
			t.Errorf("details Body for step %q", got)
		}
		if n := page.Find(`button[name="nav"][value="back"]`).Length(); n != 0 {
			t.Errorf("first step has %d Back buttons, want none", n)
		}
		if n := m02Named(page.Selection, "supplier_id") + m02Named(page.Selection, "site_id"); n != 0 {
			t.Errorf("details step has %d generic inputs, want none", n)
		}
	})
	t.Run("items step", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: log.m02Body("items")}}})
		for _, hx := range []bool{false, true} {
			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.get(t, base+"/edit/items", hdr...)
			page := m02Page(t, rec, http.StatusOK, "GET items")
			what := "items step (htmx " + strconv.FormatBool(hx) + ")"
			form := stepForm(page, base+"/edit/items")
			if form.Length() != 1 {
				t.Fatalf("%s: no step form", what)
			}
			if form.Find("#m02-ref").Length() != 1 {
				t.Errorf("%s: the Body's input is not inside the step form", what)
			}
			m02BodyOf(t, page.Selection, what)
			if n := page.Find(`#grid-alerts, #doc-totals, #line-editor, a[href$="/lines/new"]`).Length(); n != 0 {
				t.Errorf("%s: %d elements of the generic lines region, want none: Body replaces M.After", what, n)
			}
			if n := form.Find(`button[name="nav"][value="back"]`).Length(); n != 1 {
				t.Errorf("%s: %d Back buttons inside the step form, want 1", what, n)
			}
			if got := m02Attr(form.Find("#doc-version"), "value"); got != version {
				t.Errorf("%s: #doc-version = %q, want %s", what, got, version)
			}
			if hx {
				if got := rootIDs(t, rec); !slices.Equal(got, []string{"wizard"}) {
					t.Errorf("%s: roots = %q, want only #wizard", what, got)
				}
			}
		}
	})
	t.Run("Ctx.Deps has the stored inputs", func(t *testing.T) {
		// CD-256 (10): a Body rendering FieldInput for the stored site gets its
		// label through c.Deps (WithStored); the harness has no LinkRead.
		dt, _ := doctype.Lookup(docscreentest.Hooked)
		site, _ := dt.Field("site_id")
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: func(c docscreen.Ctx, _ docscreen.StepState) (templ.Component, error) {
			if c.View == nil {
				return nil, errFor("details Body got no View")
			}
			return docscreen.FieldInput(site, "m02-site", c.View.Inputs["site_id"], "", c.Deps), nil
		}}}})
		page := m02Page(t, e.get(t, base+"/edit/details"), http.StatusOK, "GET details")
		if got := norm(stepForm(page, base+"/edit/details").Text()); !strings.Contains(got, "METRO-SITE") {
			t.Errorf("details Body's FieldInput with c.Deps = %q, want the stored site's label METRO-SITE", got)
		}
	})
}

// errFor is a plain error.
type errFor string

func (e errFor) Error() string { return string(e) }

// HOOK-003: a custom Save's Values and Lines reach Engine.Do in one call with
// the page's posted version and Only = the step's fields plus "lines"; the
// Save sees the loaded View (CD-256 (2)). Only is real: a disabled supplier on
// another step doesn't stop the terms save, and a disabled item on a line the
// Save sends does. A stale posted version is DOC's 409: the posted version is
// used, not a reloaded one.
func TestModule_StepSave(t *testing.T) {
	e, spy, id := m02Env(t)
	m := e.fx.M
	log := &m02Log{}
	item := "BOLT-M12"
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		log.sawSave(c)
		return docscreen.Save{
			Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref"), "remarks": "m02 remarks"},
			Lines:  []map[string]string{{"project_id": itoa(m.Project["PRJ-TECHPARK"]), "item_id": itoa(m.Item[item]), "qty": "2.000", "note": "m02 line"}},
		}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	base := m02URL(id)
	e.disable(t, "party", "SHREE") // the details step's supplier: out of the terms save's scope

	before := m02Rec(t, e, id)
	spy.take()
	rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), m02Typed))
	if got := location(t, rec); got != base+"/edit/items" {
		t.Errorf("terms save went to %q, want the items step", got)
	}
	calls := spy.take()
	if len(calls) != 1 {
		t.Fatalf("Engine.Do calls = %+v, want exactly one", calls)
	}
	c := calls[0]
	if c.ID != id || c.Action != doc.ActionSave || c.In.Version != before.Version {
		t.Errorf("Engine.Do(id %d, %s, version %d), want (%d, save, the posted %d)", c.ID, c.Action, c.In.Version, id, before.Version)
	}
	if want := map[string]string{"ref": m02Typed, "remarks": "m02 remarks"}; !maps.Equal(c.In.Values, want) {
		t.Errorf("Engine.Do Values = %v, want Save.Values %v", c.In.Values, want)
	}
	if len(c.In.Lines) != 1 || c.In.Lines[0]["note"] != "m02 line" || c.In.Lines[0]["item_id"] != itoa(m.Item[item]) {
		t.Errorf("Engine.Do Lines = %v, want Save.Lines", c.In.Lines)
	}
	if got, want := m02Sorted(c.In.Only), []string{"lines", "ref", "remarks"}; !slices.Equal(got, want) {
		t.Errorf("Engine.Do Only = %q, want %q (the step's fields plus lines)", c.In.Only, want)
	}

	after := m02Rec(t, e, id)
	if after.Version != before.Version+1 || after.Values["ref"] != m02Typed || after.Values["remarks"] != "m02 remarks" {
		t.Errorf("stored version %d ref %q remarks %q, want %d, %s, m02 remarks", after.Version, after.Values["ref"], after.Values["remarks"], before.Version+1, m02Typed)
	}
	if len(after.Lines) != 1 || after.Lines[0]["note"] != "m02 line" || after.Lines[0]["qty"] != "2.000" {
		t.Errorf("stored lines = %v, want the Save's one line", after.Lines)
	}
	saves := log.saveCalls()
	if len(saves) != 1 || saves[0].View == nil || saves[0].View.Inputs["ref"] != "HK-REF" || saves[0].Principal != e.open.ID {
		t.Errorf("Save calls = %+v, want one, by the open user, seeing the loaded View (ref HK-REF)", saves)
	}

	t.Run("lines in Only", func(t *testing.T) {
		item = "MS-ANGLE-50"
		defer func() { item = "BOLT-M12" }()
		e.disable(t, "item", item)
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), "OTHER"))
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("terms save sending a line with a disabled item = %d, want 422 (lines in Only); body %.300s", rec.Code, rec.Body)
		}
		m02WantUnchanged(t, e, id, before, audits, "the refused save")
	})

	t.Run("stale version", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		spy.take()
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version-1), "STALE"))
		if rec.Code != http.StatusConflict {
			t.Fatalf("terms save with version %d (stored %d) = %d, want 409; body %.300s", before.Version-1, before.Version, rec.Code, rec.Body)
		}
		if calls := spy.take(); len(calls) != 1 || calls[0].In.Version != before.Version-1 {
			t.Errorf("Engine.Do calls = %+v, want one with the posted version %d", calls, before.Version-1)
		}
		m02WantUnchanged(t, e, id, before, audits, "the stale save")
	})
}

// HOOK-004: the first step (details) with Body and Save, as a user restricted
// to TECH-SITE on the real auth.Service: /new's Body gets View nil and
// StepState.Defaults with the single allowed site (CD-245, AUTH-100); the
// POST's Save gets View nil, and its Values and Lines go to Engine.Do with id
// 0 in one call, so the draft is created with its lines and one audit entry.
func TestModule_FirstStepCreatesReal(t *testing.T) {
	e, spy, _ := m02Env(t)
	m := e.fx.M
	tech := itoa(e.site["TECH-SITE"])
	log := &m02Log{}
	var want docscreen.Save
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		log.sawSave(c)
		want = docscreen.Save{
			Values: map[string]string{"supplier_id": c.Req.PostFormValue("m02_supplier"), "site_id": c.Req.PostFormValue("m02_site"), "ref": c.Req.PostFormValue("m02_ref")},
			Lines: []map[string]string{
				{"project_id": itoa(m.Project["PRJ-TECHPARK"]), "item_id": itoa(m.Item["CEMENT-OPC"]), "qty": "4.000", "note": "m02 one"},
				{"project_id": itoa(m.Project["PRJ-TECHPARK"]), "item_id": itoa(m.Item["WIRE-18G"]), "qty": "6.000", "note": "m02 two"},
			},
		}
		return want, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: log.m02Body("details"), Save: save}}})
	e.who = e.restricted

	page := m02Page(t, e.get(t, m02NewURL), http.StatusOK, "GET /new")
	body := m02BodyOf(t, stepForm(page, m02NewURL), "the /new step form")
	if got := m02Attr(body, "data-site"); got != tech {
		t.Errorf("/new Body's Defaults[site_id] = %q, want the single allowed site %s", got, tech)
	}
	bodies := log.bodyCalls()
	if len(bodies) == 0 {
		t.Fatal("GET /new never called the details Body")
	}
	for _, b := range bodies {
		if b.View || b.State.Posted {
			t.Errorf("GET /new Body call %+v, want View nil and Posted false", b)
		}
		defaults, ok := m02Defaults(b.State)
		if !ok {
			t.Fatalf("StepState has no Defaults map[string]string field (CD-256 (4))")
		}
		if !maps.Equal(defaults, map[string]string{"site_id": tech}) {
			t.Errorf("GET /new StepState.Defaults = %v, want {site_id: %s}", defaults, tech)
		}
	}

	count := m02Count(t, e)
	spy.take()
	rec := e.post(t, m02NewURL, url.Values{
		"m02_supplier": {itoa(m.Party["SHREE"])}, "m02_site": {tech}, "m02_ref": {m02Typed},
	})
	to := location(t, rec)
	newID, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(to, "/doc/"+docscreentest.Hooked+"/"), "/edit/terms"), 10, 64)
	if err != nil {
		t.Fatalf("POST /new went to %q, want the new draft's terms step", to)
	}
	calls := spy.take()
	if len(calls) != 1 {
		t.Fatalf("Engine.Do calls = %+v, want exactly one create", calls)
	}
	if c := calls[0]; c.ID != 0 || c.Action != doc.ActionSave || !maps.Equal(c.In.Values, want.Values) || !reflect.DeepEqual(c.In.Lines, want.Lines) {
		t.Errorf("Engine.Do(id %d, %s, Values %v, Lines %v), want (0, save, Save.Values %v, Save.Lines %v)", c.ID, c.Action, c.In.Values, c.In.Lines, want.Values, want.Lines)
	}
	if got := m02Count(t, e); got != count+1 {
		t.Errorf("Hooked documents = %d, want %d: one draft created", got, count+1)
	}
	saves := log.saveCalls()
	if len(saves) != 1 || saves[0].View != nil || saves[0].Principal != e.restricted.ID || saves[0].Method != http.MethodPost {
		t.Errorf("Save calls = %+v, want one POST by the restricted user with View nil", saves)
	}
	stored := m02Rec(t, e, newID)
	if stored.Values["supplier_id"] != itoa(m.Party["SHREE"]) || stored.Values["site_id"] != tech || stored.Values["ref"] != m02Typed {
		t.Errorf("created draft values = %v, want SHREE on TECH-SITE with ref %s", stored.Values, m02Typed)
	}
	if len(stored.Lines) != 2 || stored.Lines[0]["note"] != "m02 one" || stored.Lines[1]["note"] != "m02 two" || stored.Lines[1]["qty"] != "6.000" {
		t.Errorf("created draft lines = %v, want the Save's two lines", stored.Lines)
	}
	if d := e.fx.Load(t, docscreentest.Hooked, newID); d.CreatedBy != e.restricted.ID {
		t.Errorf("draft created by %d, want the restricted user %d", d.CreatedBy, e.restricted.ID)
	}
	if entries := e.fx.Audit(t, docscreentest.Hooked, newID); len(entries) != 1 {
		t.Errorf("audit entries for the new draft = %d, want one (a single create)", len(entries))
	}
}

// HOOK-005: Save.Errors is a 422 with nothing written and no Engine.Do; the
// Body redraws with Posted true, the typed value and the errors in StepState.
// A 422 from Engine.Do redraws the same way, and so does /new, which creates
// nothing.
func TestModule_SaveErrors(t *testing.T) {
	e, spy, id := m02Env(t)
	log := &m02Log{}
	const msg = "M02 reference can't be bad."
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		v := c.Req.PostFormValue("m02_ref")
		if v == "bad" {
			return docscreen.Save{Values: map[string]string{"ref": "IGNORED"}, Errors: []doctype.FieldError{{Field: "ref", Message: msg}}}, nil
		}
		return docscreen.Save{Values: map[string]string{"ref": v, "supplier_id": itoa(e.fx.M.Party["SHREE"])}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{
		"details": {Body: log.m02Body("details"), Save: save},
		"terms":   {Body: log.m02Body("terms"), Save: save},
	}})
	base := m02URL(id)

	wantRedraw := func(t *testing.T, page *goquery.Document, action, typed string, errText string) {
		t.Helper()
		body := m02BodyOf(t, stepForm(page, action), "the redrawn step form")
		if got := m02Attr(body, "data-posted"); got != "true" {
			t.Errorf("redrawn Body Posted = %s, want true", got)
		}
		if got := m02Attr(body.Find("#m02-ref"), "value"); got != typed {
			t.Errorf("redrawn Body input = %q, want the typed %q", got, typed)
		}
		if errText != "" {
			if got := norm(body.Find("#m02-errors").Text()); !strings.Contains(got, errText) {
				t.Errorf("Body's StepState.Errors = %q, want %q", got, errText)
			}
			if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, errText) {
				t.Errorf("error summary = %q, want %q", got, errText)
			}
		} else if page.Find(".error-summary").Length() == 0 {
			t.Errorf("no error summary on the 422 redraw")
		}
	}

	t.Run("Save.Errors", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		spy.take()
		for _, hx := range []bool{false, true} {
			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), "bad"), hdr...)
			wantRedraw(t, m02Page(t, rec, http.StatusUnprocessableEntity, "POST terms with Save.Errors"), base+"/edit/terms", "bad", msg)
		}
		if calls := spy.take(); len(calls) != 0 {
			t.Errorf("Engine.Do calls = %+v, want none for Save.Errors", calls)
		}
		m02WantUnchanged(t, e, id, before, audits, "a Save with Errors")
	})
	t.Run("engine 422", func(t *testing.T) {
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		const long = "THIRTEEN-CHAR" // ref has MaxLen 12
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), long))
		wantRedraw(t, m02Page(t, rec, http.StatusUnprocessableEntity, "POST terms with a ref too long"), base+"/edit/terms", long, "")
		m02WantUnchanged(t, e, id, before, audits, "the refused save")
	})
	t.Run("new", func(t *testing.T) {
		count := m02Count(t, e)
		log.mu.Lock()
		log.bodies = nil
		log.mu.Unlock()
		rec := e.post(t, m02NewURL, url.Values{"m02_ref": {"bad"}})
		wantRedraw(t, m02Page(t, rec, http.StatusUnprocessableEntity, "POST /new with Save.Errors"), m02NewURL, "bad", msg)
		if got := m02Count(t, e); got != count {
			t.Errorf("Hooked documents = %d, want %d: nothing created", got, count)
		}
		for _, b := range log.bodyCalls() {
			if b.View {
				t.Errorf("/new redraw Body call %+v, want View nil", b)
			}
		}
	})
}

// HOOK-006: a custom step opened at one version and saved elsewhere since is
// a 409 when posted with the old version: nothing is written, the conflict
// banner shows, and the Body redraws with Posted true and the typed value.
func TestModule_Conflict(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m02Log{}
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref")}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	base := m02URL(id)
	opened := pageVersion(t, e.get(t, base+"/edit/terms"))

	stored := m02Rec(t, e, id)
	if _, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSave, doc.ActionInput{
		Version: stored.Version, Values: map[string]string{"remarks": "changed elsewhere"}, Lines: stored.Lines,
	}); err != nil {
		t.Fatalf("saving the draft elsewhere: %v", err)
	}
	for _, hx := range []bool{false, true} {
		var hdr []string
		if hx {
			hdr = []string{"HX-Request", "true"}
		}
		what := "stale terms POST (htmx " + strconv.FormatBool(hx) + ")"
		before := m02Rec(t, e, id)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(opened), m02Typed), hdr...)
		page := m02Page(t, rec, http.StatusConflict, what)
		body := m02BodyOf(t, stepForm(page, base+"/edit/terms"), what)
		if got := m02Attr(body, "data-posted"); got != "true" {
			t.Errorf("%s: Body Posted = %s, want true", what, got)
		}
		if got := m02Attr(body.Find("#m02-ref"), "value"); got != m02Typed {
			t.Errorf("%s: Body input = %q, want the typed %s kept", what, got, m02Typed)
		}
		if page.Find("#wizard .banner--warning").Length() == 0 {
			t.Errorf("%s: no conflict banner", what)
		}
		m02WantUnchanged(t, e, id, before, audits, what)
		if got := before.Values; got["remarks"] != "changed elsewhere" || got["ref"] != "HK-REF" {
			t.Errorf("%s: stored remarks %q ref %q, want the other save's", what, got["remarks"], got["ref"])
		}
	}
}
