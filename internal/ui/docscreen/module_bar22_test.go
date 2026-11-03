package docscreen_test

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-22 (CD-275 UX-E4, UX-E6; expense UX §5.5, §5.6, §6): with Module.Line
// the line page's Save line is the sticky ActionBar's primary, posting the
// line form through form="line-form"; CheckExtra.Totals follow the check
// page's grand total and its words. Both run on a fixture database, the real
// engine and the real AUTH service (CD-125), through the hooked harnesses.

const m22SaveLabel = "Save line"

// m22Submits are the page's submit buttons in document order: every
// <button> whose type isn't button or reset, and every input of type submit
// or image.
func m22Submits(page *goquery.Document) *goquery.Selection {
	return page.Find("button, input").FilterFunction(func(_ int, s *goquery.Selection) bool {
		typ := strings.ToLower(strings.TrimSpace(s.AttrOr("type", "")))
		if goquery.NodeName(s) == "input" {
			return typ == "submit" || typ == "image"
		}
		return typ == "" || typ == "submit"
	})
}

// m22Owner is the id of the form that submit s submits: its form attribute,
// else its nearest form ancestor's id.
func m22Owner(s *goquery.Selection) string {
	if f, ok := s.Attr("form"); ok {
		return f
	}
	return s.Closest("form").AttrOr("id", "")
}

// m22Default is form id's default button, the one Enter in a field presses:
// the first submit in tree order whose form owner is that form (HTML
// implicit submission).
func m22Default(page *goquery.Document, id string) *goquery.Selection {
	return m22Submits(page).FilterFunction(func(_ int, s *goquery.Selection) bool { return m22Owner(s) == id }).First()
}

// m22EnterCopy reports whether s is components.EnterSubmit's copy of save:
// an empty, aria-hidden, tabindex -1 submit inside the form, with save's
// name and value (CD-286 R4).
func m22EnterCopy(s, save *goquery.Selection) bool {
	return s.HasClass("enter-submit") && s.AttrOr("aria-hidden", "") == "true" && s.AttrOr("tabindex", "") == "-1" &&
		norm(s.Text()) == "" && s.AttrOr("name", "") == save.AttrOr("name", "") && s.AttrOr("value", "") == save.AttrOr("value", "")
}

// m22WantBar fails unless the line page has Save line once, as the
// ActionBar's primary (the bar's first button, Primary variant), outside
// every form and posting form#line-form through form="line-form"; Enter in
// a line field presses it (the form's default button is Save line or its
// Enter copy, and the form holds no other submit); the version stays in the
// form and the Back link still leads to items.
func m22WantBar(t *testing.T, page *goquery.Document, items, what string) {
	t.Helper()
	form := page.Find("form#line-form")
	if form.Length() != 1 {
		t.Fatalf("%s: found %d form#line-form, want 1", what, form.Length())
	}
	if form.Find(`input[name="version"]`).Length() != 1 {
		t.Errorf("%s: form#line-form lost its version input", what)
	}
	saves := m22Submits(page).FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == m22SaveLabel })
	if saves.Length() != 1 {
		t.Fatalf("%s: found %d %q submits, want exactly 1 (in the sticky bar)", what, saves.Length(), m22SaveLabel)
	}
	save := saves.First()
	if got, ok := save.Attr("form"); !ok || got != "line-form" {
		t.Errorf("%s: %s has form=%q (set %v), want form=\"line-form\"", what, m22SaveLabel, got, ok)
	}
	if n := save.Closest("form").Length(); n != 0 {
		t.Errorf("%s: %s sits inside a form (id %q), want it outside the line form, in the sticky bar", what, m22SaveLabel, save.Closest("form").AttrOr("id", ""))
	}
	bar := save.Closest(".actionbar")
	if bar.Length() != 1 {
		t.Fatalf("%s: %s is not inside an .actionbar", what, m22SaveLabel)
	}
	if bar.Closest("form").Length() != 0 {
		t.Errorf("%s: the .actionbar sits inside a form", what)
	}
	if first := bar.Find("button, a").First(); first.Get(0) != save.Get(0) {
		t.Errorf("%s: the bar's first control is %q, want %s as the primary", what, norm(first.Text()), m22SaveLabel)
	}
	if !save.HasClass("btn-primary") {
		t.Errorf("%s: %s class = %q, want the Primary variant (btn-primary)", what, m22SaveLabel, save.AttrOr("class", ""))
	}
	if !inOrder(page, form, bar) {
		t.Errorf("%s: the bar is before the line form, want it after", what)
	}

	def := m22Default(page, "line-form")
	switch {
	case def.Length() == 0:
		t.Errorf("%s: form#line-form has no default button: Enter in a field submits nothing", what)
	case def.Get(0) == save.Get(0):
	case m22EnterCopy(def, save):
	default:
		t.Errorf("%s: Enter in a field presses %q (class %q), want %s or its Enter copy", what, norm(def.Text()), def.AttrOr("class", ""), m22SaveLabel)
	}
	form.Find("button, input").Each(func(_ int, s *goquery.Selection) {
		if m22Submits(page).IndexOfSelection(s) >= 0 && !m22EnterCopy(s, save) {
			t.Errorf("%s: form#line-form still holds a submit %q, want Save line only in the bar", what, norm(s.Text()))
		}
	})

	back := page.Find("main .wizard-head a.wizard-close")
	if back.Length() != 1 || back.AttrOr("href", "") != items {
		t.Errorf("%s: %d ‹ in the wizard head (href %q), want one leading to %s", what, back.Length(), back.AttrOr("href", ""), items)
	}
}

