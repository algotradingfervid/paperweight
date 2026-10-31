package components_test

import (
	"fmt"
	"testing"
)

// M0-40b review: a display cell names its column to a screen reader. The
// head is aria-hidden and the cards are hidden from 64rem, so each cell
// carries its Label as visually-hidden text before its Text, as the
// editable grid's hidden field labels do.
func TestLineGrid_DisplayCellLabels(t *testing.T) {
	_, doc := m40bRender(t, m40bDisplayProps(true))
	rows := m40bRows(doc)
	if rows.Length() != 2 {
		t.Fatalf("%d display rows, want 2", rows.Length())
	}
	for i := range 2 {
		no := i + 1
		for _, c := range m40bCells(no) {
			el := m40bOwn(rows.Eq(i), c.text)
			if el.Length() != 1 {
				t.Errorf("line %d: %d elements hold %q, want 1", no, el.Length(), c.text)
				continue
			}
			label := el.PrevAllFiltered(".visually-hidden")
			if label.Length() != 1 || m40bNorm(label.Text()) != c.label {
				t.Errorf("line %d: %s %q has %d visually-hidden labels before it reading %q, want one reading %q", no, c.label, c.text, label.Length(), m40bNorm(label.Text()), c.label)
			}
		}
	}
}

// M0-40b review: a display list's TaxableTotal shows in the grid, since
// the cards' total is hidden from 64rem; with none, the grid has no total.
func TestLineGrid_DisplayTotal(t *testing.T) {
	for _, total := range []string{m40bAmount1, ""} {
		p := m40bDisplayProps(false)
		p.TaxableTotal = total
		_, doc := m40bRender(t, p)
		got := doc.Find(".line-editor .linegrid-total")
		what := fmt.Sprintf("TaxableTotal %q", total)
		switch {
		case total == "" && got.Length() != 0:
			t.Errorf("%s: the grid shows a total %q, want none", what, m40bNorm(got.Text()))
		case total != "" && (got.Length() != 1 || m40bNorm(got.Text()) != "Taxable total "+total):
			t.Errorf("%s: the grid's total reads %q (%d), want %q", what, m40bNorm(got.Text()), got.Length(), "Taxable total "+total)
		}
	}
}
