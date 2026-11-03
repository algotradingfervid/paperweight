package components_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// R5: quick chips are GET links in a labelled group, plain words with the
// count, the current one marked aria-current (UI v2 §5.12, A-23).
func TestFilterBar_QuickChips(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/doc/po",
		Quick: []components.QuickChip{
			{Label: "All", Href: "/doc/po"},
			{Label: "Draft", Href: "/doc/po?f=state%3Ain%3Adraft", Count: "3"},
			{Label: "Approved", Href: "/doc/po?f=state%3Ain%3Aapproved", Count: "1,204", Current: true},
		},
	}))
	ul := doc.Find(`ul[aria-label="Quick filters"]`)
	if ul.Length() != 1 {
		t.Fatalf("%d quick filter groups, want 1", ul.Length())
	}
	var got []string
	ul.Find("a.chip").Each(func(_ int, a *goquery.Selection) {
		got = append(got, text(a)+" -> "+a.AttrOr("href", "")+" current="+a.AttrOr("aria-current", ""))
	})
	want := []string{
		"All -> /doc/po current=",
		"Draft 3 -> /doc/po?f=state%3Ain%3Adraft current=",
		"Approved 1,204 -> /doc/po?f=state%3Ain%3Aapproved current=true",
	}
	if !slices.Equal(got, want) {
		t.Errorf("quick chips = %q, want %q", got, want)
	}
	if n := ul.Find("svg").Length(); n != 0 {
		t.Errorf("quick chips carry %d icons; they are plain words", n)
	}

	doc = uitest.Render(t, components.FilterBar(components.FilterBarProps{Action: "/doc/po"}))
	if doc.Find(`ul[aria-label="Quick filters"]`).Length() != 0 {
		t.Error("a quick filter group with no chips")
	}
}

// R5: the Filters control is a disclosure inside the bar's GET form, so it
// works without JavaScript; its ticks are named checkboxes, the active
// count shows on the button, and Export sits in it for phones.
func TestFilterBar_FiltersPanel(t *testing.T) {
	p := components.FilterBarProps{
		Action: "/doc/po", SearchLabel: "Search purchase orders",
		Sorts:  []components.Option{{Value: "number.asc", Label: "Number"}},
		Active: 2,
		Panel: []components.FilterGroup{{
			Label: "State", Name: "f",
			Options: []components.Option{{Value: "state:in:draft", Label: "Draft"}, {Value: "state:in:approved", Label: "Approved"}},
			Checked: []string{"state:in:approved"},
		}},
		Export: &components.Link{Label: "Export", Href: "/doc/po/export.csv"},
	}
	doc := uitest.Render(t, components.FilterBar(p))
	form := doc.Find("form.filterbar")
	if got := form.AttrOr("hx-trigger", ""); got != "submit, change from:#filter-q, change from:#filter-sort" {
		t.Errorf("hx-trigger = %q: a tick must not reload the list and close the panel", got)
	}
	det := form.Find("details.filterbar-panel")
	if det.Length() != 1 {
		t.Fatalf("%d Filters disclosures in the form, want 1", det.Length())
	}
	if got := text(det.Find("summary")); got != "Filters 2 on" {
		t.Errorf("summary = %q, want Filters 2 on", got)
	}
	var boxes []string
	det.Find(`input[type="checkbox"]`).Each(func(_ int, s *goquery.Selection) {
		_, on := s.Attr("checked")
		boxes = append(boxes, s.AttrOr("name", "")+"="+s.AttrOr("value", "")+" "+map[bool]string{true: "on", false: "off"}[on])
	})
	if want := []string{"f=state:in:draft off", "f=state:in:approved on"}; !slices.Equal(boxes, want) {
		t.Errorf("checkboxes = %q, want %q", boxes, want)
	}
	if got := text(det.Find("legend")); got != "State" {
		t.Errorf("group legend = %q", got)
	}
	if det.Find(`button[type="submit"]`).Length() != 1 || !strings.Contains(text(det.Find(`button[type="submit"]`)), "Show results") {
		t.Error("no Show results submit button in the panel")
	}
	if det.Find(`.filterbar-narrow a[href="/doc/po/export.csv"]`).Length() != 1 {
		t.Error("Export is not in the panel for phones")
	}
	bar := form.Find(`.filterbar-result a[href="/doc/po/export.csv"]`)
	if bar.Length() != 1 || !bar.HasClass("filterbar-wide") {
		t.Errorf("bar Export: %d, wide-only %v; with a panel it shows from 64rem only", bar.Length(), bar.HasClass("filterbar-wide"))
	}

	p.Active = 0
	doc = uitest.Render(t, components.FilterBar(p))
	if got := text(doc.Find("details.filterbar-panel summary")); got != "Filters" {
		t.Errorf("summary with nothing on = %q, want Filters", got)
	}

	p.Panel = nil
	doc = uitest.Render(t, components.FilterBar(p))
	if doc.Find("details").Length() != 0 {
		t.Error("a Filters disclosure with no groups")
	}
	if got := doc.Find("form").AttrOr("hx-trigger", ""); got != "change, submit" {
		t.Errorf("hx-trigger with no panel = %q, want change, submit", got)
	}
	if doc.Find(`a[href="/doc/po/export.csv"]`).HasClass("filterbar-wide") {
		t.Error("with no panel Export stays on phones")
	}
}

