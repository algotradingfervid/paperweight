package components_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// accName is a control's accessible name: aria-label, else its text.
func accName(s *goquery.Selection) string {
	if l := s.AttrOr("aria-label", ""); l != "" {
		return l
	}
	return text(s)
}

// controlsNamed returns the links and buttons whose accessible name is name.
func controlsNamed(doc *goquery.Document, name string) *goquery.Selection {
	return doc.Find("a, button").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return accName(s) == name
	})
}

// ownerForm is the form a button submits: its form attribute, else the
// closest form ancestor.
func ownerForm(doc *goquery.Document, b *goquery.Selection) *goquery.Selection {
	if id, ok := b.Attr("form"); ok && id != "" {
		return doc.Find("form#" + id)
	}
	return b.Closest("form")
}

// inRow reports whether e sits in row no's part of the grid: after
// form#grid-row-<no> and before the next row form (or the grid total).
func inRow(doc *goquery.Document, e *goquery.Selection, no int) bool {
	row := doc.Find(fmt.Sprintf("form#grid-row-%d", no))
	if row.Length() != 1 || !holds(row, e) && !before(doc, row, e) {
		return false
	}
	next := doc.Find(fmt.Sprintf("form#grid-row-%d", no+1))
	if next.Length() == 0 {
		next = doc.Find("#grid-total")
	}
	return next.Length() == 1 && before(doc, e, next)
}

func TestLineGrid_ActionsColumn(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})
	rows := gridRows(3)
	rows[1].RemoveURL = ""
	doc := uitest.RenderCtx(t, ctx, components.LineGrid(rows, "₹2,25,000.00"))
	ed := doc.Find("#line-editor")
	if ed.Length() != 1 {
		t.Fatalf("want one #line-editor, got %d", ed.Length())
	}

	var head *goquery.Selection
	ed.Find(`[aria-hidden="true"]`).EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if s.Closest("form").Length() == 0 {
			head = s
			return false
		}
		return true
	})
	if head == nil {
		t.Fatal(`no aria-hidden="true" header outside the row forms`)
	}
	var labels []string
	head.Children().Each(func(_ int, s *goquery.Selection) { labels = append(labels, text(s)) })
	want := []string{"#", "Item", "Qty", "UoM", "Rate", "Tax", "Amount", "Actions"}
	if strings.Join(labels, "|") != strings.Join(want, "|") {
		t.Errorf("header tracks = %q, want %q", labels, want)
	}
	if !before(doc, head, doc.Find("form#grid-row-1")) {
		t.Error("header is not before the first row")
	}

	for _, r := range rows {
		rm := controlsNamed(doc, fmt.Sprintf("Remove line %d", r.No))
		if r.RemoveURL == "" {
			if rm.Length() != 0 {
				t.Errorf("row %d: Remove shown with no RemoveURL", r.No)
			}
		} else {
			if rm.Length() != 1 {
				t.Errorf("row %d: want one \"Remove line %d\" control, got %d", r.No, r.No, rm.Length())
			} else {
				if !rm.Is("a") || rm.AttrOr("href", "") != r.RemoveURL || !rm.HasClass("btn") {
					t.Errorf("row %d: Remove is <%s href=%q class=%q>, want a Button link to %s",
						r.No, goquery.NodeName(rm), rm.AttrOr("href", ""), rm.AttrOr("class", ""), r.RemoveURL)
				}
				if !inRow(doc, rm, r.No) {
					t.Errorf("row %d: Remove is not in its row", r.No)
				}
			}
		}

		for _, mv := range []struct{ word, url string }{{"Move up", r.MoveUpURL}, {"Move down", r.MoveDownURL}} {
			name := fmt.Sprintf("%s line %d", mv.word, r.No)
			b := controlsNamed(doc, name)
			if mv.url == "" {
				if b.Length() != 0 {
					t.Errorf("row %d: %q shown with no URL", r.No, name)
				}
				continue
			}
			if b.Length() != 1 {
				t.Errorf("row %d: want one %q control, got %d", r.No, name, b.Length())
				continue
			}
			if !b.Is("button") || !b.HasClass("btn") {
				t.Errorf("row %d: %q is <%s class=%q>, want a Button", r.No, name, goquery.NodeName(b), b.AttrOr("class", ""))
			}
			if !inRow(doc, b, r.No) {
				t.Errorf("row %d: %q is not in its row", r.No, name)
			}
			f := ownerForm(doc, b)
			if f.Length() != 1 || f.AttrOr("method", "") != "post" || f.AttrOr("action", "") != mv.url {
				t.Errorf("row %d: %q submits form method=%q action=%q, want POST to %s",
					r.No, name, f.AttrOr("method", ""), f.AttrOr("action", ""), mv.url)
				continue
			}
			if f.Find(`input[type="hidden"][name="csrf_token"]`).AttrOr("value", "") != "tok-9" {
				t.Errorf("row %d: %q form has no csrf_token", r.No, name)
			}
			if doc.Find(fmt.Sprintf(`a[href=%q]`, mv.url)).Length() != 0 {
				t.Errorf("row %d: %q is a GET link", r.No, name)
			}
		}
	}
}

