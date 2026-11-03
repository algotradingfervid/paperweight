package docscreen_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// M0-40b (CD-313, CD-315; purchase UX §5.6), on Hooked over the real engine
// and the real AUTH (CD-125). Module.LineGrid gives each line five display
// cells. Hooked's note field is hidden, so without LineGrid its rows have
// three editable cells and the editable grid would show (M0-40): with
// LineGrid it must never show. From 64rem the Items step, the check page
// and the document page draw the display grid (LineListProps.Display) with
// the cards kept for narrower widths. Change, Remove and Move are filled as
// for the cards and show only where the cards show them (the Items step of
// a draft the viewer may write); the note comes from LineCard.Note, and
// on the document page from PageExtra.LineNotes first (CD-233).

// Test values.
const (
	m40bPageNote = "m40b page note on line 2"
	m40bCodeSub  = "m40b-code-"
)

func m40bCardNote(no int) string { return "m40b card note on line " + strconv.Itoa(no) }

// m40bRate and m40bValue are line no's Rate and Value cells.
func m40bRate(no int) string  { return "1,234.500" + strconv.Itoa(no) }
func m40bValue(no int) string { return "₹9,876.0" + strconv.Itoa(no) }

// m40bHead is the display grid's head up to Amount.
var m40bHead = []string{"#", "Item", "Project", "Quantity", "Rate", "Value", "Amount"}

// m40bCalls records what LineGrid was called with.
type m40bCalls struct {
	n   int
	dts []string
	ids []int64
	nos []int
}

// m40bModule is Hooked's module: note hidden (three editable cells),
// LineCard adding a note on every line, LineGrid's five cells and a Page
// whose LineNotes put a note on line 2.
func m40bModule(calls *m40bCalls) docscreen.Module {
	return docscreen.Module{
		Hide: m01bHide("note"),
		LineCard: func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
			c := l.Card
			c.Note = m40bCardNote(l.No)
			return c
		},
		LineGrid: func(c docscreen.Ctx, l docscreen.LineView) []components.GridCell {
			calls.n++
			if c.DT != nil {
				calls.dts = append(calls.dts, c.DT.Name)
			}
			if c.View != nil {
				calls.ids = append(calls.ids, c.View.Info.ID)
			}
			calls.nos = append(calls.nos, l.No)
			return []components.GridCell{
				{Label: "Item", Text: l.Display["item_id"], Sub: m40bCodeSub + strconv.Itoa(l.No)},
				{Label: "Project", Text: l.Display["project_id"]},
				{Label: "Quantity", Text: l.Display["qty"], Kind: "qty"},
				{Label: "Rate", Text: m40bRate(l.No), Kind: "rate"},
				{Label: "Value", Text: m40bValue(l.No), Kind: "money"},
			}
		},
		Page: func(docscreen.Ctx) (docscreen.PageExtra, error) {
			return docscreen.PageExtra{LineNotes: map[int]string{2: m40bPageNote}}, nil
		},
	}
}

// m40bEnv is the Hooked harness with one two-line draft by the open user,
// m40bModule mounted, and a reader whose only right on Hooked is read.
type m40bEnv struct {
	*hookedEnv
	id     int64
	calls  *m40bCalls
	reader auth.Principal
}

func m40bNewEnv(t *testing.T) *m40bEnv {
	t.Helper()
	calls := &m40bCalls{}
	e, id := m01bHooked(t, m40bModule(calls))
	reader := m04User(t, e, "m40b-reader@vardhan.test", "M40b Reader", map[string][]auth.Right{docscreentest.Hooked: {"read"}}, 0)
	return &m40bEnv{hookedEnv: e, id: id, calls: calls, reader: reader}
}

// m40bLines are document id's lines as the open user's View gives them.
func (e *m40bEnv) m40bLines(t *testing.T) []docscreen.LineView {
	t.Helper()
	v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, e.id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	if len(v.Lines) != 2 {
		t.Fatalf("the draft has %d lines, want 2", len(v.Lines))
	}
	return v.Lines
}

// m40bWant is what a page's display grid must show.
type m40bWant struct {
	lines   []docscreen.LineView
	actions bool           // Change, Remove and Move on each row
	version string         // the move forms' version, with actions
	notes   map[int]string // line no → its note row
	absent  []string       // texts the grid must not show
}