// R5: the search box has an icon and a placeholder; with a placeholder the
// label is for screen readers only. Sort has an icon and a hidden label.
func TestFilterBar_SearchIconPlaceholder(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/doc/po", SearchLabel: "Search purchase orders", Placeholder: "Number or remarks",
		Sorts: []components.Option{{Value: "number.asc", Label: "Number"}},
	}))
	in := doc.Find("#filter-q")
	if got := in.AttrOr("placeholder", ""); got != "Number or remarks" {
		t.Errorf("placeholder = %q", got)
	}
	if in.Parent().Find("svg").Length() != 1 {
		t.Error("no search icon beside the input")
	}
	if !doc.Find(`label[for="filter-q"]`).HasClass("visually-hidden") {
		t.Error("search label still visible with a placeholder")
	}
	if !doc.Find(`label[for="filter-sort"]`).HasClass("visually-hidden") || doc.Find("#filter-sort").Parent().Find("svg").Length() != 1 {
		t.Error("sort: want a hidden label and an icon")
	}

	doc = uitest.Render(t, components.FilterBar(components.FilterBarProps{Action: "/search", SearchLabel: "Search"}))
	if doc.Find(`label[for="filter-q"]`).HasClass("visually-hidden") {
		t.Error("with no placeholder the label is the only hint; keep it visible")
	}
}

// R5, CD-366: on phones New is the bottom nav's, so the bar's New shows
// from 64rem only when the shell's New sheet offers the same page; a New
// the sheet doesn't offer (Items) stays on phones.
func TestFilterBar_NewOnPhonesOnlyWhenNotInNav(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{New: []ui.NavItem{{Label: "Purchase order", Href: "/doc/po/new"}}})
	for label, c := range map[string]struct {
		href string
		wide bool
	}{
		"in the sheet":     {"/doc/po/new", true},
		"not in the sheet": {"/masters/items/new", false},
	} {
		doc := uitest.RenderCtx(t, ctx, components.FilterBar(components.FilterBarProps{
			Action: "/x", New: &components.Link{Label: "New", Href: c.href},
		}))
		a := doc.Find(`a[href="` + c.href + `"]`)
		if a.Length() != 1 || a.HasClass("filterbar-wide") != c.wide {
			t.Errorf("%s: %d New links, wide-only %v; want 1, %v", label, a.Length(), a.HasClass("filterbar-wide"), c.wide)
		}
	}
}

// A panel with no search box still gets its row and Filters button; a count
// with no sorts draws no sort select.
func TestFilterBar_PanelWithoutSearchAndCountWithoutSort(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/m/uom", Count: "3 units",
		Panel: []components.FilterGroup{{Label: "Show", Name: "include_disabled", Options: []components.Option{{Value: "true", Label: "Disabled units too"}}}},
	}))
	if doc.Find(".filterbar-row details.filterbar-panel").Length() != 1 {
		t.Error("no Filters button without a search box")
	}
	if doc.Find("#filter-sort, label[for=\"filter-sort\"]").Length() != 0 {
		t.Error("a sort select with no sorts")
	}
	if got := text(doc.Find(".filterbar-result .filterbar-count")); got != "3 units" {
		t.Errorf("count = %q", got)
	}
}
