package docscreen_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	sitemaster "github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-2: refusals and errors from the hooks, Steps keys (CD-256 (8)), Body
// steps are never empty, StepLabels and navigation from a custom step.

// m02BodyErr is the error m02Fail's Body returns.
const m02BodyErr = errFor("m02 body failed")

// m02Fail is a Body that fails when fail says so and otherwise renders
// #m02-body.
func m02Fail(fail func(docscreen.Ctx, docscreen.StepState) bool) func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) {
	return func(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
		if fail(c, s) {
			return nil, m02BodyErr
		}
		return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, `<div id="m02-body"><input name="m02_ref" value=""/></div>`)
			return err
		}), nil
	}
}

func m02Always(docscreen.Ctx, docscreen.StepState) bool { return true }

func m02WhenPosted(_ docscreen.Ctx, s docscreen.StepState) bool { return s.Posted }

// m02SaveWithErrors is a Save that always returns a parse error.
func m02SaveWithErrors(docscreen.Ctx) (docscreen.Save, error) {
	return docscreen.Save{Errors: []doctype.FieldError{{Field: "ref", Message: "M02 reference is not right."}}}, nil
}

// m02Want500 fails unless rec is a plain 500 that doesn't show err's text.
func m02Want500(t *testing.T, code int, body, what string) {
	t.Helper()
	if code != http.StatusInternalServerError {
		t.Errorf("%s = %d, want a plain 500; body %.300s", what, code, body)
		return
	}
	if strings.Contains(body, string(m02BodyErr)) || strings.Contains(body, "m02 save broke") {
		t.Errorf("%s: the 500 shows the hook's own error text", what)
	}
}

// HOOK-007: an error from Save goes through Classify: AUTH's 403 from
// CheckLinkTx, inside the hook's own db.Read on a record the user can't read,
// shows AUTH's exact refusal (CD-240); a plain error is a 500. Nothing is
// written either way. A Body's error is a 500 wherever a step renders.
func TestModule_SaveError(t *testing.T) {
	e, _, _ := m02Env(t)
	own, err := e.draft(t, e.restricted, "TECH-SITE")
	if err != nil {
		t.Fatalf("restricted user's draft: %v", err)
	}
	metro := e.site["METRO-SITE"]
	checkLink := func(ctx context.Context, p auth.Principal) error {
		return e.fx.DB.Read(ctx, func(tx *db.Tx) error {
			return e.d.Auth.CheckLinkTx(ctx, tx, p, sitemaster.DocType, metro)
		})
	}
	// AUTH's own refusal, read from AUTH: the setup decides the outcome.
	refusal := checkLink(t.Context(), e.restricted)
	var le *auth.LinkError
	if !errors.As(refusal, &le) {
		t.Fatalf("CheckLinkTx on METRO-SITE as the restricted user = %v (%T), want AUTH's *auth.LinkError", refusal, refusal)
	}
	want := refusal.Error()

	var plain bool
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		if plain {
			return docscreen.Save{}, errFor("m02 save broke")
		}
		if err := checkLink(c.Req.Context(), c.Principal); err != nil {
			return docscreen.Save{}, err
		}
		return docscreen.Save{Values: map[string]string{"ref": "NEVER"}}, nil
	}
	log := &m02Log{}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	e.who = e.restricted
	base := m02URL(own.ID)

	t.Run("403", func(t *testing.T) {
		before := m02Rec(t, e, own.ID)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, own.ID))
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), m02Typed))
		if rec.Code != http.StatusForbidden {
			t.Fatalf("terms save refused by CheckLinkTx = %d, want 403; body %.300s", rec.Code, rec.Body)
		}
		if got := pageText(t, rec); !strings.Contains(got, want) {
			t.Errorf("403 page = %q, want AUTH's refusal %q", got, want)
		}
		m02WantUnchanged(t, e, own.ID, before, audits, "the refused save")
	})
	t.Run("plain error", func(t *testing.T) {
		plain = true
		before := m02Rec(t, e, own.ID)
		audits := len(e.fx.Audit(t, docscreentest.Hooked, own.ID))
		rec := e.post(t, base+"/edit/terms", m02Typing(strconv.Itoa(before.Version), m02Typed))
		m02Want500(t, rec.Code, rec.Body.String(), "terms save whose Save returns a plain error")
		m02WantUnchanged(t, e, own.ID, before, audits, "the failed save")
	})
	t.Run("Body errors at every stepViewOf call site", m02StepViewErrors)
}

