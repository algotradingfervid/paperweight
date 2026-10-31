package docscreen_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// CD-256 (8) on a POST: a bad Steps key is a plain 500 before any Save or
// write, on a draft's step and on /new, even where the answer is a redirect
// that renders no step.
func TestModule_StepKeysOnPost(t *testing.T) {
	e, _, id := m02Env(t)
	saved := false
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		saved = true
		return docscreen.Save{Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref")}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"bogus": {}, "terms": {Save: save}, "details": {Save: save}}})

	before := m02Rec(t, e, id)
	audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
	rec := e.post(t, m02URL(id)+"/edit/terms", m02Typing(strconv.Itoa(before.Version), m02Typed))
	m02Want500(t, rec.Code, rec.Body.String(), "POST terms with Steps key bogus")
	m02WantUnchanged(t, e, id, before, audits, "POST terms with Steps key bogus")

	count := m02Count(t, e)
	rec = e.post(t, m02NewURL, url.Values{"m02_ref": {m02Typed}})
	m02Want500(t, rec.Code, rec.Body.String(), "POST /new with Steps key bogus")
	if got := m02Count(t, e); got != count {
		t.Errorf("Hooked documents = %d, want %d: nothing created", got, count)
	}
	if saved {
		t.Errorf("Save ran with a bad Steps key")
	}
}

// HOOK-007 on /new: an error from the first step's Save goes through
// Classify, so a plain one is a 500 and no draft is created.
func TestModule_NewSaveError(t *testing.T) {
	e, _, _ := m02Env(t)
	save := func(docscreen.Ctx) (docscreen.Save, error) { return docscreen.Save{}, errFor("m02 save broke") }
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Save: save}}})
	count := m02Count(t, e)
	rec := e.post(t, m02NewURL, url.Values{"m02_ref": {m02Typed}})
	m02Want500(t, rec.Code, rec.Body.String(), "POST /new whose Save returns a plain error")
	if got := m02Count(t, e); got != count {
		t.Errorf("Hooked documents = %d, want %d: nothing created", got, count)
	}
}

// CD-94 with a custom Save: a page opened before another save removed line 2
// posts a Save naming line 2's id. That is the conflict (409, the Body redrawn
// Posted with the typed value), not an unknown id's 500.
func TestModule_StaleRemovedLine(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m02Log{}
	opened := m02Rec(t, e, id)
	removed := opened.Lines[1]["id"]
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref")}, Lines: []map[string]string{{"id": removed, "note": "stale"}}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	if _, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSave, doc.ActionInput{
		Version: opened.Version, Values: opened.Values, Lines: opened.Lines[:1],
	}); err != nil {
		t.Fatalf("removing line 2 elsewhere: %v", err)
	}
	before := m02Rec(t, e, id)
	audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
	rec := e.post(t, m02URL(id)+"/edit/terms", m02Typing(strconv.Itoa(opened.Version), m02Typed))
	page := m02Page(t, rec, http.StatusConflict, "stale terms POST naming a removed line")
	body := m02BodyOf(t, stepForm(page, m02URL(id)+"/edit/terms"), "the conflict redraw")
	if got := m02Attr(body, "data-posted"); got != "true" {
		t.Errorf("Body Posted = %s, want true", got)
	}
	if got := m02Attr(body.Find("#m02-ref"), "value"); got != m02Typed {
		t.Errorf("Body input = %q, want the typed %s", got, m02Typed)
	}
	m02WantUnchanged(t, e, id, before, audits, "the stale save")
}

// CD-256 (4) on /new's redraw: a 422 from the first step's Save still gives
// the Body StepState.Defaults with the single allowed site.
func TestModule_NewRedrawDefaults(t *testing.T) {
	e, _, _ := m02Env(t)
	tech := itoa(e.site["TECH-SITE"])
	log := &m02Log{}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: log.m02Body("details"), Save: m02SaveWithErrors}}})
	e.who = e.restricted
	page := m02Page(t, e.post(t, m02NewURL, url.Values{"m02_ref": {"x"}}), http.StatusUnprocessableEntity, "POST /new with Save.Errors")
	body := m02BodyOf(t, stepForm(page, m02NewURL), "the /new redraw")
	if got := m02Attr(body, "data-posted"); got != "true" {
		t.Errorf("Body Posted = %s, want true", got)
	}
	if got := m02Attr(body, "data-site"); got != tech {
		t.Errorf("/new redraw Defaults[site_id] = %q, want %s", got, tech)
	}
}

