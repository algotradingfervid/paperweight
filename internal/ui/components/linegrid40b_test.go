package components_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
)

// M0-40b (CD-313, purchase UX §5.6): with LineListProps.Display the grid
// rows are read-only. A cell with no Input renders its Text, with Sub as a
// muted second line; a cell of Kind qty, rate or money carries
// data-kind="<Kind>" and the num class (right-aligned tabular numerals) on
// the element holding its Text. A display row has no row form, no htmx post
// and no input; its actions are Change and Remove (links named by lineAria)
// and the Move up and Move down forms, each posting only the hidden csrf
// token and version. The cards still render, for widths below 64rem. With
// Display false the list renders byte for byte as before M0-40b.

// Test values: the largest values a purchase line can hold (CD-169).
const (
	m40bItem1   = "OPC Cement 53 Grade"
	m40bSub1    = "CEMENT-OPC-53-GRADE-ULTRATECH"
	m40bQty1    = "1,00,000"
	m40bUnit1   = "kg"
	m40bRate1   = "10,00,00,000.0000"
	m40bValue1  = "₹1,23,45,678.90"
	m40bAmount1 = "₹10,00,00,00,000.00"
	m40bNote1   = "Last bought at ₹345.0000 from Shree Steel Traders on 12-10-2026"
	m40bItem2   = "Binding wire 18 gauge"
	m40bSub2    = "WIRE-18G"
	m40bQty2    = "5"
	m40bUnit2   = "Roll"
	m40bRate2   = "350.0000"
	m40bValue2  = "₹1,750.00"
	m40bAmount2 = "₹2,065.00"
)

// m40bCell is one display cell as the test expects to find it.
type m40bCell struct{ label, text, sub, kind string }

// m40bCells are line no's five display cells.
func m40bCells(no int) []m40bCell {
	if no == 1 {
		return []m40bCell{
			{"Item", m40bItem1, m40bSub1, ""},
			{"Quantity", m40bQty1, "", "qty"},
			{"Unit", m40bUnit1, "", ""},
			{"Rate", m40bRate1, "", "rate"},
			{"Value", m40bValue1, "", "money"},
		}
	}
	return []m40bCell{
		{"Item", m40bItem2, m40bSub2, ""},
		{"Quantity", m40bQty2, "", "qty"},
		{"Unit", m40bUnit2, "", ""},
		{"Rate", m40bRate2, "", "rate"},
		{"Value", m40bValue2, "", "money"},
	}
}

func m40bLineURL(no int) string { return fmt.Sprintf("/doc/x/1/lines/%d", no) }

// m40bDisplayRows are two display rows. With actions, line 1 has Change,
// Remove and Move down, and line 2 Change, Remove and Move up; line 1 has
// a note.
func m40bDisplayRows(actions bool) []components.LineGridRow {
	var rows []components.LineGridRow
	for no, amount := range map[int]string{1: m40bAmount1, 2: m40bAmount2} {
		r := components.LineGridRow{No: no, Amount: amount}
		for _, c := range m40bCells(no) {
			r.Cells = append(r.Cells, components.GridCell{Label: c.label, Text: c.text, Sub: c.sub, Kind: c.kind})
		}
		if actions {
			r.ChangeURL, r.RemoveURL, r.Version = m40bLineURL(no), m40bLineURL(no)+"/remove", "4"
			if no == 1 {
				r.MoveDownURL = m40bLineURL(1) + "/move?dir=down"
			} else {
				r.MoveUpURL = m40bLineURL(2) + "/move?dir=up"
			}
		}
		if no == 1 {
			r.Note = m40bNote1
		}
		rows = append(rows, r)
	}
	slices.SortFunc(rows, func(a, b components.LineGridRow) int { return a.No - b.No })
	return rows
}