// HOOK-039 (CD-275 UX-E4): with Module.Line, Save line is the sticky bar's
// primary posting form="line-form", on the GET of a new and an existing
// line (full page and htmx) and on the 422 and 409 redraws; Enter in a field
// submits it; Back stays. Without Module.Line the generic line pages stay
// byte-identical (HOOK-001) and keep Save line inside the form.
func TestModule_LineBar(t *testing.T) {
	t.Run("GET", func(t *testing.T) {
		e, id, _, _ := m20TopEnv(t)
		base := m01bURL(id)
		for _, path := range []string{"/lines/new", "/lines/1", "/lines/2"} {
			for _, hx := range []bool{false, true} {
				what := "GET " + path + " htmx " + m22Bool(hx)
				m22WantBar(t, m01bOK(t, e.get(t, base+path, m20Hdr(hx)...), what), base+"/edit/items", what)
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
				m22WantBar(t, uitest.Doc(t, rec), base+"/edit/items", what)
			}
		}
	})

	t.Run("409 redraw", func(t *testing.T) {
		e, id, _, _ := m20TopEnv(t)
		base := m01bURL(id)
		old := m01bVersion(t, e, id)
		location(t, e.post(t, base+"/lines/2", url.Values{"version": {old}, "qty": {"6"}}))
		for _, path := range []string{"/lines/new", "/lines/1"} {
			what := "POST " + path + " with a stale version"
			rec := e.post(t, base+path, url.Values{"version": {old}, "qty": {"9"}})
			m03Status(t, rec.Code, http.StatusConflict, what, rec.Body.String())
			m22WantBar(t, uitest.Doc(t, rec), base+"/edit/items", what)
		}
	})

	t.Run("no Line has the bar too", func(t *testing.T) {
		e, id := m20Env(t, 2, docscreen.Module{Hide: m01bHide(), LineNoun: m20Noun})
		base := m01bURL(id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			what := "GET " + path + " without Module.Line"
			m22WantBar(t, m01bOK(t, e.get(t, base+path), what), base+"/edit/items", what)
		}
	})

	t.Run("no Line is byte-identical", func(t *testing.T) {
		n := 0
		for _, p := range goldenPages() {
			if !strings.HasPrefix(p.name, "line-") {
				continue
			}
			n++
			t.Run(p.name, func(t *testing.T) {
				got := renderGolden(t, p, func(d *docscreen.Deps) {
					d.Modules = map[string]docscreen.Module{docscreentest.Sample: {}, docscreentest.Hooked: {Line: (&m03Hooks{}).line()}}
				})
				wantGolden(t, p.name, got)
			})
		}
		if n < 4 {
			t.Fatalf("goldenPages has %d line pages, want the 4 captured (line-1, line-new, line-1-422, line-1-remove)", n)
		}
	})
}

func m22Bool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// m22Totals is a Check whose Totals are Less advance and To pay.
func m22Totals(docscreen.Ctx) (docscreen.CheckExtra, error) {
	return docscreen.CheckExtra{Totals: []components.Pair{
		{Label: m04LessLabel, Value: m04LessValue, Num: true},
		{Label: m04PayLabel, Value: m04PayValue, Num: true},
	}}, nil
}

// m22Row is the summary row label: value inside sel.
func m22Row(sel *goquery.Selection, label, value string) *goquery.Selection {
	return sel.Find(".summary-row").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Find("dt").Text()) == label && norm(s.Find("dd").Text()) == value
	})
}

