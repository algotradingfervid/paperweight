package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// RR1b finding 6: the sort select shows its choice through a button's
// selectedcontent, which clips with an ellipsis inside the select's end
// padding, so the text never runs under the arrow; the options follow.
func TestFilterBar_SortSelectedContent(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{Action: "/x", Sorts: []components.Option{{Value: "date", Label: "Date"}, {Value: "name", Label: "Name"}}, Sort: "name"}))
	sel := doc.Find("select#filter-sort")
	if sel.Find("button > selectedcontent").Length() != 1 {
		t.Error("the sort select has no button holding selectedcontent")
	}
	if got := sel.Find("option[selected]").Text(); got != "Name" {
		t.Errorf("selected option = %q, want Name", got)
	}
}