// m40bDisplayProps is a display list of the two rows, with their cards.
func m40bDisplayProps(actions bool) components.LineListProps {
	cards := []components.LineCard{
		{No: 1, Item: m40bItem1, Code: m40bSub1, QtyRate: m40bQty1 + " kg × " + m40bRate1, Amount: m40bAmount1, Tax: "GST 18%", Note: m40bNote1},
		{No: 2, Item: m40bItem2, Code: m40bSub2, QtyRate: m40bQty2 + " Roll × " + m40bRate2, Amount: m40bAmount2, Tax: "GST 18%"},
	}
	return components.LineListProps{Cards: cards, Grid: m40bDisplayRows(actions), Total: 2, Display: true, TaxableTotal: m40bAmount1}
}

// m40bRender renders c and parses it; a panic (such as a nil Input
// rendered as a component) fails the test instead of the package.
func m40bRender(t *testing.T, p components.LineListProps) (raw string, doc *goquery.Document) {
	t.Helper()
	var b bytes.Buffer
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("LineList panicked rendering %d grid rows: %v", len(p.Grid), r)
			}
		}()
		if err := components.LineList(p).Render(context.Background(), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
	}()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatalf("parse HTML: %v", err)
	}
	return b.String(), doc
}

// m40bNorm collapses runs of whitespace.
func m40bNorm(s string) string { return strings.Join(strings.Fields(s), " ") }

// m40bOwn is the deepest elements in sel whose whole text is text.
func m40bOwn(sel *goquery.Selection, text string) *goquery.Selection {
	return sel.Find("*").FilterFunction(func(_ int, s *goquery.Selection) bool {
		if m40bNorm(s.Text()) != text {
			return false
		}
		return s.Children().FilterFunction(func(_ int, c *goquery.Selection) bool { return m40bNorm(c.Text()) == text }).Length() == 0
	})
}

// m40bPos is n's index in document order.
func m40bPos(doc *goquery.Document, n *goquery.Selection) int {
	return slices.Index(doc.Find("*").Nodes, n.Get(0))
}

// m40bRows are the grid's rows inside the line editor.
func m40bRows(doc *goquery.Document) *goquery.Selection {
	return doc.Find(".line-editor .linegrid .linegrid-row")
}

