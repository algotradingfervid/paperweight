package components_test

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// rowsProps is an editor with rows 1..n, each with a required-looking text
// cell and a rate cell; row errs[no] shows that error on its factor field.
func rowsProps(n int, errs map[int]string) components.RowsProps {
	p := components.RowsProps{ID: "conversions", Legend: "Unit conversions", AddLabel: "Add conversion"}
	for no := 1; no <= n; no++ {
		name := func(f string) string { return fmt.Sprintf("lines[%d].%s", no, f) }
		id := func(f string) string { return fmt.Sprintf("grid-%d-%s", no, f) }
		p.Rows = append(p.Rows, components.RowsRow{No: no, Cells: []components.GridCell{
			{Label: "Unit", Input: components.TextInput(components.FieldProps{ID: id("unit"), Name: name("unit"), Label: "Unit", Value: "box"})},
			{Label: "Factor", Input: components.RateInput(components.FieldProps{ID: id("factor"), Name: name("factor"), Label: "Factor", Value: "12", Error: errs[no]})},
		}})
	}
	return p
}

// rowOf is the row element holding control e.
func rowOf(e *goquery.Selection) *goquery.Selection { return e.Closest(".rows-row") }

// visibleButtons are the buttons a person can see and reach.
func visibleButtons(doc *goquery.Document) *goquery.Selection {
	return doc.Find("button").FilterFunction(func(_ int, b *goquery.Selection) bool {
		return b.AttrOr("aria-hidden", "") != "true"
	})
}

// inForm renders the editor and a Save button in components.Form, as a
// master screen does.
func inForm(t *testing.T, f components.FormProps, p components.RowsProps) *goquery.Document {
	t.Helper()
	body := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if err := components.RowsEditor(p).Render(ctx, w); err != nil {
			return err
		}
		return components.Button(components.ButtonProps{Label: "Save", Variant: components.Primary}).Render(ctx, w)
	})
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-1"})
	return uitest.RenderCtx(t, templ.WithChildren(ctx, body), components.Form(f))
}

func TestRowsEditor_FieldsetLegendAndCells(t *testing.T) {
	doc := uitest.Render(t, components.RowsEditor(rowsProps(2, map[int]string{2: "Enter the factor."})))

	fs := doc.Find("fieldset#conversions")
	if fs.Length() != 1 {
		t.Fatalf("want one fieldset#conversions, got %d", fs.Length())
	}
	if got := text(fs.ChildrenFiltered("legend")); got != "Unit conversions" {
		t.Errorf("legend = %q", got)
	}
	if n := doc.Find("form").Length(); n != 0 {
		t.Errorf("the editor renders %d forms, want none: it sits inside the caller's", n)
	}
	if n := doc.Find("table").Length(); n != 0 {
		t.Errorf("%d <table> elements", n)
	}

	rows := fs.Find(".rows-row")
	if rows.Length() != 2 {
		t.Fatalf("want 2 rows, got %d", rows.Length())
	}
	for no := 1; no <= 2; no++ {
		row := rows.Eq(no - 1)
		for _, f := range []string{"unit", "factor"} {
			in := row.Find(fmt.Sprintf("input#grid-%d-%s", no, f))
			if in.Length() != 1 {
				t.Fatalf("row %d: no input #grid-%d-%s", no, no, f)
			}
			if got, want := in.AttrOr("name", ""), fmt.Sprintf("lines[%d].%s", no, f); got != want {
				t.Errorf("row %d: name = %q, want %q", no, got, want)
			}
		}
		if got := plain(row.Find(".rows-no")); got != fmt.Sprint(no) {
			t.Errorf("row %d: number = %q", no, got)
		}
		if got := text(row.Find(".rows-no .visually-hidden")); got != "Row" {
			t.Errorf("row %d: hidden word = %q, want Row", no, got)
		}
	}
	if got := text(rows.Eq(1).Find("#grid-2-factor-msg")); got != "Enter the factor." {
		t.Errorf("row 2 field error = %q", got)
	}
	if got := text(rows.Eq(0).Find("#grid-1-factor-msg")); got != "" {
		t.Errorf("row 1 shows a field error %q", got)
	}
}