func TestLineGrid_RendersCellInputs(t *testing.T) {
	rows := gridRows(2)
	doc := uitest.Render(t, components.LineGrid(rows, "₹1,50,000.00"))
	for _, r := range rows {
		f := doc.Find(fmt.Sprintf("form#grid-row-%d", r.No))
		if f.Length() != 1 {
			t.Fatalf("no form#grid-row-%d", r.No)
		}
		qty := f.Find(fmt.Sprintf("input#grid-%d-qty", r.No))
		if qty.Length() != 1 {
			t.Fatalf("row %d: no QtyInput #grid-%d-qty in the row form", r.No, r.No)
		}
		if got := qty.AttrOr("inputmode", ""); got != "decimal" {
			t.Errorf("row %d qty inputmode = %q, want \"decimal\"", r.No, got)
		}
		if got := qty.AttrOr("hx-post", ""); got != "/ui/validate/qty" {
			t.Errorf("row %d qty hx-post = %q, want /ui/validate/qty", r.No, got)
		}
		if got := qty.AttrOr("value", ""); got != "1,200" {
			t.Errorf("row %d qty value = %q, want 1,200", r.No, got)
		}
		if rate := f.Find(fmt.Sprintf("input#grid-%d-rate", r.No)); rate.AttrOr("hx-post", "") != "/ui/validate/rate" {
			t.Errorf("row %d: RateInput not rendered as given (hx-post %q)", r.No, rate.AttrOr("hx-post", ""))
		}
		for _, field := range []string{"item", "uom", "tax"} {
			if f.Find(fmt.Sprintf("input#grid-%d-%s", r.No, field)).Length() != 1 {
				t.Errorf("row %d: no input #grid-%d-%s", r.No, r.No, field)
			}
		}
	}
}

func TestLineGrid_HiddenLabelsForNumberAndAmount(t *testing.T) {
	rows := gridRows(3)
	rows[2].Amount = "₹99,99,99,999.99"
	doc := uitest.Render(t, components.LineGrid(rows, "₹1,00,00,49,999.99"))
	for _, r := range rows {
		f := doc.Find(fmt.Sprintf("form#grid-row-%d", r.No))
		if f.Length() != 1 {
			t.Fatalf("no form#grid-row-%d", r.No)
		}
		for label, value := range map[string]string{"Line": strconv.Itoa(r.No), "Amount": r.Amount} {
			hidden := f.Find(".visually-hidden").FilterFunction(func(_ int, s *goquery.Selection) bool {
				return text(s) == label
			})
			if hidden.Length() != 1 {
				t.Errorf("row %d: want one .visually-hidden %q label, got %d", r.No, label, hidden.Length())
				continue
			}
			if got := plain(hidden.Parent()); got != value {
				t.Errorf("row %d: %q label sits with visible text %q, want %q", r.No, label, got, value)
			}
		}
	}
}