// HOOK-058 (M0-40b, CD-313): a display row renders each cell's Text, Sub in
// its own element after Text in the same cell, data-kind and num on every
// number cell (and on no other), # and Amount from the row, and the note
// once, after the row's cells and before the next row. The cards still
// render and the list is a grid list.
func TestLineGrid_DisplayCells(t *testing.T) {
	_, doc := m40bRender(t, m40bDisplayProps(true))
	if doc.Find(".linelist.linelist--grid").Length() != 1 {
		t.Error("the display list is not a grid list (.linelist.linelist--grid): the cards would show from 64rem too")
	}
	if n := doc.Find(".linelist .lines .lines-card").Length(); n != 2 {
		t.Errorf("%d line cards, want 2: cards still show below 64rem (CD-313)", n)
	}
	rows := m40bRows(doc)
	if rows.Length() != 2 {
		t.Fatalf("%d .linegrid-row in .line-editor .linegrid, want 2", rows.Length())
	}
	var head []string
	doc.Find(".line-editor .linegrid-head").Children().Each(func(_ int, s *goquery.Selection) { head = append(head, m40bNorm(s.Text())) })
	if want := []string{"#", "Item", "Quantity", "Unit", "Rate", "Value", "Amount"}; len(head) < len(want) || !slices.Equal(head[:len(want)], want) {
		t.Errorf("the head reads %q, want it to start %q", head, want)
	}
	for i := range 2 {
		no, row := i+1, rows.Eq(i)
		what := fmt.Sprintf("line %d", no)
		texts := []string{fmt.Sprint(no)}
		for _, c := range m40bCells(no) {
			texts = append(texts, c.text)
			el := m40bOwn(row, c.text)
			if el.Length() != 1 {
				t.Errorf("%s: %d elements hold exactly %q (%s), want 1", what, el.Length(), c.text, c.label)
				continue
			}
			// The nearest marker and num class around Text, inside the row.
			kind, num := el.Closest("[data-kind]"), el.Closest(".num")
			if kind.Length() > 0 && !row.Contains(kind.Get(0)) {
				kind = kind.Slice(0, 0)
			}
			inNum := num.Length() > 0 && row.Contains(num.Get(0))
			switch {
			case c.kind != "" && kind.Length() == 0:
				t.Errorf("%s: %s %q has no data-kind marker, want data-kind=%q", what, c.label, c.text, c.kind)
			case c.kind != "" && kind.AttrOr("data-kind", "") != c.kind:
				t.Errorf("%s: %s %q has data-kind=%q, want %q", what, c.label, c.text, kind.AttrOr("data-kind", ""), c.kind)
			case c.kind != "" && !kind.HasClass("num"):
				t.Errorf("%s: %s's data-kind element has class %q, want num (right-aligned tabular numerals)", what, c.label, kind.AttrOr("class", ""))
			case c.kind == "" && kind.AttrOr("data-kind", "") != "":
				t.Errorf("%s: text cell %s sits in data-kind=%q, want no number marker", what, c.label, kind.AttrOr("data-kind", ""))
			case c.kind == "" && inNum:
				t.Errorf("%s: text cell %s is set as a number (num)", what, c.label)
			}
			if c.sub == "" {
				continue
			}
			sub := m40bOwn(row, c.sub)
			switch {
			case sub.Length() != 1:
				t.Errorf("%s: %d elements hold exactly Sub %q, want 1", what, sub.Length(), c.sub)
			case sub.Get(0) == el.Get(0) || sub.Contains(el.Get(0)) || el.Contains(sub.Get(0)):
				t.Errorf("%s: Sub %q and Text %q are not separate elements: Sub is its own second line", what, c.sub, c.text)
			case m40bPos(doc, sub) < m40bPos(doc, el):
				t.Errorf("%s: Sub %q comes before Text %q, want it under Text", what, c.sub, c.text)
			default:
				// The smallest element holding both is the cell: it holds no other cell's text.
				cell := el.Parent()
				for cell.Length() > 0 && !cell.Contains(sub.Get(0)) {
					cell = cell.Parent()
				}
				for _, other := range m40bCells(no)[1:] {
					if strings.Contains(m40bNorm(cell.Text()), other.text) {
						t.Errorf("%s: Text %q and Sub %q share an element with %s %q: Sub must sit in the Item cell", what, c.text, c.sub, other.label, other.text)
					}
				}
			}
		}
		texts = append(texts, []string{m40bAmount1, m40bAmount2}[i])
		if got := m40bNorm(row.Text()); !m40bInOrder(got, texts) {
			t.Errorf("%s reads %q, want # then the cells then Amount in order: %q", what, got, texts)
		}
	}
	// The note shows once, after line 1's amount and before line 2.
	notes := m40bOwn(doc.Find(".line-editor"), m40bNote1)
	if notes.Length() != 1 {
		t.Fatalf("the note shows %d times in the line editor, want once", notes.Length())
	}
	amount1, item2 := m40bOwn(rows.Eq(0), m40bAmount1), m40bOwn(rows.Eq(1), m40bItem2)
	if amount1.Length() == 1 && item2.Length() == 1 && (m40bPos(doc, notes) < m40bPos(doc, amount1) || m40bPos(doc, notes) > m40bPos(doc, item2)) {
		t.Error("line 1's note is not between line 1's amount and line 2: the note is line 1's second row")
	}
}

// m40bInOrder reports whether each of parts appears in s after the one before.
func m40bInOrder(s string, parts []string) bool {
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			return false
		}
		at += i + len(p)
	}
	return true
}