// m02StepViewErrors is HOOK-007 at every call site of stepViewOf in form.go:
// a Body that returns an error is a plain 500 on newGet, newPost's redraw,
// stepGet (through arrival), stepPost's redraw, advance's htmx answer and
// failingStep; on checkPage the check step has no Body, so a Steps key
// "check" fails it.
func m02StepViewErrors(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	m := e.fx.M
	supplier := itoa(m.Party["SHREE"])

	t.Run("newGet", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: m02Fail(m02Always)}}})
		rec := e.get(t, m02NewURL)
		m02Want500(t, rec.Code, rec.Body.String(), "GET /new")
	})
	t.Run("newPost redraw", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: m02Fail(m02WhenPosted), Save: m02SaveWithErrors}}})
		m02Page(t, e.get(t, m02NewURL), http.StatusOK, "GET /new (the Body fails only on a redraw)")
		count := m02Count(t, e)
		rec := e.post(t, m02NewURL, url.Values{"m02_ref": {"x"}})
		m02Want500(t, rec.Code, rec.Body.String(), "POST /new redraw")
		if got := m02Count(t, e); got != count {
			t.Errorf("Hooked documents = %d, want %d", got, count)
		}
	})
	t.Run("stepGet", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: m02Fail(m02Always)}}})
		for _, hx := range []bool{false, true} {
			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.get(t, base+"/edit/terms", hdr...)
			m02Want500(t, rec.Code, rec.Body.String(), "GET terms (htmx "+strconv.FormatBool(hx)+")")
		}
	})
	t.Run("stepPost redraw", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: m02Fail(m02WhenPosted), Save: m02SaveWithErrors}}})
		m02Page(t, e.get(t, base+"/edit/terms"), http.StatusOK, "GET terms (the Body fails only on a redraw)")
		rec := e.post(t, base+"/edit/terms", m02Typing(m02Version(t, e, id), "x"))
		m02Want500(t, rec.Code, rec.Body.String(), "POST terms redraw")
	})
	t.Run("advance", func(t *testing.T) {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: m02Fail(m02Always)}}})
		form := url.Values{"version": {m02Version(t, e, id)}, "supplier_id": {supplier}, "site_id": {itoa(e.site["METRO-SITE"])}}
		rec := e.post(t, base+"/edit/details", form, "HX-Request", "true")
		m02Want500(t, rec.Code, rec.Body.String(), "htmx details save advancing to terms")
	})
	t.Run("failingStep", func(t *testing.T) {
		res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{"site_id": itoa(e.site["METRO-SITE"]), "ref": "HK-REF"},
			Lines:  []map[string]string{{"project_id": itoa(m.Project["PRJ-METRO"]), "item_id": itoa(m.Item["CEMENT-OPC"]), "qty": "10.000"}},
		})
		if err != nil {
			t.Fatalf("saving a draft with no supplier: %v", err)
		}
		m02Mount(e, docscreen.Module{})
		version := m02Version(t, e, res.ID)
		if rec := e.post(t, m02URL(res.ID)+"/submit", url.Values{"version": {version}}, "HX-Request", "true"); rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("submit with no supplier and no module = %d, want 422 on the failing step: the setup decides the outcome", rec.Code)
		}
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"details": {Body: m02Fail(m02Always)}}})
		rec := e.post(t, m02URL(res.ID)+"/submit", url.Values{"version": {version}}, "HX-Request", "true")
		m02Want500(t, rec.Code, rec.Body.String(), "submit refused, rendering the failing details step")
	})
	t.Run("checkPage", func(t *testing.T) {
		amend := func(m docscreen.Module) (int, string) {
			t.Helper()
			res, err := e.draft(t, e.open, "METRO-SITE")
			if err != nil {
				t.Fatalf("draft: %v", err)
			}
			sub, err := e.engine.Do(t.Context(), e.fx.Admin, docscreentest.Hooked, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
			if err != nil {
				t.Fatalf("submit: %v", err)
			}
			if _, err := e.engine.Do(t.Context(), e.fx.Admin, docscreentest.Hooked, res.ID, doc.ActionCancel, doc.ActionInput{Version: sub.Version, Reason: "m02 cancel"}); err != nil {
				t.Fatalf("cancel: %v", err)
			}
			m02Mount(e, m)
			who := e.who
			e.who = e.fx.Admin
			defer func() { e.who = who }()
			rec := e.post(t, m02URL(res.ID)+"/amend", url.Values{"version": {m02Version(t, e, res.ID)}}, "HX-Request", "true")
			return rec.Code, rec.Body.String()
		}
		if code, body := amend(docscreen.Module{}); code != http.StatusOK {
			t.Fatalf("htmx amend with no module = %d, want 200 with the check page: the setup decides the outcome; body %.300s", code, body)
		}
		code, body := amend(docscreen.Module{Steps: map[string]docscreen.Step{"check": {Body: m02Fail(func(docscreen.Ctx, docscreen.StepState) bool { return false })}}})
		m02Want500(t, code, body, "htmx amend leading to the check page with a Steps key check")
	})
}