// m40bWantDisplay fails unless page shows the display grid of want.lines
// in a grid list beside the line cards, with no editable cell anywhere on
// the page, and want's actions and notes.
func m40bWantDisplay(t *testing.T, e *m40bEnv, page *goquery.Document, want m40bWant, what string) {
	t.Helper()
	if page.Find(".linelist.linelist--grid .line-editor").Length() != 1 {
		t.Errorf("%s: no display grid in a grid list (.linelist.linelist--grid .line-editor)", what)
	}
	if n := page.Find(".linelist .lines .lines-card").Length(); n != 2 {
		t.Errorf("%s: %d line cards, want 2: cards still show below 64rem", what, n)
	}
	for sel, why := range map[string]string{
		"form.linegrid-form":               "a grid row form",
		`[id^="grid-1-"], [id^="grid-2-"]`: "an editable grid cell",
		`[hx-post*="/lines/"]`:             "an htmx line save",
		"#line-editor input:not([type=hidden]), #line-editor select, #line-editor textarea, .line-editor input:not([type=hidden])": "an input in the grid",
	} {
		if n := page.Find(sel).Length(); n != 0 {
			t.Errorf("%s: %d %s (%s), want none: lines are edited only on the line page", what, n, sel, why)
		}
	}
	ed := page.Find(".line-editor").First()
	var head []string
	ed.Find(".linegrid-head").Children().Each(func(_ int, s *goquery.Selection) { head = append(head, norm(s.Text())) })
	if len(head) < len(m40bHead) || !slices.Equal(head[:len(m40bHead)], m40bHead) {
		t.Errorf("%s: the grid head reads %q, want it to start %q", what, head, m40bHead)
	}
	rows := ed.Find(".linegrid .linegrid-row")
	if rows.Length() != 2 {
		t.Fatalf("%s: the display grid has %d rows, want 2", what, rows.Length())
	}
	text := norm(ed.Text())
	for i, l := range want.lines {
		no, row := l.No, rows.Eq(i)
		rt := norm(row.Text())
		for _, s := range []string{strconv.Itoa(no), l.Display["item_id"], m40bCodeSub + strconv.Itoa(no), l.Display["project_id"], l.Display["qty"], m40bRate(no), m40bValue(no)} {
			if s == "" || !strings.Contains(rt, s) {
				t.Errorf("%s: line %d's row %q lacks %q", what, no, rt, s)
			}
		}
		for kind, s := range map[string]string{"qty": l.Display["qty"], "rate": m40bRate(no), "money": m40bValue(no)} {
			if k := row.Find(`[data-kind="` + kind + `"]`); k.Length() != 1 || norm(k.Text()) != s {
				t.Errorf("%s: line %d has %d data-kind=%q cells reading %q, want one reading %q", what, no, k.Length(), kind, norm(k.Text()), s)
			}
		}
		if n, ok := want.notes[no]; ok {
			next := len(text)
			if i+1 < len(want.lines) {
				next = strings.Index(text, m40bCodeSub+strconv.Itoa(want.lines[i+1].No))
			}
			at := strings.Index(text, n)
			if at < strings.Index(text, m40bCodeSub+strconv.Itoa(no)) || at > next {
				t.Errorf("%s: line %d's note %q is not in its rows of the grid (%q)", what, no, n, text)
			}
		}
		m40bWantActions(t, ed, docscreentest.Hooked, e.id, no, len(want.lines), want, what)
	}
	for _, s := range want.absent {
		if strings.Contains(text, s) {
			t.Errorf("%s: the display grid shows %q", what, s)
		}
	}
	if !slices.Contains(e.calls.dts, docscreentest.Hooked) || !slices.Contains(e.calls.ids, e.id) || !slices.Contains(e.calls.nos, 1) || !slices.Contains(e.calls.nos, 2) {
		t.Errorf("%s: LineGrid called with DocTypes %q, documents %v, lines %v; want Hooked %d, lines 1 and 2", what, e.calls.dts, e.calls.ids, e.calls.nos, e.id)
	}
}

// m40bWantActions checks line no's actions in the grid ed: with
// want.actions, Change and Remove link to the line and remove pages and
// the first line moves down, the last up, posting want.version; without,
// none of them.
func m40bWantActions(t *testing.T, ed *goquery.Selection, dt string, id int64, no, n int, want m40bWant, what string) {
	t.Helper()
	base := "/doc/" + dt + "/" + itoa(id) + "/lines/" + strconv.Itoa(no)
	named := func(name string) *goquery.Selection {
		return ed.Find("a, button").FilterFunction(func(_ int, s *goquery.Selection) bool { return s.AttrOr("aria-label", "") == name })
	}
	links := map[string]string{"Change": base, "Remove": base + "/remove"}
	moves := map[string]string{}
	if no > 1 {
		moves["Move up"] = base + "/move?dir=up"
	}
	if no < n {
		moves["Move down"] = base + "/move?dir=down"
	}
	for verb, href := range links {
		name := verb + " line " + strconv.Itoa(no)
		a := named(name)
		switch {
		case !want.actions && a.Length() != 0:
			t.Errorf("%s: the grid has %q, want no line actions here", what, name)
		case want.actions && (a.Length() != 1 || a.AttrOr("href", "") != href):
			t.Errorf("%s: %d grid controls named %q (href %q), want one link to %q", what, a.Length(), name, a.AttrOr("href", ""), href)
		}
	}
	for verb, action := range moves {
		name := verb + " line " + strconv.Itoa(no)
		b := named(name)
		switch {
		case !want.actions && b.Length() != 0:
			t.Errorf("%s: the grid has %q, want no line actions here", what, name)
		case !want.actions:
		case b.Length() != 1 || b.Closest("form").AttrOr("action", "") != action:
			t.Errorf("%s: %d grid controls named %q, want one in a form posting to %q", what, b.Length(), name, action)
		case b.Closest("form").Find(`input[name="version"]`).AttrOr("value", "") != want.version:
			t.Errorf("%s: %q posts version %q, want the draft's %s (CD-94)", what, name, b.Closest("form").Find(`input[name="version"]`).AttrOr("value", ""), want.version)
		}
	}
	if !want.actions {
		if n := ed.Find(`form[action*="/lines/"], a[href*="/lines/"]`).Length(); n != 0 {
			t.Errorf("%s: the grid has %d line links or forms, want none", what, n)
		}
	}
}