// HOOK-058 (M0-40b, CD-313): a display row is read-only: no row form, no
// htmx post or trigger, no input but the move forms' hidden csrf token and
// version, no select or textarea, and every form in the editor is a move
// form of a row.
func TestLineGrid_DisplayNoForm(t *testing.T) {
	for _, actions := range []bool{true, false} {
		_, doc := m40bRender(t, m40bDisplayProps(actions))
		ed := doc.Find(".line-editor")
		what := fmt.Sprintf("actions %v", actions)
		if ed.Length() != 1 || m40bRows(doc).Length() != 2 {
			t.Fatalf("%s: %d line editors with %d rows, want one with 2", what, ed.Length(), m40bRows(doc).Length())
		}
		for sel, why := range map[string]string{
			"form.linegrid-form":               "a row form",
			"[hx-post]":                        "an htmx post",
			"[hx-trigger]":                     "an htmx trigger",
			"select, textarea":                 "a select or textarea",
			`input:not([type="hidden"])`:       "a visible input",
			"[id^='grid-1-'], [id^='grid-2-']": "an editable cell id",
		} {
			if n := ed.Find(sel).Length(); n != 0 {
				t.Errorf("%s: the display grid has %d %s (%s), want none", what, n, sel, why)
			}
		}
		moves := []string{m40bLineURL(1) + "/move?dir=down", m40bLineURL(2) + "/move?dir=up"}
		ed.Find("form").Each(func(_ int, f *goquery.Selection) {
			if a := f.AttrOr("action", ""); !actions || !slices.Contains(moves, a) {
				t.Errorf("%s: the display grid has a form posting to %q, want only the rows' move forms", what, a)
			}
		})
		ed.Find("input").Each(func(_ int, in *goquery.Selection) {
			if n := in.AttrOr("name", ""); n != "csrf_token" && n != "version" {
				t.Errorf("%s: the display grid has an input named %q, want only a move form's csrf_token and version", what, n)
			}
		})
	}
}

// HOOK-058 (M0-40b, CD-313): a display row's actions are Change and Remove,
// links to its ChangeURL and RemoveURL, and Move up or Move down, forms
// posting its version to the move URL, each named "<verb> line <no>"
// (lineAria), inside the line editor; a URL left empty hides its action, so
// rows with none have no actions.
func TestLineGrid_DisplayActions(t *testing.T) {
	_, doc := m40bRender(t, m40bDisplayProps(true))
	ed := doc.Find(".line-editor")
	named := func(name string) *goquery.Selection {
		return ed.Find("a, button").FilterFunction(func(_ int, s *goquery.Selection) bool { return s.AttrOr("aria-label", "") == name })
	}
	for no := 1; no <= 2; no++ {
		for verb, href := range map[string]string{"Change": m40bLineURL(no), "Remove": m40bLineURL(no) + "/remove"} {
			name := fmt.Sprintf("%s line %d", verb, no)
			a := named(name)
			switch {
			case a.Length() != 1:
				t.Errorf("%d controls named %q in the display grid, want 1", a.Length(), name)
			case goquery.NodeName(a) != "a" || a.AttrOr("href", "") != href:
				t.Errorf("%q is <%s href=%q>, want a link to %q", name, goquery.NodeName(a), a.AttrOr("href", ""), href)
			case m40bNorm(a.Text()) != verb:
				t.Errorf("%q reads %q, want %q", name, m40bNorm(a.Text()), verb)
			}
		}
	}
	for name, action := range map[string]string{
		"Move down line 1": m40bLineURL(1) + "/move?dir=down",
		"Move up line 2":   m40bLineURL(2) + "/move?dir=up",
	} {
		b := named(name)
		if b.Length() != 1 {
			t.Errorf("%d controls named %q in the display grid, want 1", b.Length(), name)
			continue
		}
		f := b.Closest("form")
		if f.AttrOr("action", "") != action || f.AttrOr("method", "") != "post" {
			t.Errorf("%q is in a form posting to %q (%q), want POST %q", name, f.AttrOr("action", ""), f.AttrOr("method", ""), action)
		}
		if v := f.Find(`input[type="hidden"][name="version"]`); v.AttrOr("value", "") != "4" {
			t.Errorf("%q's form posts version %q, want the row's 4 (CD-94)", name, v.AttrOr("value", ""))
		}
	}
	for _, name := range []string{"Move up line 1", "Move down line 2"} {
		if n := named(name).Length(); n != 0 {
			t.Errorf("%d controls named %q: an empty move URL hides it", n, name)
		}
	}
	_, bare := m40bRender(t, m40bDisplayProps(false))
	bare.Find(".line-editor a, .line-editor button").Each(func(_ int, s *goquery.Selection) {
		t.Errorf("a display row with no URLs has the control %q (%q), want no actions", m40bNorm(s.Text()), s.AttrOr("aria-label", ""))
	})
}

