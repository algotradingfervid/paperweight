package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// R5 (GRN M3): a Wide column's grid track is named in data-wide, as NoWrap
// tracks are in data-nowrap, counting the title's track when there is no
// title column; with none the attribute is left out.
func TestDataList_WideTracks(t *testing.T) {
	cols := []components.Column{
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "vendor", Label: "Vendor", Priority: components.PMeta, Wide: true},
		{Key: "date", Label: "Date", Priority: components.PMeta, NoWrap: true},
		{Key: "project", Label: "Project", Priority: components.PLow, Wide: true},
		{Key: "state", Label: "State", Priority: components.PState},
	}
	rows := []components.Row{{Href: "/x/1", Title: "PO/1", Values: map[string]string{"amount": "₹1.00"}}}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Orders", Columns: cols, Rows: rows}))
	dl := doc.Find("div.datalist")
	if got := dl.AttrOr("data-wide", ""); got != "3 5" {
		t.Errorf("data-wide = %q, want 3 5 (vendor and project after title and amount)", got)
	}
	if n := dl.Find("dl.datalist-wide").Length(); n != 2 {
		t.Errorf("%d cells marked datalist-wide, want vendor and project (blank, kept for its track)", n)
	}
	if got := dl.AttrOr("data-nowrap", ""); got != "4" {
		t.Errorf("data-nowrap = %q, want 4", got)
	}
	cols[1].Wide, cols[3].Wide = false, false
	doc = uitest.Render(t, components.DataList(components.DataListProps{Label: "Orders", Columns: cols, Rows: rows}))
	if _, ok := doc.Find("div.datalist").Attr("data-wide"); ok {
		t.Error("data-wide with no Wide column")
	}
}
