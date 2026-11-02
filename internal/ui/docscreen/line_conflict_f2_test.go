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
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const f2Keep = "Your entries stay on this page until you reload, so you can copy them."

// f2Conflict fails unless page's only banner is the warning ConflictBanner
// with DOC's stale text, a "Reload {label}" button to href, and the one
// autofocus (tabindex -1) around it.
func f2Conflict(t *testing.T, what string, page *goquery.Document, label, href string) {
	t.Helper()
	got := strings.Join(bannerTexts(page.Selection, "warning"), " ")
	if !strings.Contains(got, "Someone else changed "+label+" after you opened it.") || !strings.Contains(got, f2Keep) {
		t.Errorf("%s: warning banners = %q, want DOC's stale text and the keep-your-entries line", what, got)
	}
	if d := bannerTexts(page.Selection, "danger"); len(d) != 0 {
		t.Errorf("%s: danger banners = %q, want none", what, d)
	}
	reload := buttonsNamed(page.Find(".banner--warning"), "Reload "+label)
	if h := reload.AttrOr("href", ""); reload.Length() != 1 || h != href {
		t.Errorf("%s: Reload %s = %d buttons, href %q, want one to %s", what, label, reload.Length(), h, href)
	}
	f2Focus(t, what, page, ".banner--warning")
}

// f2Focus fails unless the page's one autofocus wraps the banner selected
// by sel and can take focus.
func f2Focus(t *testing.T, what string, page *goquery.Document, sel string) {
	t.Helper()
	f := page.Find("[autofocus]")
	if f.Length() != 1 || f.Find(sel).Length() != 1 || f.AttrOr("tabindex", "") != "-1" {
		html, _ := goquery.OuterHtml(f)
		t.Errorf("%s: %d autofocus (%.200s), want one, tabindex -1, around %s", what, f.Length(), html, sel)
	}
}

// F2 (CD-362, CD-379): a stale line page, new or existing, generic or with
// the module's Line, is DOC's 409 with the ConflictBanner; Reload goes to
// that page, which loads, and the typed value stays.
func TestLinePage_ConflictBanner(t *testing.T) {
	modules := map[string]func(t *testing.T) (*hookedEnv, int64){
		"generic": func(t *testing.T) (*hookedEnv, int64) { return m01bHooked(t, docscreen.Module{}) },
		"module Line": func(t *testing.T) (*hookedEnv, int64) {
			e, id, _ := m03Env(t, nil)
			return e, id
		},
	}
	for name, env := range modules {
		t.Run(name, func(t *testing.T) {
			e, id := env(t)
			base := m01bURL(id)
			label := "Draft " + itoa(id)
			before := m01bRecord(t, e, id)
			stale := strconv.Itoa(before.Version + 5)
			for _, path := range []string{"/lines/new", "/lines/1", "/lines/2"} {
				form := e.m01bLine1(stale)
				form.Set("qty", "9")
				rec := e.post(t, base+path, form)
				m03Status(t, rec.Code, http.StatusConflict, "stale POST "+path, rec.Body.String())
				page := uitest.Doc(t, rec)
				f2Conflict(t, path, page, label, base+path)
				if got := page.Find(`[name="qty"]`).AttrOr("value", ""); got != "9" {
					t.Errorf("%s: qty = %q, want the typed 9 kept", path, got)
				}
				m01bOK(t, e.get(t, base+path), "GET the Reload target "+path)
			}
			m03Unchanged(t, e, id, before, "stale line saves")
		})
	}
}

// A stale remove page's Reload is that remove page.
func TestLinePage_RemoveConflictBanner(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	base := m01bURL(id)
	stale := strconv.Itoa(m01bRecord(t, e, id).Version + 5)
	rec := e.post(t, base+"/lines/1/remove", url.Values{"version": {stale}})
	m03Status(t, rec.Code, http.StatusConflict, "stale POST remove", rec.Body.String())
	f2Conflict(t, "remove", uitest.Doc(t, rec), "Draft "+itoa(id), base+"/lines/1/remove")
}

// A line removed since a stale page was read has no page to reload: the 409
// keeps the typed values on the form, and Reload goes to the items step.
func TestLinePage_ConflictLineGone(t *testing.T) {
	t.Run("generic", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{})
		f2Gone(t, e, id)
	})
	t.Run("module Line", func(t *testing.T) {
		e, id, h := m03Env(t, nil)
		f2Gone(t, e, id)
		calls := h.taken()
		if n := len(calls); n == 0 || calls[n-1].Hook != "body" || calls[n-1].No != 0 || !calls[n-1].State.Posted {
			t.Errorf("hook calls = %+v, want Line.Body redrawn last as no line, with Posted", calls)
		}
	})
}

// f2Gone posts stale to line 3 of Hooked id, which has two lines.
func f2Gone(t *testing.T, e *hookedEnv, id int64) {
	t.Helper()
	base := m01bURL(id)
	before := m01bRecord(t, e, id)
	form := e.m01bLine1(strconv.Itoa(before.Version + 5))
	form.Set("qty", "9")
	rec := e.post(t, base+"/lines/3", form)
	m03Status(t, rec.Code, http.StatusConflict, "stale POST /lines/3", rec.Body.String())
	page := uitest.Doc(t, rec)
	f2Conflict(t, "gone", page, "Draft "+itoa(id), base+"/edit/items")
	if got := page.Find(`[name="qty"]`).AttrOr("value", ""); got != "9" {
		t.Errorf("gone: qty = %q, want the typed 9 kept", got)
	}
	m03Unchanged(t, e, id, before, "a stale save of a removed line")
}