// CD-256 (8): Steps keys must be FormSteps slugs other than check; any other
// key makes every wizard render a plain 500, whichever step is shown.
func TestModule_StepKeys(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	ok := m02Fail(func(docscreen.Ctx, docscreen.StepState) bool { return false })
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: ok}}})
	for _, path := range []string{base + "/edit/details", base + "/edit/terms", base + "/edit/check", m02NewURL} {
		m02Page(t, e.get(t, path), http.StatusOK, "GET "+path+" with a valid Steps key")
	}
	for _, key := range []string{"bogus", "notes", "check"} {
		m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{key: {Body: ok}, "terms": {Body: ok}}})
		for _, path := range []string{base + "/edit/details", base + "/edit/terms", base + "/edit/items", base + "/edit/check", m02NewURL} {
			rec := e.get(t, path)
			m02Want500(t, rec.Code, rec.Body.String(), "GET "+path+" with Steps key "+key)
		}
	}
}

// m02Notes is a test DocType Hooked can't stand in for: lines and no header
// field, so FormSteps adds the empty "notes" step (Notes) after
// items, which shows Nothing needed here.
const m02Notes = "docscreentest.M02Notes"

var m02NotesOnce sync.Once

func m02RegisterNotes() {
	m02NotesOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m02Notes, Label: "Notes Order", PluralLabel: "Notes Orders", Table: "m02_notes", LinesTable: "m02_notes_line",
			Submittable: true, Hooks: scopeHooks{},
			Fields: []doctype.Field{
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, OnLine: true},
			},
		})
	})
}