// m40bGolden is LineList with Display false, an editable 3-cell grid with a
// note and its cards, as rendered at 0961f4a8 before M0-40b: Display false
// stays byte-identical (CD-313).
const m40bGolden = `<div class="linelist linelist--grid"><ol class="lines"><li class="lines-card"><div class="lines-top"><span class="lines-no">1</span> <span class="lines-name"><span>OPC Cement 53 Grade</span> <span class="code">CEMENT-OPC</span></span> <span class="num lines-amount">₹1,000.00</span></div><p class="lines-meta"><span class="num">10 kg × 350.0000</span> <span></span></p><div class="lines-actions"><a href="/doc/x/1/lines/1" class="btn tap btn-ghost btn--ghost" aria-label="Change line 1"><span>Change</span></a><a href="/doc/x/1/lines/1/remove" class="btn tap btn-ghost btn--ghost" aria-label="Remove line 1"><span>Remove</span></a><form method="post" action="/doc/x/1/lines/1/move?dir=down"><input type="hidden" name="csrf_token" value=""><input type="hidden" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move down line 1"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"></path><path d="m19 12-7 7-7-7"></path></svg><span>Move down</span></button></form></div></li><li class="lines-card"><div class="lines-top"><span class="lines-no">2</span> <span class="lines-name"><span>Binding wire 18 gauge</span> <span class="code">WIRE-18G</span></span> <span class="num lines-amount">₹1,000.00</span></div><p class="lines-meta"><span class="num">5 kg × 350.0000</span> <span></span></p><p class="lines-meta">Last bought at ₹345.0000 from Shree Steel Traders on 12-10-2026</p><div class="lines-actions"><a href="/doc/x/1/lines/2" class="btn tap btn-ghost btn--ghost" aria-label="Change line 2"><span>Change</span></a><a href="/doc/x/1/lines/2/remove" class="btn tap btn-ghost btn--ghost" aria-label="Remove line 2"><span>Remove</span></a><form method="post" action="/doc/x/1/lines/2/move?dir=up"><input type="hidden" name="csrf_token" value=""><input type="hidden" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move up line 2"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"></path><path d="M12 19V5"></path></svg><span>Move up</span></button></form></div></li></ol><p class="lines-total"><span>Taxable total</span> <span class="num">₹2,000.00</span></p><a href="/doc/x/1/lines/new" class="btn tap btn-outline btn--secondary" data-add-line><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 12h14"></path><path d="M12 5v14"></path></svg><span>Add an item</span></a><div id="line-editor" class="line-editor"><div class="linegrid linegrid--3" x-data="linegrid"><div class="linegrid-head" aria-hidden="true"><span>#</span> <span>Item</span> <span>Quantity</span> <span>Rate</span> <span class="num">Amount</span> <span>Actions</span></div><div class="linegrid-row"><form id="grid-row-1" class="linegrid-form" hx-post="/doc/x/1/lines/1" hx-trigger="change" hx-target="this" hx-swap="outerHTML" hx-include="#doc-version" hx-sync="#line-editor:queue all"><span class="linegrid-no"><span class="visually-hidden">Line</span>1</span> <div class="fieldset field"><label for="grid-1-item" class="fieldset-legend field__label">Item </label><input type="text" value="OPC Cement 53 Grade" aria-describedby="grid-1-item-msg" class="input field__input" id="grid-1-item" name="item"><p id="grid-1-item-msg" class="field__msg"></p></div><div class="fieldset field"><label for="grid-1-qty" class="fieldset-legend field__label">Quantity </label><div class="join field__control"><input type="text" inputmode="decimal" autocomplete="off" value="10" hx-post="/ui/validate/qty" hx-swap="outerHTML" hx-target="#grid-1-qty-msg" hx-trigger="change" hx-vals="{&#34;_id&#34;:&#34;grid-1-qty&#34;,&#34;_name&#34;:&#34;qty&#34;}" aria-describedby="grid-1-qty-msg" class="input join-item field__input num" id="grid-1-qty" name="qty"> <span class="join-item field__suffix">kg</span></div><p id="grid-1-qty-msg" class="field__msg"></p></div><div class="fieldset field"><label for="grid-1-rate" class="fieldset-legend field__label">Rate </label><div class="join field__control"><input type="text" inputmode="decimal" autocomplete="off" value="350.0000" hx-post="/ui/validate/rate" hx-swap="outerHTML" hx-target="#grid-1-rate-msg" hx-trigger="change" hx-vals="{&#34;_id&#34;:&#34;grid-1-rate&#34;,&#34;_name&#34;:&#34;rate&#34;}" aria-describedby="grid-1-rate-msg" class="input join-item field__input num" id="grid-1-rate" name="rate"> </div><p id="grid-1-rate-msg" class="field__msg"></p></div><span class="num linegrid-amount"><span class="visually-hidden">Amount</span>₹1,000.00</span></form><div class="linegrid-actions"><a href="/doc/x/1/lines/1/remove" class="btn tap btn-ghost btn--ghost" aria-label="Remove line 1"><span>Remove</span></a><form method="post" action="/doc/x/1/lines/1/move?dir=down"><input type="hidden" name="csrf_token" value=""><input type="hidden" id="grid-1-move-down-version" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move down line 1"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 5v14"></path><path d="m19 12-7 7-7-7"></path></svg><span>Move down</span></button></form></div></div><div class="linegrid-row"><form id="grid-row-2" class="linegrid-form" hx-post="/doc/x/1/lines/2" hx-trigger="change" hx-target="this" hx-swap="outerHTML" hx-include="#doc-version" hx-sync="#line-editor:queue all"><span class="linegrid-no"><span class="visually-hidden">Line</span>2</span> <div class="fieldset field"><label for="grid-2-item" class="fieldset-legend field__label">Item </label><input type="text" value="Binding wire 18 gauge" aria-describedby="grid-2-item-msg" class="input field__input" id="grid-2-item" name="item"><p id="grid-2-item-msg" class="field__msg"></p></div><div class="fieldset field"><label for="grid-2-qty" class="fieldset-legend field__label">Quantity </label><div class="join field__control"><input type="text" inputmode="decimal" autocomplete="off" value="5" hx-post="/ui/validate/qty" hx-swap="outerHTML" hx-target="#grid-2-qty-msg" hx-trigger="change" hx-vals="{&#34;_id&#34;:&#34;grid-2-qty&#34;,&#34;_name&#34;:&#34;qty&#34;}" aria-describedby="grid-2-qty-msg" class="input join-item field__input num" id="grid-2-qty" name="qty"> <span class="join-item field__suffix">kg</span></div><p id="grid-2-qty-msg" class="field__msg"></p></div><div class="fieldset field"><label for="grid-2-rate" class="fieldset-legend field__label">Rate </label><div class="join field__control"><input type="text" inputmode="decimal" autocomplete="off" value="350.0000" hx-post="/ui/validate/rate" hx-swap="outerHTML" hx-target="#grid-2-rate-msg" hx-trigger="change" hx-vals="{&#34;_id&#34;:&#34;grid-2-rate&#34;,&#34;_name&#34;:&#34;rate&#34;}" aria-describedby="grid-2-rate-msg" class="input join-item field__input num" id="grid-2-rate" name="rate"> </div><p id="grid-2-rate-msg" class="field__msg"></p></div><span class="num linegrid-amount"><span class="visually-hidden">Amount</span>₹1,000.00</span></form><div class="linegrid-actions"><a href="/doc/x/1/lines/2/remove" class="btn tap btn-ghost btn--ghost" aria-label="Remove line 2"><span>Remove</span></a><form method="post" action="/doc/x/1/lines/2/move?dir=up"><input type="hidden" name="csrf_token" value=""><input type="hidden" id="grid-2-move-up-version" name="version" value="4"><button type="submit" class="btn tap btn-ghost btn--ghost" data-busy-label="Saving…" aria-label="Move up line 2"><svg class="icon" aria-hidden="true" focusable="false" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 7-7 7 7"></path><path d="M12 19V5"></path></svg><span>Move up</span></button></form><p>Last bought at ₹345.0000 from Shree Steel Traders on 12-10-2026</p></div></div><p id="grid-total" class="linegrid-total"><span>Taxable total</span> <span class="num">₹2,000.00</span></p></div></div></div>`