// A missing line is a 404 unless the post is stale: at the current version,
// from the grid, or with a {no} that was never a line. Past draft it goes to
// the document page, as any other refusal there does.
func TestLinePage_MissingLine(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	base := m01bURL(id)
	rec := m01bRecord(t, e, id)
	current, stale := e.m01bLine1(strconv.Itoa(rec.Version)), e.m01bLine1(strconv.Itoa(rec.Version+5))
	if r := e.post(t, base+"/lines/3", current); r.Code != http.StatusNotFound {
		t.Errorf("POST /lines/3 at the current version = %d, want 404; body %.300s", r.Code, r.Body)
	}
	for _, no := range []string{"0", "x"} {
		if r := e.post(t, base+"/lines/"+no, stale); r.Code != http.StatusNotFound {
			t.Errorf("stale POST /lines/%s = %d, want 404; body %.300s", no, r.Code, r.Body)
		}
	}
	if r := e.post(t, base+"/lines/3", stale, "HX-Request", "true"); r.Code != http.StatusNotFound {
		t.Errorf("stale htmx POST /lines/3 = %d, want 404; body %.300s", r.Code, r.Body)
	}
	m01bSubmit(t, e, id)
	r := e.post(t, base+"/lines/3", stale)
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != base {
		t.Errorf("stale POST /lines/3 past draft = %d to %q, want 303 to the document", r.Code, r.Header().Get("Location"))
	}
}

// Only a 409 is the ConflictBanner: another refusal with no field stays
// danger, with no Reload, on the line and remove pages, and takes focus.
func TestLinePage_RefusalStaysDanger(t *testing.T) {
	const msg = "Something else went wrong."
	for _, path := range []string{"/lines/1", "/lines/new", "/lines/1/remove"} {
		e := newLinesEnv(t, manyLines(2))
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusUnprocessableEntity, msg: msg})
		rec := e.post(t, doc42+path, url.Values{"version": {"5"}, "qty": {"3"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST %s = %d, want 422; body %.300s", path, rec.Code, rec.Body)
		}
		page := uitest.Doc(t, rec)
		if got := bannerTexts(page.Selection, "danger"); len(got) != 1 || got[0] != msg {
			t.Errorf("%s: danger banners = %q, want %q", path, got, msg)
		}
		if got := bannerTexts(page.Selection, "warning"); len(got) != 0 {
			t.Errorf("%s: warning banners = %q, want none", path, got)
		}
		if strings.Contains(norm(page.Find("main").Text()), "Reload") {
			t.Errorf("%s: a non-409 refusal offers Reload: %q", path, norm(page.Find("main").Text()))
		}
		f2Focus(t, path, page, ".banner--danger")
	}
}

const f2Note = "Fields marked * are required."

// f2HasNote reports whether page's line form starts with the required note.
func f2HasNote(t *testing.T, page *goquery.Document) bool {
	t.Helper()
	form := page.Find("main form")
	if form.Length() != 1 {
		t.Fatalf("%d forms in main, want the line form", form.Length())
	}
	return form.Find("p.field__hint").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Text()) == f2Note
	}).Length() == 1
}

// F2: the line form starts with the required note when a line field it
// shows is required, on the page, its redraws and with the module's Line.
func TestLinePage_RequiredNote(t *testing.T) {
	t.Run("generic", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{})
		base := m01bURL(id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			if !f2HasNote(t, m01bOK(t, e.get(t, base+path), "GET "+path)) {
				t.Errorf("GET %s: no required note", path)
			}
			form := e.m01bLine1(m01bVersion(t, e, id))
			form.Set("qty", "abc")
			rec := e.post(t, base+path, form)
			m03Status(t, rec.Code, http.StatusUnprocessableEntity, "POST "+path, rec.Body.String())
			if !f2HasNote(t, uitest.Doc(t, rec)) {
				t.Errorf("POST %s 422: no required note", path)
			}
		}
	})
	t.Run("module Line", func(t *testing.T) {
		e, id, _ := m03Env(t, nil)
		if !f2HasNote(t, m01bOK(t, e.get(t, m01bURL(id)+"/lines/1"), "GET line 1")) {
			t.Error("no required note with the module's Line")
		}
	})
	// item_id is the one required line field; supplier_id, required on the
	// header, doesn't count.
	t.Run("no required line field", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("item_id")})
		if f2HasNote(t, m01bOK(t, e.get(t, m01bURL(id)+"/lines/1"), "GET line 1")) {
			t.Error("a required note with no required field shown")
		}
	})
	t.Run("remove page", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{})
		if f2HasNote(t, m01bOK(t, e.get(t, m01bURL(id)+"/lines/1/remove"), "GET remove")) {
			t.Error("a required note on the remove question")
		}
	})
}