// Hooks spec §4.1: a Body replaces the step's inputs only; the step's
// ReadOnly fields still show as text beside it.
func TestModule_BodyKeepsFixed(t *testing.T) {
	e, id := m01bROEnv(t)
	m01bMount(e, m01bRO, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: m02Fail(func(docscreen.Ctx, docscreen.StepState) bool { return false })}}})
	base := "/doc/" + m01bRO + "/" + itoa(id) + "/edit/details"
	page := m01bOK(t, e.get(t, base), "GET details")
	form := stepForm(page, base)
	m02BodyOf(t, form, "details step form")
	if got := norm(page.Find("#wizard").Text()); !strings.Contains(got, m01bTag) || !strings.Contains(got, "Batch tag") {
		t.Errorf("details step with a Body = %q, want the ReadOnly Batch tag %s shown", got, m01bTag)
	}
	if n := m02Named(page.Selection, "supplier_id"); n != 0 {
		t.Errorf("%d generic supplier_id inputs, want none", n)
	}
}

// CD-94: a 422 redraw made before the save (Save.Errors) carries the version
// the page posted, not the one stored since, so a re-post after another save
// is still a 409 and never overwrites it.
func TestModule_RedrawKeepsPostedVersion(t *testing.T) {
	e, _, id := m02Env(t)
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		v := c.Req.PostFormValue("m02_ref")
		if v == "bad" {
			return docscreen.Save{Errors: []doctype.FieldError{{Field: "ref", Message: "M02 reference can't be bad."}}}, nil
		}
		return docscreen.Save{Values: map[string]string{"ref": v}}, nil
	}
	log := &m02Log{}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	base := m02URL(id) + "/edit/terms"
	opened := m02Rec(t, e, id)
	if _, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSave, doc.ActionInput{
		Version: opened.Version, Values: map[string]string{"remarks": "changed elsewhere"}, Lines: opened.Lines,
	}); err != nil {
		t.Fatalf("saving the draft elsewhere: %v", err)
	}
	posted := strconv.Itoa(opened.Version)
	page := m02Page(t, e.post(t, base, m02Typing(posted, "bad")), http.StatusUnprocessableEntity, "POST terms with Save.Errors")
	got := m02Attr(stepForm(page, base).Find("#doc-version"), "value")
	if got != posted {
		t.Errorf("redrawn #doc-version = %q, want the posted %s", got, posted)
	}
	before := m02Rec(t, e, id)
	audits := len(e.fx.Audit(t, docscreentest.Hooked, id))
	if rec := e.post(t, base, m02Typing(got, m02Typed)); rec.Code != http.StatusConflict {
		t.Errorf("re-post with the redrawn version = %d, want 409", rec.Code)
	}
	m02WantUnchanged(t, e, id, before, audits, "the re-post")
}

// An Items step with a Body has no line editor, so a line error and a
// whole-lines error from its Save link to the step heading, and every summary
// link targets an element on the page.
func TestModule_ItemsBodyErrorTarget(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m02Log{}
	save := func(docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Errors: []doctype.FieldError{
			{Line: 1, Field: "qty", Message: "M02 line quantity is not right."},
			{Field: "lines", Message: "M02 lines are not right."},
		}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: log.m02Body("items"), Save: save}}})
	base := m02URL(id) + "/edit/items"
	page := m02Page(t, e.post(t, base, m02Typing(m02Version(t, e, id), "")), http.StatusUnprocessableEntity, "POST items with line errors")
	m02BodyOf(t, stepForm(page, base), "the items redraw")
	links := page.Find(".error-summary a")
	if links.Length() != 2 {
		t.Fatalf("%d summary links, want 2", links.Length())
	}
	links.Each(func(_ int, a *goquery.Selection) {
		href := m02Attr(a, "href")
		if href != "#step-title" {
			t.Errorf("summary link %q = %q, want #step-title", norm(a.Text()), href)
		}
		if page.Find(href).Length() != 1 {
			t.Errorf("summary link %q targets %q, which is not on the page", norm(a.Text()), href)
		}
	})
}