func TestRowsEditor_HeadNamesEachCell(t *testing.T) {
	doc := uitest.Render(t, components.RowsEditor(rowsProps(2, nil)))
	head := doc.Find(".rows-head")
	if head.Length() != 1 {
		t.Fatalf("want one .rows-head, got %d", head.Length())
	}
	if got := head.AttrOr("aria-hidden", ""); got != "true" {
		t.Errorf("head aria-hidden = %q: each control keeps its own label", got)
	}
	var labels []string
	head.Children().Each(func(_ int, s *goquery.Selection) { labels = append(labels, text(s)) })
	if got, want := strings.Join(labels, "|"), "#|Unit|Factor|Actions"; got != want {
		t.Errorf("head tracks = %q, want %q", got, want)
	}
	if !before(doc, head, doc.Find(".rows-row").First()) {
		t.Error("head is not before the first row")
	}
}

// CD-203: a cell declares the wide number track, and the head's track for
// that column gets it too, so the two stay aligned.
func TestRowsEditor_WideCellMarksHeadAndRow(t *testing.T) {
	p := rowsProps(2, nil)
	for i := range p.Rows {
		p.Rows[i].Cells[1].Wide = true
	}
	doc := uitest.Render(t, components.RowsEditor(p))
	for _, c := range []struct {
		what string
		cell *goquery.Selection
		want bool
	}{
		{"head Unit", doc.Find(".rows-head > .rows-cell").Eq(0), false},
		{"head Factor", doc.Find(".rows-head > .rows-cell").Eq(1), true},
		{"row 1 Unit", doc.Find("#grid-1-unit").Closest(".rows-cell"), false},
		{"row 1 Factor", doc.Find("#grid-1-factor").Closest(".rows-cell"), true},
		{"row 2 Factor", doc.Find("#grid-2-factor").Closest(".rows-cell"), true},
	} {
		if c.cell.Length() != 1 {
			t.Errorf("%s: no .rows-cell", c.what)
			continue
		}
		if got := c.cell.HasClass("rows-cell--wide"); got != c.want {
			t.Errorf("%s: wide = %v, want %v", c.what, got, c.want)
		}
	}
}

func TestRowsEditor_AddAndRemoveAreSubmitters(t *testing.T) {
	doc := uitest.Render(t, components.RowsEditor(rowsProps(3, nil)))

	check := func(b *goquery.Selection, what, value, busy string) {
		t.Helper()
		if b.Length() != 1 {
			t.Fatalf("%s: want one button, got %d", what, b.Length())
		}
		for attr, want := range map[string]string{"type": "submit", "name": "_rows", "value": value, "data-busy-label": busy} {
			if got := b.AttrOr(attr, ""); got != want {
				t.Errorf("%s: %s = %q, want %q", what, attr, got, want)
			}
		}
		if _, ok := b.Attr("formnovalidate"); !ok {
			t.Errorf("%s: no formnovalidate, so an empty required field would block it", what)
		}
		if !b.HasClass("btn") || !b.HasClass("tap") {
			t.Errorf("%s: class = %q, want components.Button's", what, b.AttrOr("class", ""))
		}
	}

	add := controlsNamed(doc, "Add conversion")
	check(add, "Add", "add", "Adding…")
	if rowOf(add).Length() != 0 {
		t.Error("Add sits inside a row")
	}
	if !before(doc, doc.Find(".rows-row").Last(), add) {
		t.Error("Add is not after the rows")
	}

	for no := 1; no <= 3; no++ {
		rm := controlsNamed(doc, fmt.Sprintf("Remove row %d", no))
		check(rm, fmt.Sprintf("Remove %d", no), fmt.Sprintf("remove:%d", no), "Removing…")
		if got := text(rm); got != "Remove" {
			t.Errorf("Remove %d: visible text = %q", no, got)
		}
		if !holds(rowOf(rm), doc.Find(fmt.Sprintf("#grid-%d-unit", no))) {
			t.Errorf("Remove %d is not in its row", no)
		}
	}
}