// HOOK-042 (CD-275 UX-E6): CheckExtra.Totals render after the grand total
// and its amount in words, in their order, on the check page's GET and its
// redraws (full page and htmx); nil Totals give today's check page byte for
// byte.
func TestModule_CheckTotalsAfter(t *testing.T) {
	e := newHookedEnv(t)
	id := m04OrderDraft(t, e)
	check := m04URL(fixture.Order, id) + "/edit/check"
	m04Mount(e, fixture.Order, docscreen.Module{Check: m22Totals})
	amount, err := types.ParseMoney(m04Amount)
	if err != nil {
		t.Fatal(err)
	}
	total := types.FormatINR(amount)

	for _, c := range []struct {
		what string
		get  bool
		hdr  []string
	}{
		{what: "GET check", get: true},
		{what: "htmx GET check", get: true, hdr: []string{"HX-Request", "true"}},
		{what: "stale POST check"},
		{what: "htmx stale POST check", hdr: []string{"HX-Request", "true"}},
	} {
		var page *goquery.Document
		if c.get {
			page = m04Page(t, e.get(t, check, c.hdr...), http.StatusOK, c.what)
		} else {
			page = m04Page(t, e.post(t, check, url.Values{"version": {"99"}}, c.hdr...), http.StatusConflict, c.what)
		}
		wiz := page.Find("#wizard")
		grand := wiz.Find("p.num").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == total })
		if grand.Length() != 1 {
			t.Fatalf("%s: grand total %s shown %d times, want once", c.what, total, grand.Length())
		}
		words := wiz.Find("p.in-words")
		if words.Length() != 1 || !inOrder(page, grand, words) {
			t.Fatalf("%s: %d amount-in-words lines, want one after the grand total", c.what, words.Length())
		}
		less, pay := m22Row(wiz, m04LessLabel, m04LessValue), m22Row(wiz, m04PayLabel, m04PayValue)
		if less.Length() != 1 || pay.Length() != 1 {
			t.Fatalf("%s: %s shown %d times and %s %d times, want each once", c.what, m04LessLabel, less.Length(), m04PayLabel, pay.Length())
		}
		if !inOrder(page, grand, less) || !inOrder(page, words, less) || words.Contains(less.Get(0)) {
			t.Errorf("%s: %s is before the grand total or its words, want after both (UX-E6)", c.what, m04LessLabel)
		}
		if !inOrder(page, less, pay) {
			t.Errorf("%s: %s comes before %s, want the module's order", c.what, m04PayLabel, m04LessLabel)
		}
	}

	t.Run("nil Totals match the captured check pages", func(t *testing.T) {
		none := docscreen.Module{Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) { return docscreen.CheckExtra{}, nil }}
		n := 0
		for _, p := range goldenPages() {
			if !strings.Contains(p.name, "check") && !strings.HasPrefix(p.name, "post-submit-422") {
				continue
			}
			n++
			t.Run(p.name, func(t *testing.T) {
				got := renderGolden(t, p, func(d *docscreen.Deps) {
					d.Modules = map[string]docscreen.Module{docscreentest.Sample: none, shipType: none}
				})
				wantGolden(t, p.name, got)
			})
		}
		if n < 5 {
			t.Fatalf("goldenPages has %d check pages, want step-check, step-check-htmx, ship-check and post-submit-422 (both)", n)
		}
	})

	t.Run("nil Totals is today's page", func(t *testing.T) {
		for _, hdr := range [][]string{nil, {"HX-Request", "true"}} {
			m04Mount(e, fixture.Order, docscreen.Module{})
			want := e.get(t, check, hdr...)
			m04Mount(e, fixture.Order, docscreen.Module{Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) { return docscreen.CheckExtra{}, nil }})
			got := e.get(t, check, hdr...)
			if got.Code != http.StatusOK || want.Code != http.StatusOK {
				t.Fatalf("GET check = %d and %d, want 200", got.Code, want.Code)
			}
			if !bytes.Equal(got.Body.Bytes(), want.Body.Bytes()) {
				t.Errorf("GET check (headers %v) with nil Totals differs from the page without a Check:\n got: %.400s\nwant: %.400s", hdr, m22Diff(got.Body.String(), want.Body.String()), m22Diff(want.Body.String(), got.Body.String()))
			}
		}
	})
}

// m22Diff is a from its first byte that differs from b.
func m22Diff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[i:]
}
