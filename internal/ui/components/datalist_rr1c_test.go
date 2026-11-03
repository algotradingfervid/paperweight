package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// RR1c 1: a column whose header label holds a word of more than 8 letters
// ("Quantities", "Reference") has its grid track named in data-label, so the CSS gives
// the track a minimum that fits the word; a word of exactly 8 letters
// ("Employee") or a shorter one doesn't, and with none the attribute is left
// out.
func TestDataList_LabelTracks(t *testing.T) {
	cols := []components.Column{
		{Key: "po", Label: "Quantities", Priority: components.PTitle},
		{Key: "value", Label: "Value", Priority: components.PFigure, Num: true},
		{Key: "employee", Label: "Employee", Priority: components.PMeta},
		{Key: "ref", Label: "Reference no.", Priority: components.PMeta},
		{Key: "order", Label: "Order, rate", Priority: components.PLow},
	}
	rows := []components.Row{{Href: "/x/1", Values: map[string]string{"po": "PO/1"}}}
	render := func() (string, bool) {
		return uitest.Render(t, components.DataList(components.DataListProps{Label: "Lines", Columns: cols, Rows: rows})).Find("div.datalist").Attr("data-label")
	}
	if got, _ := render(); got != "1 4" {
		t.Errorf("data-label = %q, want 1 4 (Quantities and Reference, 9 letters)", got)
	}
	cols[0].Label, cols[3].Label = "PO number", "Received"
	if got, ok := render(); ok {
		t.Errorf("data-label = %q with no header word over 8 letters", got)
	}
}