// In a form the caller owns, with its Save after the editor, Enter in a text
// field clicks the form's first submit button. That one must not be a row
// button, or Enter would remove row 1 past the field validation.
func TestRowsEditor_EnterInAFieldDoesNotRemove(t *testing.T) {
	for _, c := range []struct {
		name string
		rows int
		hx   bool
	}{
		{"plain form", 2, false},
		{"htmx form", 2, true},
		{"plain form, no rows", 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := inForm(t, components.FormProps{ID: "f", Action: "/masters/item/1", HX: c.hx}, rowsProps(c.rows, nil))
			first := doc.Find("form#f button[type=submit]").First()
			if name, ok := first.Attr("name"); ok {
				t.Errorf("first submit button posts %s=%q, want it to save the form", name, first.AttrOr("value", ""))
			}
			if _, ok := first.Attr("formnovalidate"); ok {
				t.Error("first submit button skips the field validation")
			}
			if got := first.AttrOr("aria-hidden", ""); got != "true" {
				t.Errorf("first submit button aria-hidden = %q, want it out of the accessibility tree", got)
			}
			if got := first.AttrOr("tabindex", ""); got != "-1" {
				t.Errorf("first submit button tabindex = %q, want it out of the tab order", got)
			}
			if first.Closest(".visually-hidden").Length() != 1 {
				t.Error("first submit button is not visually hidden")
			}
			if !before(doc, first, doc.Find("button[name=_rows]").First()) {
				t.Error("a row button comes before the Enter button")
			}
		})
	}
}

// A caller's form keeps every submitter's own name and value: the busy state
// and hx-disabled-elt act on the rendered buttons, which must stay enabled
// and inside the form they post. An htmx form passes the DisabledElt the
// editor's doc comment asks for, because Form's default "find" reaches only
// the hidden first submit; that selector covers every submit in the form.
func TestRowsEditor_InARealFormKeepsSubmitters(t *testing.T) {
	doc := inForm(t, components.FormProps{ID: "f", Action: "/masters/item/1", HX: true, DisabledElt: "#f button[type=submit]"}, rowsProps(2, nil))
	if n := doc.Find("form").Length(); n != 1 {
		t.Fatalf("want one form, got %d", n)
	}
	f := doc.Find("form#f")
	disabled := f.AttrOr("hx-disabled-elt", "")
	if strings.HasPrefix(disabled, "find ") {
		t.Fatalf("hx-disabled-elt = %q reaches only the hidden first submit", disabled)
	}
	// The hidden Save, Remove twice, Add and the form's Save.
	if n := doc.Find(disabled).Length(); n != 5 {
		t.Errorf("hx-disabled-elt %q matches %d buttons, want all 5 submits", disabled, n)
	}
	if got := f.Find(`input[name="csrf_token"]`).AttrOr("value", ""); got != "tok-1" {
		t.Errorf("csrf_token = %q", got)
	}
	for _, value := range []string{"add", "remove:1", "remove:2"} {
		b := f.Find(fmt.Sprintf(`button[type=submit][name="_rows"][value=%q]`, value))
		if b.Length() != 1 {
			t.Errorf("no submit button %s in the form", value)
			continue
		}
		if _, ok := b.Attr("disabled"); ok {
			t.Errorf("%s is rendered disabled", value)
		}
		if _, ok := b.Attr("form"); ok {
			t.Errorf("%s posts another form", value)
		}
	}
}

func TestRowsEditor_ErrorShown(t *testing.T) {
	p := rowsProps(1, nil)
	p.Error = "Add at least one conversion."
	doc := uitest.Render(t, components.RowsEditor(p))
	msg := doc.Find("fieldset#conversions #conversions-msg")
	if got := text(msg); got != p.Error {
		t.Errorf("message = %q", got)
	}
	if msg.Find("svg").Length() != 1 {
		t.Error("error has no icon")
	}
	if got := doc.Find("fieldset#conversions").AttrOr("aria-describedby", ""); got != "conversions-msg" {
		t.Errorf("aria-describedby = %q", got)
	}

	clean := uitest.Render(t, components.RowsEditor(rowsProps(1, nil)))
	if got := text(clean.Find("#conversions-msg")); got != "" {
		t.Errorf("message with no error = %q", got)
	}
}

func TestRowsEditor_ZeroRowsShowsAddOnly(t *testing.T) {
	doc := uitest.Render(t, components.RowsEditor(rowsProps(0, nil)))
	if n := doc.Find(".rows-row, .rows-head, .rows-items").Length(); n != 0 {
		t.Errorf("%d row parts with no rows", n)
	}
	vis := visibleButtons(doc)
	if vis.Length() != 1 || accName(vis) != "Add conversion" {
		t.Errorf("visible buttons = %d (%q), want only Add conversion", vis.Length(), accName(vis))
	}
}
