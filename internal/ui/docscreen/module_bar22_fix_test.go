package docscreen_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m22WantNoSubmit fails unless form#line-form holds no submit at all and
// its default button (Enter in a field) is the bar's button[form=line-form],
// which the upload's AlsoDisable disables mid-upload (UX-E5). An Enter copy
// inside the form isn't disabled and would save a line without its receipt.
func m22WantNoSubmit(t *testing.T, page *goquery.Document, what string) {
	t.Helper()
	form := page.Find("form#line-form")
	if form.Length() != 1 {
		t.Fatalf("%s: found %d form#line-form, want 1", what, form.Length())
	}
	m22Submits(page).Each(func(_ int, s *goquery.Selection) {
		if s.Closest("form#line-form").Length() > 0 {
			t.Errorf("%s: form#line-form holds a submit (class %q, text %q), want none", what, s.AttrOr("class", ""), norm(s.Text()))
		}
	})
	def := m22Default(page, "line-form")
	bar := page.Find(".actionbar button[form=line-form]")
	if def.Length() == 0 || bar.Length() == 0 || def.Get(0) != bar.Get(0) {
		t.Errorf("%s: line-form's default button is %q (class %q), want the bar's button[form=line-form]", what, norm(def.Text()), def.AttrOr("class", ""))
	}
}

// HOOK-039 (CD-275 UX-E4, UX-E5): with Module.Line the line form holds no
// submit, so Enter presses the bar's Save line, on the GET of a new and an
// existing line, the 422 and 409 redraws, full page and htmx.
func TestModule_LineFormNoSubmit(t *testing.T) {
	t.Run("GET", func(t *testing.T) {
		e, id, _, _ := m20TopEnv(t)
		base := m01bURL(id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			for _, hx := range []bool{false, true} {
				what := "GET " + path + " htmx " + m22Bool(hx)
				m22WantNoSubmit(t, m01bOK(t, e.get(t, base+path, m20Hdr(hx)...), what), what)
			}
		}
	})

	t.Run("422 redraw", func(t *testing.T) {
		e, id, _, _ := m20TopEnv(t)
		base := m01bURL(id)
		version := m01bVersion(t, e, id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			for _, hx := range []bool{false, true} {
				what := "POST " + path + " qty abc, htmx " + m22Bool(hx)
				rec := e.post(t, base+path, url.Values{"version": {version}, "qty": {"abc"}}, m20Hdr(hx)...)
				m03Status(t, rec.Code, http.StatusUnprocessableEntity, what, rec.Body.String())
				m22WantNoSubmit(t, uitest.Doc(t, rec), what)
			}
		}
	})

	t.Run("409 redraw", func(t *testing.T) {
		e, id, _, _ := m20TopEnv(t)
		base := m01bURL(id)
		old := m01bVersion(t, e, id)
		location(t, e.post(t, base+"/lines/2", url.Values{"version": {old}, "qty": {"6"}}))
		for _, path := range []string{"/lines/new", "/lines/1"} {
			for _, hx := range []bool{false, true} {
				what := "POST " + path + " with a stale version, htmx " + m22Bool(hx)
				rec := e.post(t, base+path, url.Values{"version": {old}, "qty": {"9"}}, m20Hdr(hx)...)
				m03Status(t, rec.Code, http.StatusConflict, what, rec.Body.String())
				m22WantNoSubmit(t, uitest.Doc(t, rec), what)
			}
		}
	})
}