// HOOK-060 (M0-40b, CD-313): on the Items step of a draft its writer
// opens, the display grid shows every line's LineGrid cells with Change,
// Remove and Move filled as for the cards, and LineCard's notes; the
// editable grid never renders, though Hooked's three editable cells would
// show it without LineGrid.
func TestLineGrid_DisplayItemsStep(t *testing.T) {
	e := m40bNewEnv(t)
	lines := e.m40bLines(t)
	page := m01bOK(t, e.get(t, m01bURL(e.id)+"/edit/items"), "GET items")
	m40bWantDisplay(t, e, page, m40bWant{
		lines: lines, actions: true, version: m01bVersion(t, e.hookedEnv, e.id),
		notes: map[int]string{1: m40bCardNote(1), 2: m40bCardNote(2)},
	}, "Items step")
}

// HOOK-060 (M0-40b, CD-313): the check page draws the display grid too,
// with LineCard's notes, and, like its cards, no line actions.
func TestLineGrid_DisplayCheckPage(t *testing.T) {
	e := m40bNewEnv(t)
	lines := e.m40bLines(t)
	page := m01bOK(t, e.get(t, m01bURL(e.id)+"/edit/check"), "GET check")
	m40bWantDisplay(t, e, page, m40bWant{
		lines: lines, notes: map[int]string{1: m40bCardNote(1), 2: m40bCardNote(2)},
	}, "check page")
}

// HOOK-061 (M0-40b, CD-313, CD-263 G1): a reader with only read on Hooked
// opening the draft gets the read-only document page, whose display grid
// has no line actions; its notes are LineCard's, with PageExtra.LineNotes
// first on line 2 (CD-233).
func TestLineGrid_DisplayReaderDraft(t *testing.T) {
	e := m40bNewEnv(t)
	lines := e.m40bLines(t)
	e.who = e.reader
	page := m01bOK(t, e.get(t, m01bURL(e.id)), "reader GET the draft")
	if norm(page.Find(".doc-head__top .badge--draft").Text()) != "Draft" {
		t.Fatal("the reader's page is not the draft's document page")
	}
	m40bWantDisplay(t, e, page, m40bWant{
		lines: lines, notes: map[int]string{1: m40bCardNote(1), 2: m40bPageNote}, absent: []string{m40bCardNote(2)},
	}, "reader's draft page")
}

// HOOK-061 (M0-40b, CD-313, CD-233): an approved document's page draws the
// display grid for its writer with no line actions; line 2's note is
// PageExtra.LineNotes', line 1 keeps LineCard's.
func TestLineGrid_DisplayApprovedPage(t *testing.T) {
	e := m40bNewEnv(t)
	lines := e.m40bLines(t)
	m01bSubmit(t, e.hookedEnv, e.id)
	page := m01bOK(t, e.get(t, m01bURL(e.id)), "GET the approved document")
	m40bWantDisplay(t, e, page, m40bWant{
		lines: lines, notes: map[int]string{1: m40bCardNote(1), 2: m40bPageNote}, absent: []string{m40bCardNote(2)},
	}, "approved document page")
}

// HOOK-062 (M0-40b, CD-313): with LineGrid, an htmx line save answers as
// for the cards, 303 to the Items step, and saves the line; without
// LineGrid the same three-cell draft answers with its grid row (M0-40).
func TestLineGrid_DisplayLineSave(t *testing.T) {
	e := m40bNewEnv(t)
	rec := e.post(t, m01bURL(e.id)+"/lines/1", e.m01bLine1(m01bVersion(t, e.hookedEnv, e.id)), "HX-Request", "true")
	if loc := rec.Header().Get("HX-Redirect"); rec.Code != http.StatusOK || loc != m01bURL(e.id)+"/edit/items" {
		t.Errorf("htmx line save = %d to %q, want HX-Redirect to %s; body %.300s", rec.Code, loc, m01bURL(e.id)+"/edit/items", rec.Body)
	}
	if got := m01bRecord(t, e.hookedEnv, e.id).Lines[0]["qty"]; got != "7.000" && got != "7" {
		t.Errorf("line 1's quantity after the save = %q, want 7", got)
	}
	if body := rec.Body.String(); strings.Contains(body, "grid-row-1") {
		t.Errorf("the save answered with a grid row: %.300s", body)
	}
}