// m40bEditableProps are the golden's props: today's fields only.
func m40bEditableProps() components.LineListProps {
	row := func(no int, item, qty, note string) components.LineGridRow {
		id := fmt.Sprintf("grid-%d-", no)
		r := components.LineGridRow{
			No: no, Amount: "₹1,000.00", PostURL: m40bLineURL(no), RemoveURL: m40bLineURL(no) + "/remove", Version: "4", Note: note,
			Cells: []components.GridCell{
				{Label: "Item", Input: components.TextInput(components.FieldProps{ID: id + "item", Name: "item", Label: "Item", Value: item})},
				{Label: "Quantity", Input: components.QtyInput(components.FieldProps{ID: id + "qty", Name: "qty", Label: "Quantity", Value: qty}, false, "kg")},
				{Label: "Rate", Input: components.RateInput(components.FieldProps{ID: id + "rate", Name: "rate", Label: "Rate", Value: m40bRate2})},
			},
		}
		if no == 1 {
			r.MoveDownURL = m40bLineURL(1) + "/move?dir=down"
		} else {
			r.MoveUpURL = m40bLineURL(2) + "/move?dir=up"
		}
		return r
	}
	return components.LineListProps{
		Cards: []components.LineCard{
			{No: 1, Item: m40bItem1, Code: "CEMENT-OPC", QtyRate: "10 kg × " + m40bRate2, Amount: "₹1,000.00", ChangeURL: m40bLineURL(1), RemoveURL: m40bLineURL(1) + "/remove", MoveDownURL: m40bLineURL(1) + "/move?dir=down", Version: "4"},
			{No: 2, Item: m40bItem2, Code: m40bSub2, QtyRate: "5 kg × " + m40bRate2, Amount: "₹1,000.00", ChangeURL: m40bLineURL(2), RemoveURL: m40bLineURL(2) + "/remove", MoveUpURL: m40bLineURL(2) + "/move?dir=up", Version: "4", Note: m40bNote1},
		},
		Grid:         []components.LineGridRow{row(1, m40bItem1, "10", ""), row(2, m40bItem2, "5", m40bNote1)},
		TaxableTotal: "₹2,000.00", AddURL: "/doc/x/1/lines/new", Total: 2, Editable: true,
	}
}

// HOOK-059 (M0-40b, CD-313): Display false renders an editable grid list
// exactly as before M0-40b.
func TestLineGrid_EditableUnchanged(t *testing.T) {
	raw, _ := m40bRender(t, m40bEditableProps())
	if raw != m40bGolden {
		t.Errorf("LineList with Display false changed:\n got %s\nwant %s", raw, m40bGolden)
	}
}
