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

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-20 (CD-270, CD-275; hooks spec §7 item 6; expense UX §5.4, §5.5, §6
// UX-E1, UX-E2, EUX-037): Line.Top, Module.LineNoun and Module.AddTop. Every
// test runs on docscreentest.Hooked on a fixture database, the real engine and
// the real AUTH service (CD-125), through the m01b and m03 harnesses.

const (
	m20TopMarker = "M20 line top"
	m20Sha       = "m20sha256posted"
	m20Empty     = "No expenses yet. Add one for each bill."
)

// m20Noun is the expense claim's LineNoun (CD-270 Q1).
var m20Noun = docscreen.LineNoun{Add: "Add an expense", Title: "Expense", Empty: m20Empty}

// m20Env is the Hooked harness with one draft saved by the open user on
// METRO-SITE with n lines (items CEMENT-OPC, WIRE-18G, BOLT-M12 in turn), and
// m as Hooked's module.
func m20Env(t *testing.T, n int, m docscreen.Module) (*hookedEnv, int64) {
	t.Helper()
	e := newHookedEnv(t)
	fm := e.fx.M
	items := []string{"CEMENT-OPC", "WIRE-18G", "BOLT-M12"}
	lines := []map[string]string{}
	for i := range n {
		lines = append(lines, map[string]string{
			"project_id": itoa(fm.Project["PRJ-METRO"]), "item_id": itoa(fm.Item[items[i%len(items)]]),
			"qty": strconv.Itoa(i+1) + ".000", "note": "m20 line " + strconv.Itoa(i+1),
		})
	}
	res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(fm.Party["SHREE"]), "site_id": itoa(e.site["METRO-SITE"]), "ref": "HK-REF"},
		Lines:  lines,
	})
	if err != nil {
		t.Fatalf("saving a Hooked draft with %d lines: %v", n, err)
	}
	if got := len(m01bRecord(t, e, res.ID).Lines); got != n {
		t.Fatalf("stored lines = %d, want %d", got, n)
	}
	m01bMount(e, docscreentest.Hooked, m)
	return e, res.ID
}

// m20Call is one call of Line.Top.
type m20Call struct {
	No     int
	Posted bool
	ViewID int64
}

// m20Top is a Line.Top that renders #m20-top with its own upload form (a form
// that may not sit inside the line form) and, when Posted, the posted
// receipt_sha256 in #m20-top-posted, as the expense receipt area redraws it.
type m20Top struct {
	err   error
	mu    sync.Mutex
	calls []m20Call
}

