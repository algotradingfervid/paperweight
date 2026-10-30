package components_test

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestFilterBar_NoSearchLabelNoSearchBox(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/approvals", ClearURL: "/approvals",
		Chips: []components.Chip{{Label: "Type: Purchase Order", RemoveURL: "/approvals"}},
		Keep:  []components.Param{{Name: "type", Value: "fixture.Order"}},
	}))
	if n := doc.Find(`input[type="search"], .filterbar-search, label[for="filter-q"]`).Length(); n != 0 {
		t.Errorf("%d search box elements with no SearchLabel, want none", n)
	}
	if got := doc.Find(`form input[type="hidden"][name="type"]`).AttrOr("value", ""); got != "fixture.Order" {
		t.Errorf("kept type = %q, want fixture.Order", got)
	}
	if doc.Find("a.chip").Length() != 1 {
		t.Error("the rest of the bar went with the search box")
	}
}

// Every existing caller names its search box, so it keeps it.
func TestFilterBar_SearchLabelKeepsSearchBox(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/doc/po", Search: "cement", SearchLabel: "Search purchase orders",
	}))
	in := doc.Find(`form input[type="search"][name="q"]`)
	if in.Length() != 1 || in.AttrOr("value", "") != "cement" {
		t.Errorf("%d search inputs, value %q; want one with cement", in.Length(), in.AttrOr("value", ""))
	}
	if got := text(doc.Find(`label[for="filter-q"]`)); got != "Search purchase orders" {
		t.Errorf("search label = %q", got)
	}
}

// A form with no search box is not a search landmark.
func TestFilterBar_SearchRoleOnlyWithSearchBox(t *testing.T) {
	for label, c := range map[string]struct {
		props components.FilterBarProps
		role  string
	}{
		"search box": {components.FilterBarProps{Action: "/doc/po", SearchLabel: "Search purchase orders"}, "search"},
		"no search":  {components.FilterBarProps{Action: "/approvals", Links: []components.Link{{Label: "Expense Claim (1)", Href: "/x"}}}, ""},
	} {
		doc := uitest.Render(t, components.FilterBar(c.props))
		if got := doc.Find("form").AttrOr("role", ""); got != c.role {
			t.Errorf("%s: role = %q, want %q", label, got, c.role)
		}
	}
}

// The first row is drawn only when something goes in it.
func TestFilterBar_NoEmptyRow(t *testing.T) {
	chips := []components.Chip{{Label: "Type: Purchase Order", RemoveURL: "/approvals"}}
	for label, c := range map[string]struct {
		props components.FilterBarProps
		rows  int
	}{
		"chips only": {components.FilterBarProps{Chips: chips}, 0},
		"search":     {components.FilterBarProps{SearchLabel: "Search purchase orders"}, 1},
		"sort":       {components.FilterBarProps{Sorts: []components.Option{{Value: "date", Label: "Date"}}}, 1},
		"filter":     {components.FilterBarProps{FilterURL: "/doc/po/filter"}, 1},
		"links":      {components.FilterBarProps{Links: []components.Link{{Label: "Expense Claim (1)", Href: "/x"}}}, 1},
	} {
		c.props.Action = "/x"
		doc := uitest.Render(t, components.FilterBar(c.props))
		if got := doc.Find(".filterbar-row").Length(); got != c.rows {
			t.Errorf("%s: %d rows, want %d", label, got, c.rows)
		}
	}
}

func TestFilterBar_LinksInBarOrder(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/approvals",
		Links: []components.Link{
			{Label: "Expense Claim (1)", Href: "/approvals?type=fixture.Claim"},
			{Label: "Purchase Order (2)", Href: "/approvals?type=Purchase+Order"},
		},
	}))
	var got []string
	doc.Find(".filterbar-row a.btn").Each(func(_ int, s *goquery.Selection) {
		got = append(got, text(s)+" -> "+s.AttrOr("href", ""))
	})
	want := []string{
		"Expense Claim (1) -> /approvals?type=fixture.Claim",
		"Purchase Order (2) -> /approvals?type=Purchase+Order",
	}
	if !slices.Equal(got, want) {
		t.Errorf("links = %q, want %q", got, want)
	}
}