// A step with a Body is never Nothing needed here, though its generic form
// would be (hooks spec §4.1).
func TestModule_BodyNeverEmpty(t *testing.T) {
	m02RegisterNotes()
	e, _, _ := m02Env(t)
	e.fx.Exec(t, `CREATE TABLE m02_notes (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL)`)
	e.fx.Exec(t, `CREATE TABLE m02_notes_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL, item_id INTEGER)`)
	e.fx.Naming.Prefix[m02Notes] = "MN"
	ctx := t.Context()
	a, isReal := e.d.Auth.(*auth.Auth)
	if !isReal {
		t.Fatalf("Deps.Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	all := map[auth.Right]auth.Scope{}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "delete"} {
		all[r] = auth.ScopeAll
	}
	if err := a.SetRights(ctx, e.fx.Admin, "Hooker", m02Notes, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	p, _, err := a.LoadPrincipal(ctx, e.open.ID)
	if err != nil {
		t.Fatalf("LoadPrincipal: %v", err)
	}
	e.open, e.who = p, p
	res, err := e.engine.Do(ctx, p, m02Notes, 0, doc.ActionSave, doc.ActionInput{Lines: []map[string]string{{"item_id": itoa(e.fx.M.Item["CEMENT-OPC"])}}})
	if err != nil {
		t.Fatalf("saving a notes draft: %v", err)
	}
	notes := "/doc/" + m02Notes + "/" + itoa(res.ID) + "/edit/notes"
	text := func(what string) (string, *goquery.Document) {
		t.Helper()
		page := m02Page(t, e.get(t, notes), http.StatusOK, what)
		return norm(page.Find("#wizard").Text()), page
	}
	if got, _ := text("GET notes with no module"); !strings.Contains(got, "Nothing needed here") {
		t.Fatalf("notes step with no module = %q, want Nothing needed here: the setup decides the outcome", got)
	}
	log := &m02Log{}
	e.d.Modules = map[string]docscreen.Module{m02Notes: {Steps: map[string]docscreen.Step{"notes": {Body: log.m02Body("notes")}}}}
	e.remount()
	got, page := text("GET notes with a Body")
	if strings.Contains(got, "Nothing needed here") {
		t.Errorf("notes step with a Body = %q, want no Nothing needed here", got)
	}
	m02BodyOf(t, stepForm(page, "/doc/"+m02Notes+"/"+itoa(res.ID)+"/edit/notes"), "the notes step form")
}

// m02StepperLabels are the Stepper's step labels, without the hidden
// "Completed:" prefix, and the hrefs of its links.
func m02StepperLabels(page *goquery.Document) (labels, hrefs []string) {
	page.Find(".stepper__list li").Each(func(_ int, li *goquery.Selection) {
		c := li.Clone()
		c.Find(".visually-hidden").Remove()
		labels = append(labels, norm(c.Text()))
		if h, ok := li.Find("a").Attr("href"); ok {
			hrefs = append(hrefs, h)
		}
	})
	return labels, hrefs
}

// StepLabels changes only the label shown, in the Stepper and the heading, on
// a draft and on /new; slugs, URLs and order stay FormSteps'.
func TestModule_StepLabels(t *testing.T) {
	e, _, id := m02Env(t)
	m02Mount(e, docscreen.Module{StepLabels: map[string]string{"details": "Choose supplier", "terms": "Delivery terms"}})
	base := m02URL(id)
	wantLabels := []string{"Choose supplier", "Delivery terms", "Items", "Check"}

	page := m02Page(t, e.get(t, base+"/edit/details"), http.StatusOK, "GET details")
	labels, hrefs := m02StepperLabels(page)
	if !slices.Equal(labels, wantLabels) {
		t.Errorf("stepper labels = %q, want %q", labels, wantLabels)
	}
	if want := []string{base + "/edit/terms", base + "/edit/items", base + "/edit/check"}; !slices.Equal(hrefs, want) {
		t.Errorf("stepper links = %q, want %q (slugs unchanged)", hrefs, want)
	}
	if got := norm(page.Find("h2#step-title").Text()); got != "Choose supplier" {
		t.Errorf("details heading = %q, want Choose supplier", got)
	}

	page = m02Page(t, e.get(t, base+"/edit/terms"), http.StatusOK, "GET terms")
	if got := norm(page.Find("h2#step-title").Text()); got != "Delivery terms" {
		t.Errorf("terms heading = %q, want Delivery terms", got)
	}
	if got := stepCount(page.Selection); got != "Step 2 of 4" {
		t.Errorf("terms stepper = %q, want Step 2 of 4", got)
	}
	if labels, _ := m02StepperLabels(page); !slices.Equal(labels, wantLabels) {
		t.Errorf("terms stepper labels = %q, want %q", labels, wantLabels)
	}
	if m02Named(page.Selection, "ref") != 1 {
		t.Errorf("terms step lost its generic ref input: StepLabels changes only labels")
	}

	page = m02Page(t, e.get(t, m02NewURL), http.StatusOK, "GET /new")
	if got := norm(page.Find("h2#step-title").Text()); got != "Choose supplier" {
		t.Errorf("/new heading = %q, want Choose supplier", got)
	}
	if labels, _ := m02StepperLabels(page); !slices.Equal(labels, wantLabels) {
		t.Errorf("/new stepper labels = %q, want %q", labels, wantLabels)
	}

	form := url.Values{"version": {m02Version(t, e, id)}, "ref": {"LABELLED"}, "remarks": {"r"}}
	if got := location(t, e.post(t, base+"/edit/terms", form)); got != base+"/edit/items" {
		t.Errorf("terms save went to %q, want %s/edit/items", got, base)
	}
	if got := m02Rec(t, e, id).Values["ref"]; got != "LABELLED" {
		t.Errorf("stored ref = %q, want LABELLED", got)
	}
}

// HOOK-014: from a custom step (Body and Save), return=check goes to the
// check page, Save and finish later to the document page (303 and htmx's
// HX-Redirect), Back to the step before and a plain save to the next step
// (htmx: its frame and HX-Push-Url); each saves the Save's values.
func TestModule_Navigation(t *testing.T) {
	e, _, id := m02Env(t)
	log := &m02Log{}
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		return docscreen.Save{Values: map[string]string{"ref": c.Req.PostFormValue("m02_ref")}}, nil
	}
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"terms": {Body: log.m02Body("terms"), Save: save}}})
	base := m02URL(id)
	cases := []struct {
		name, ref string
		set       url.Values
		hx        bool
		want      string // Location, HX-Redirect or HX-Push-Url
	}{
		{name: "return=check", ref: "NAV-CHECK", set: url.Values{"return": {"check"}}, want: base + "/edit/check"},
		{name: "finish later", ref: "NAV-FINISH", set: url.Values{"nav": {"finish"}}, want: base},
		{name: "finish later htmx", ref: "NAV-FIN-HX", set: url.Values{"nav": {"finish"}}, hx: true, want: base},
		{name: "back", ref: "NAV-BACK", set: url.Values{"nav": {"back"}}, want: base + "/edit/details"},
		{name: "next", ref: "NAV-NEXT", want: base + "/edit/items"},
		{name: "next htmx", ref: "NAV-NEXT-HX", hx: true, want: base + "/edit/items"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			form := m02Typing(m02Version(t, e, id), tc.ref)
			for k, v := range tc.set {
				form[k] = v
			}
			var hdr []string
			if tc.hx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := e.post(t, base+"/edit/terms", form, hdr...)
			switch {
			case !tc.hx:
				if got := location(t, rec); got != tc.want {
					t.Errorf("went to %q, want %q", got, tc.want)
				}
			case tc.set.Get("nav") == "finish":
				if rec.Code != http.StatusOK || pathOf(t, rec.Header().Get("HX-Redirect")) != tc.want {
					t.Errorf("htmx finish = %d, HX-Redirect %q; want 200 and %q", rec.Code, rec.Header().Get("HX-Redirect"), tc.want)
				}
			default:
				if rec.Code != http.StatusOK || pathOf(t, rec.Header().Get("HX-Push-Url")) != tc.want {
					t.Errorf("htmx next = %d, HX-Push-Url %q; want 200 and %q", rec.Code, rec.Header().Get("HX-Push-Url"), tc.want)
				}
				if got := norm(uitest.Doc(t, rec).Find("h2#step-title").Text()); got != "Items" {
					t.Errorf("htmx next frame heading = %q, want Items", got)
				}
			}
			if got := m02Rec(t, e, id).Values["ref"]; got != tc.ref {
				t.Errorf("stored ref = %q, want the Save's %q", got, tc.ref)
			}
		})
	}
}