func (h *m20Top) top(c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	call := m20Call{No: no, Posted: s.Posted}
	if c.View != nil {
		call.ViewID = c.View.Info.ID
	}
	h.mu.Lock()
	h.calls = append(h.calls, call)
	h.mu.Unlock()
	if h.err != nil {
		return nil, h.err
	}
	posted := ""
	if s.Posted {
		posted = c.Req.PostFormValue("receipt_sha256")
	}
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<div id="m20-top"><p>`+m20TopMarker+`</p>`+
			`<form id="m20-top-form" method="post" action="/modules/m20/receipt"><input type="file" name="m20-file"></form>`+
			`<p id="m20-top-posted">`+templ.EscapeString(posted)+`</p></div>`)
		return err
	}), nil
}

// taken returns the calls so far and forgets them.
func (h *m20Top) taken() []m20Call {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.calls
	h.calls = nil
	return out
}

// m20TopEnv is the two-line Hooked draft with a Line module: m03Hooks' Body
// and Save, and m20Top as Top.
func m20TopEnv(t *testing.T) (*hookedEnv, int64, *m03Hooks, *m20Top) {
	t.Helper()
	e, id := m20Env(t, 2, docscreen.Module{})
	h, top := &m03Hooks{e: e}, &m20Top{}
	l := h.line()
	l.Top = top.top
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Line: l})
	return e, id, h, top
}

// m20Pos is sel's first element's place in the page in document order, or -1.
func m20Pos(page *goquery.Document, sel *goquery.Selection) int {
	if sel.Length() == 0 {
		return -1
	}
	return page.Find("*").IndexOfSelection(sel.First())
}

// m20WantTop fails unless the page has Line.Top once, after the h1 and before
// the line form, outside every form, with its own form intact (so not nested
// in the line form, where the parser would drop it); and the line form
// (id line-form, posting to action) holds the version and Line.Body.
func m20WantTop(t *testing.T, page *goquery.Document, action, what string) {
	t.Helper()
	top := page.Find("#m20-top")
	if top.Length() != 1 || !strings.Contains(norm(top.Text()), m20TopMarker) {
		t.Fatalf("%s: found %d #m20-top with the marker, want Line.Top once", what, top.Length())
	}
	form := page.Find("form#line-form")
	if form.Length() != 1 {
		t.Fatalf("%s: found %d form#line-form, want the line form with id line-form", what, form.Length())
	}
	if got, _ := form.Attr("action"); got != action {
		t.Errorf("%s: form#line-form posts to %q, want %q", what, got, action)
	}
	if form.Find(`input[name="version"]`).Length() != 1 {
		t.Errorf("%s: form#line-form has no version input", what)
	}
	if form.Find("#m03-body").Length() != 1 {
		t.Errorf("%s: Line.Body is not inside form#line-form", what)
	}
	if n := top.ParentsFiltered("form").Length(); n != 0 {
		t.Errorf("%s: Line.Top sits inside %d forms, want none (outside the line form)", what, n)
	}
	if n := page.Find("#m20-top #m20-top-form").Length(); n != 1 {
		t.Errorf("%s: found %d forms of Top's own inside #m20-top, want 1 (a nested form is legal outside the line form)", what, n)
	}
	h1 := page.Find("main h1")
	if h1.Length() != 1 {
		t.Fatalf("%s: found %d main h1, want 1", what, h1.Length())
	}
	if p, ph := m20Pos(page, top), m20Pos(page, h1); p < ph {
		t.Errorf("%s: Line.Top (at %d) is before the h1 (at %d), want it after", what, p, ph)
	}
	if p, pf := m20Pos(page, top), m20Pos(page, form); p > pf {
		t.Errorf("%s: Line.Top (at %d) is after the line form (at %d), want it before", what, p, pf)
	}
}

// m20Hdr is the htmx request header when hx.
func m20Hdr(hx bool) []string {
	if hx {
		return []string{"HX-Request", "true"}
	}
	return nil
}

// HOOK-033 (UX-E1, CD-275): Line.Top renders after the h1 and outside the
// line form, which gets id="line-form" only when Module.Line is set (HOOK-001
// stays byte-identical); Top gets the line no and a StepState with Posted true
// on a 422 and 409 redraw, so it can redraw the posted receipt; an error is a
// 500; a form inside Top is legal.
func TestModule_LineTop(t *testing.T) {
	t.Run("GET", func(t *testing.T) {
		e, id, _, top := m20TopEnv(t)
		base := m01bURL(id)
		for _, c := range []struct {
			path string
			no   int
		}{{"/lines/new", 0}, {"/lines/1", 1}, {"/lines/2", 2}} {
			for _, hx := range []bool{false, true} {
				what := "GET " + c.path + " htmx " + strconv.FormatBool(hx)
				page := m01bOK(t, e.get(t, base+c.path, m20Hdr(hx)...), what)
				m20WantTop(t, page, base+c.path, what)
				if got := norm(page.Find("#m20-top-posted").Text()); got != "" {
					t.Errorf("%s: Top shows a posted receipt %q on a GET", what, got)
				}
				want := []m20Call{{No: c.no, Posted: false, ViewID: id}}
				if got := top.taken(); !slices.Equal(got, want) {
					t.Errorf("%s: Top calls = %+v, want %+v", what, got, want)
				}
			}
		}
	})

	t.Run("422 redraw", func(t *testing.T) {
		e, id, _, top := m20TopEnv(t)
		base := m01bURL(id)
		version := m01bVersion(t, e, id)
		for _, c := range []struct {
			path string
			no   int
		}{{"/lines/new", 0}, {"/lines/1", 1}} {
			what := "POST " + c.path + " with qty abc"
			rec := e.post(t, base+c.path, url.Values{"version": {version}, "qty": {"abc"}, "receipt_sha256": {m20Sha}})
			m03Status(t, rec.Code, http.StatusUnprocessableEntity, what, rec.Body.String())
			page := uitest.Doc(t, rec)
			m20WantTop(t, page, base+c.path, what)
			if got := norm(page.Find("#m20-top-posted").Text()); got != m20Sha {
				t.Errorf("%s: Top shows the posted receipt %q, want %q (Top reads c.Req)", what, got, m20Sha)
			}
			want := []m20Call{{No: c.no, Posted: true, ViewID: id}}
			if got := top.taken(); !slices.Equal(got, want) {
				t.Errorf("%s: Top calls = %+v, want %+v", what, got, want)
			}
		}
	})

	t.Run("409 redraw", func(t *testing.T) {
		e, id, _, top := m20TopEnv(t)
		base := m01bURL(id)
		old := m01bVersion(t, e, id)
		location(t, e.post(t, base+"/lines/2", url.Values{"version": {old}, "qty": {"6"}}))
		top.taken()
		for _, c := range []struct {
			path string
			no   int
		}{{"/lines/new", 0}, {"/lines/1", 1}} {
			what := "POST " + c.path + " with a stale version"
			rec := e.post(t, base+c.path, url.Values{"version": {old}, "qty": {"9"}, "receipt_sha256": {m20Sha}})
			m03Status(t, rec.Code, http.StatusConflict, what, rec.Body.String())
			page := uitest.Doc(t, rec)
			m20WantTop(t, page, base+c.path, what)
			if got := norm(page.Find("#m20-top-posted").Text()); got != m20Sha {
				t.Errorf("%s: Top shows the posted receipt %q, want %q", what, got, m20Sha)
			}
			calls := top.taken()
			if n := len(calls); n == 0 || calls[n-1] != (m20Call{No: c.no, Posted: true, ViewID: id}) {
				t.Errorf("%s: Top calls = %+v, want the redraw last with no %d and Posted true", what, calls, c.no)
			}
		}
	})

	t.Run("error is 500", func(t *testing.T) {
		e, id, _, top := m20TopEnv(t)
		top.err = errors.New("m20 top failed")
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			rec := e.get(t, base+path)
			m03Status(t, rec.Code, http.StatusInternalServerError, "GET "+path+" with a failing Top", rec.Body.String())
			rec = e.post(t, base+path, url.Values{"version": {strconv.Itoa(before.Version)}, "qty": {"abc"}})
			m03Status(t, rec.Code, http.StatusInternalServerError, "POST "+path+" 422 redraw with a failing Top", rec.Body.String())
			m03Unchanged(t, e, id, before, "a 422 with a failing Top on "+path)
		}
		if len(top.taken()) == 0 {
			t.Error("Top was never called: the 500s above prove nothing")
		}
	})

	t.Run("no Line, no form id", func(t *testing.T) {
		e, id := m20Env(t, 2, docscreen.Module{Hide: m01bHide(), LineNoun: m20Noun})
		base := m01bURL(id)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			page := m01bOK(t, e.get(t, base+path), "GET "+path+" without Module.Line")
			if n := page.Find("#line-form").Length(); n != 0 {
				t.Errorf("GET %s without Module.Line: %d #line-form, want none (CD-275: only with Module.Line)", path, n)
			}
			if n := page.Find(`form[action="` + base + path + `"] [name="qty"]`).Length(); n != 1 {
				t.Errorf("GET %s without Module.Line: %d generic qty inputs in the line form, want 1", path, n)
			}
		}
	})
}

// m20Adds are the page's (or the htmx fragment's) links to the new-line page.
func m20Adds(page *goquery.Document, id int64) *goquery.Selection {
	return page.Find(`a[href="` + m01bURL(id) + `/lines/new"]`)
}

// m20Title fails unless the page's h1 reads want and, on a full page, its
// <title> starts with it.
func m20Title(t *testing.T, page *goquery.Document, want, what string) {
	t.Helper()
	if got := norm(page.Find("main h1").Text()); got != want {
		t.Errorf("%s: h1 = %q, want %q", what, got, want)
	}
	title := page.Find("head title")
	if title.Length() == 0 {
		return // an htmx fragment has no <title>
	}
	if got := strings.TrimPrefix(norm(title.Text()), "Error: "); !strings.HasPrefix(got, want+" ·") {
		t.Errorf("%s: <title> = %q, want it to start with %q", what, got, want)
	}
}

// HOOK-034 (UX-E2, CD-270 Q1, CD-275): LineNoun.Add labels the Items step's
// add button and the new-line page's title; Title names an existing line's
// page ("Expense 3"); Empty is the Items step's text with no lines. A zero
// LineNoun keeps "Add an item", "Line {n}" and today's Items step, byte for
// byte.
func TestModule_LineNoun(t *testing.T) {
	t.Run("Items step", func(t *testing.T) {
		for _, n := range []int{0, 3} {
			e, id := m20Env(t, n, docscreen.Module{LineNoun: m20Noun})
			for _, hx := range []bool{false, true} {
				what := "items step with " + strconv.Itoa(n) + " lines, htmx " + strconv.FormatBool(hx)
				rec := e.get(t, m01bURL(id)+"/edit/items", m20Hdr(hx)...)
				page := m01bOK(t, rec, what)
				adds := m20Adds(page, id)
				if adds.Length() != 1 {
					t.Fatalf("%s: %d add links, want 1", what, adds.Length())
				}
				if got := norm(adds.Text()); got != m20Noun.Add {
					t.Errorf("%s: add button reads %q, want %q", what, got, m20Noun.Add)
				}
				if strings.Contains(rec.Body.String(), "Add an item") {
					t.Errorf("%s: the page still says Add an item", what)
				}
				main := norm(page.Find("body").Text())
				if n == 0 && !strings.Contains(main, m20Empty) {
					t.Errorf("%s: page text %q, want the Empty text %q", what, main, m20Empty)
				}
				if n > 0 && strings.Contains(main, m20Empty) {
					t.Errorf("%s: the Empty text shows beside %d lines", what, n)
				}
			}
		}
	})

	t.Run("line page titles", func(t *testing.T) {
		e, id := m20Env(t, 3, docscreen.Module{})
		base := m01bURL(id)
		h := &m03Hooks{e: e}
		for _, c := range []struct {
			name string
			m    docscreen.Module
		}{
			{"generic line page", docscreen.Module{LineNoun: m20Noun}},
			{"Module.Line", docscreen.Module{LineNoun: m20Noun, Line: h.line()}},
		} {
			m01bMount(e, docscreentest.Hooked, c.m)
			for _, hx := range []bool{false, true} {
				what := c.name + " htmx " + strconv.FormatBool(hx)
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/new", m20Hdr(hx)...), what+" /lines/new"), "Add an expense", what+" /lines/new")
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/3", m20Hdr(hx)...), what+" /lines/3"), "Expense 3", what+" /lines/3")
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/1", m20Hdr(hx)...), what+" /lines/1"), "Expense 1", what+" /lines/1")
			}
			version := m01bVersion(t, e, id)
			for _, p := range []struct{ path, title string }{{"/lines/new", "Add an expense"}, {"/lines/3", "Expense 3"}} {
				rec := e.post(t, base+p.path, url.Values{"version": {version}, "qty": {"abc"}})
				m03Status(t, rec.Code, http.StatusUnprocessableEntity, c.name+" POST "+p.path+" with qty abc", rec.Body.String())
				m20Title(t, uitest.Doc(t, rec), p.title, c.name+" 422 on "+p.path)
			}
		}
	})

	t.Run("zero LineNoun is today's", func(t *testing.T) {
		for _, n := range []int{0, 2} {
			e, id := m20Env(t, n, docscreen.Module{})
			base := m01bURL(id)
			paths := []string{"/edit/items", "/lines/new"}
			if n > 0 {
				paths = append(paths, "/lines/1", "/lines/2")
			}
			for _, path := range paths {
				for _, hx := range []bool{false, true} {
					what := "GET " + path + " with " + strconv.Itoa(n) + " lines, htmx " + strconv.FormatBool(hx)
					e.d.Modules = nil
					e.remount()
					none := e.get(t, base+path, m20Hdr(hx)...)
					m01bMount(e, docscreentest.Hooked, docscreen.Module{Hide: m01bHide()})
					zero := e.get(t, base+path, m20Hdr(hx)...)
					if none.Code != http.StatusOK || zero.Code != http.StatusOK {
						t.Fatalf("%s: %d without a module, %d with a zero LineNoun; want 200", what, none.Code, zero.Code)
					}
					if none.Body.String() != zero.Body.String() {
						t.Errorf("%s: a module with a zero LineNoun changes the page:\nwithout %.600s\nwith    %.600s", what, none.Body, zero.Body)
					}
				}
			}
			page := m01bOK(t, e.get(t, base+"/edit/items"), "items step")
			if got := norm(m20Adds(page, id).Text()); got != "Add an item" {
				t.Errorf("items step with %d lines and a zero LineNoun: add button %q, want Add an item", n, got)
			}
			m20Title(t, m01bOK(t, e.get(t, base+"/lines/new"), "/lines/new"), "Add an item", "zero LineNoun /lines/new")
			if n > 0 {
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/2"), "/lines/2"), "Line 2", "zero LineNoun /lines/2")
				m01bMount(e, docscreentest.Hooked, docscreen.Module{Line: (&m03Hooks{e: e}).line()})
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/new"), "/lines/new"), "Add an item", "Module.Line, zero LineNoun /lines/new")
				m20Title(t, m01bOK(t, e.get(t, base+"/lines/2"), "/lines/2"), "Line 2", "Module.Line, zero LineNoun /lines/2")
			}
		}
	})
}

// m20WantAdds fails unless the Items step of draft id with n lines has the
// add button above the cards and, from 4 lines, again after the cards and
// the totals (outside #doc-totals, which a grid save swaps), each reading
// label.
func m20WantAdds(t *testing.T, page *goquery.Document, id int64, n int, label, what string) {
	t.Helper()
	adds := m20Adds(page, id)
	want := 1
	if n >= 4 {
		want = 2
	}
	if adds.Length() != want {
		t.Fatalf("%s: %d add buttons, want %d", what, adds.Length(), want)
	}
	adds.Each(func(i int, a *goquery.Selection) {
		if got := norm(a.Text()); got != label {
			t.Errorf("%s: add button %d reads %q, want %q", what, i+1, got, label)
		}
	})
	cards := page.Find(".lines-card")
	if cards.Length() != n {
		t.Fatalf("%s: %d cards, want %d", what, cards.Length(), n)
	}
	first := m20Pos(page, adds.First())
	if n > 0 {
		if pc := m20Pos(page, cards.First()); first > pc {
			t.Errorf("%s: the add button (at %d) is below the first card (at %d), want it above the cards", what, first, pc)
		}
	} else if ol := page.Find("ol.lines"); ol.Length() > 0 {
		if po := m20Pos(page, ol); first > po {
			t.Errorf("%s: the add button (at %d) is below the cards' list (at %d), want it above", what, first, po)
		}
	}
	if want == 1 {
		return
	}
	totals := page.Find("#doc-totals")
	if totals.Length() != 1 {
		t.Fatalf("%s: %d #doc-totals, want 1", what, totals.Length())
	}
	second := adds.Eq(1)
	if second.ParentsFiltered("#doc-totals").Length() != 0 {
		t.Errorf("%s: the repeated add button is inside #doc-totals, want it after the totals", what)
	}
	if p, pt := m20Pos(page, second), m20Pos(page, totals); p < pt {
		t.Errorf("%s: the repeated add button (at %d) is before the totals (at %d), want it after", what, p, pt)
	}
	if p, pc := m20Pos(page, second), m20Pos(page, cards.Last()); p < pc {
		t.Errorf("%s: the repeated add button (at %d) is before the last card (at %d), want it after the cards", what, p, pc)
	}
}

// HOOK-035 (UX-E2, CD-275, EUX-037): with AddTop the add button sits above
// the cards; with 4 or more lines it is repeated after the cards and the
// totals; with 0 and 3 lines only the top one shows. LineNoun.Add labels
// both. Without AddTop, one button after the cards, as today.
func TestModule_AddTop(t *testing.T) {
	for _, n := range []int{0, 3, 4, 5} {
		e, id := m20Env(t, n, docscreen.Module{})
		for _, c := range []struct {
			name  string
			m     docscreen.Module
			label string
		}{
			{"AddTop", docscreen.Module{AddTop: true}, "Add an item"},
			{"AddTop and CardsOnly", docscreen.Module{AddTop: true, CardsOnly: true}, "Add an item"},
			{"AddTop and LineNoun", docscreen.Module{AddTop: true, CardsOnly: true, LineNoun: m20Noun}, m20Noun.Add},
		} {
			m01bMount(e, docscreentest.Hooked, c.m)
			for _, hx := range []bool{false, true} {
				what := c.name + " with " + strconv.Itoa(n) + " lines, htmx " + strconv.FormatBool(hx)
				page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/items", m20Hdr(hx)...), what)
				m20WantAdds(t, page, id, n, c.label, what)
			}
		}

		m01bMount(e, docscreentest.Hooked, docscreen.Module{CardsOnly: true})
		what := "CardsOnly without AddTop with " + strconv.Itoa(n) + " lines"
		page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/items"), what)
		adds := m20Adds(page, id)
		if adds.Length() != 1 {
			t.Fatalf("%s: %d add buttons, want 1", what, adds.Length())
		}
		if cards := page.Find(".lines-card"); n > 0 && m20Pos(page, adds) < m20Pos(page, cards.Last()) {
			t.Errorf("%s: the add button is above the last card, want it after the cards as today", what)
		}
	}
}
