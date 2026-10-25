package components_test

import (
	"fmt"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// TestLineGridRowForm_MatchesGrid: the exported row form is the form the
// grid renders, so a row save can return only #grid-row-<no>.
func TestLineGridRowForm_MatchesGrid(t *testing.T) {
	rows := gridRows(3)
	grid := uitest.Render(t, components.LineGrid(rows, "₹2,25,000.00"))
	for _, r := range rows {
		id := fmt.Sprintf("grid-row-%d", r.No)
		want, err := goquery.OuterHtml(grid.Find("form#" + id))
		if err != nil {
			t.Fatal(err)
		}
		doc := uitest.Render(t, components.LineGridRowForm(r))
		form := doc.Find("form")
		if form.Length() != 1 {
			t.Fatalf("row %d: want one form, got %d", r.No, form.Length())
		}
		got, err := goquery.OuterHtml(form)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("row %d: export differs from grid form\n got: %s\nwant: %s", r.No, got, want)
		}
		for attr, v := range map[string]string{
			"id": id, "hx-target": "this", "hx-include": "#doc-version", "hx-sync": "#line-editor:queue all",
		} {
			if g := form.AttrOr(attr, ""); g != v {
				t.Errorf("row %d: %s = %q, want %q", r.No, attr, g, v)
			}
		}
	}
}
