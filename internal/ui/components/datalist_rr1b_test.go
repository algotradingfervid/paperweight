package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// RR1b: the state badge's grid track, always the last, is named in
// data-state, so the CSS keeps it as wide as the badge's longest word; with
// no state column the attribute is left out.
func TestDataList_StateTrack(t *testing.T) {
	cols := []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle},
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "vendor", Label: "Vendor", Priority: components.PMeta},
		{Key: "project", Label: "Project", Priority: components.PLow},
		{Key: "state", Label: "State", Priority: components.PState},
	}
	rows := []components.Row{{Href: "/x/1", Values: map[string]string{"no": "PO/1"}}}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Orders", Columns: cols, Rows: rows}))
	if got := doc.Find("div.datalist").AttrOr("data-state", ""); got != "5" {
		t.Errorf("data-state = %q, want 5 (after title, amount, vendor and project)", got)
	}
	doc = uitest.Render(t, components.DataList(components.DataListProps{Label: "Orders", Columns: cols[:4], Rows: rows}))
	if _, ok := doc.Find("div.datalist").Attr("data-state"); ok {
		t.Error("data-state with no state column")
	}
}

// RR1b: a row with no State draws no badge in the state column, and the
// cell is blank (hidden on cards, its track kept in rows); a row with a
// State still draws its badge.
func TestDataList_NoStateNoBadge(t *testing.T) {
	cols := []components.Column{
		{Key: "no", Label: "Number", Priority: components.PTitle},
		{Key: "disabled", Label: "Disabled", Priority: components.PState},
	}
	rows := []components.Row{
		{Href: "/x/1", Values: map[string]string{"no": "A"}},
		{Href: "/x/2", Values: map[string]string{"no": "B"}, State: "Disabled"},
	}
	doc := uitest.Render(t, components.DataList(components.DataListProps{Label: "Units", Columns: cols, Rows: rows}))
	li := doc.Find("ul.datalist-rows > li")
	if n := li.Eq(0).Find(".badge").Length(); n != 0 {
		t.Errorf("row with no State has %d badges, want none", n)
	}
	if li.Eq(0).Find("dl.datalist-blank").Length() != 1 {
		t.Error("row with no State: its state cell isn't blank")
	}
	if got := li.Eq(1).Find(".badge").Text(); got != "Disabled" {
		t.Errorf("row with State Disabled shows badge %q", got)
	}
	if li.Eq(1).Find("dl.datalist-blank").Length() != 0 {
		t.Error("row with a State: its state cell is blank")
	}
}