func TestLineList_WithGridHasOneLineEditor(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})
	doc := uitest.RenderCtx(t, ctx, components.LineList(components.LineListProps{
		Cards: lineCards(3), Grid: gridRows(3), Total: 3, Editable: true,
		AddURL: "/doc/po/7/lines/new", TaxableTotal: "₹2,25,000.00",
	}))
	if n := doc.Find("#line-editor").Length(); n != 1 {
		t.Errorf("want exactly one #line-editor, got %d", n)
	}
	if n := doc.Find("ol.lines > li").Length(); n != 3 {
		t.Errorf("want 3 cards, got %d", n)
	}
	if n := doc.Find(`form[id^="grid-row-"]`).Length(); n != 3 {
		t.Errorf("want 3 grid rows, got %d", n)
	}
	seen := map[string]int{}
	doc.Find("[id]").Each(func(_ int, s *goquery.Selection) { seen[s.AttrOr("id", "")]++ })
	for id, n := range seen {
		if n > 1 {
			t.Errorf("id %q used %d times", id, n)
		}
	}
	wrap := doc.Find(".linelist.linelist--grid")
	if wrap.Length() != 1 {
		t.Fatalf("want one .linelist.linelist--grid (hides the cards from 64rem), got %d", wrap.Length())
	}
	if !holds(wrap, doc.Find("ol.lines")) {
		t.Error("cards are outside .linelist--grid")
	}
}

func TestDataList_ValidateRejectsBadPriority(t *testing.T) {
	col := func(k string, p components.Priority) components.Column {
		return components.Column{Key: k, Label: k, Priority: p}
	}
	titled := []components.Row{{Title: "Draft 42"}}
	untitledCols := func(meta int) []components.Column {
		cols := []components.Column{col("a", components.PFigure), col("s", components.PState)}
		for i := range meta {
			cols = append(cols, col(fmt.Sprintf("m%d", i), components.PMeta))
		}
		return cols
	}
	cases := []struct {
		name    string
		p       components.DataListProps
		wantErr bool
	}{
		{"priority 0", components.DataListProps{Columns: []components.Column{col("t", components.PTitle), col("z", components.Priority(0))}, Rows: titled}, true},
		{"priority -1", components.DataListProps{Columns: []components.Column{col("t", components.PTitle), col("z", components.Priority(-1))}, Rows: titled}, true},
		{"priority 6 is low, valid", components.DataListProps{Columns: []components.Column{col("t", components.PTitle), col("z", components.Priority(6))}, Rows: titled}, false},
		{"7 columns no title (8 tracks)", components.DataListProps{Columns: untitledCols(5), Rows: titled}, false},
		{"8 columns no title (9 tracks)", components.DataListProps{Columns: untitledCols(6), Rows: titled}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if c.wantErr && err == nil {
				t.Error("Validate() = nil, want an error")
			}
			if !c.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestDataList_NoStraySeparator(t *testing.T) {
	cols := []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle, Code: true},
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "state", Label: "State", Priority: components.PState},
		{Key: "by", Label: "Raised by", Priority: components.PLow},
		{Key: "site", Label: "Site", Priority: components.Priority(6)},
	}
	r := components.Row{
		Href: "/doc/po/42", Party: "Shree Steel Traders",
		Values: map[string]string{"no": "PO/26-27/0042", "amount": "₹94,164.00", "by": "Anita Rao", "site": "Hosur Road Unit 2"},
		State:  doctype.StateApproved,
	}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Purchase orders", Columns: cols, Rows: []components.Row{r}}))
	a := doc.Find(`ul.list > li a`)
	if a.Length() != 1 {
		t.Fatalf("want one card link, got %d", a.Length())
	}
	for _, v := range []string{"Anita Rao", "Hosur Road Unit 2"} {
		if leafWith(t, a, v).Closest(`[data-priority="low"]`).Length() == 0 {
			t.Errorf("%q is not marked data-priority=low", v)
		}
	}
	if a.Find("span.badge.badge--approved").Length() != 1 {
		t.Fatal("no approved badge in the card")
	}
	// Below 64rem every [data-priority="low"] element is hidden; what is
	// left must have no "·".
	narrow := a.Clone()
	narrow.Find(`[data-priority="low"], dt, .visually-hidden`).Remove()
	got := text(narrow)
	if !strings.Contains(got, "Approved") {
		t.Fatalf("visible card %q lost the state", got)
	}
	if strings.Contains(got, "·") {
		t.Errorf("visible card below 64rem = %q, has a stray separator", got)
	}
}
