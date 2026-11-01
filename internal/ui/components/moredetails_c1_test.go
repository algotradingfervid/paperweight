package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// MoreDetails (UI v2 §5.5 item 3, N5) is a Disclosure whose summary names
// what is inside in lower case, keeping acronyms, and holds its fields.
func TestMoreDetails_SummaryOpenAndChildren(t *testing.T) {
	for _, c := range []struct {
		inside []string
		open   bool
		want   string
	}{
		{[]string{"Project", "Required by"}, false, "More details · project, required by"},
		{[]string{"Bill in company GSTIN", "PAN"}, true, "More details · bill in company GSTIN, PAN"},
		{nil, false, "More details"},
	} {
		doc := uitest.Render(t, components.MoreDetails(c.inside, c.open))
		d := doc.Find("details.disclosure")
		if d.Length() != 1 {
			t.Fatalf("%v: %d details.disclosure, want 1", c.inside, d.Length())
		}
		if got := text(d.Find("summary")); got != c.want {
			t.Errorf("summary = %q, want %q", got, c.want)
		}
		if _, open := d.Attr("open"); open != c.open {
			t.Errorf("%v: open = %v, want %v", c.inside, open, c.open)
		}
	}
	if got := components.MoreSummary([]string{"Same receipt", "No receipt"}); got != "More details · same receipt, no receipt" {
		t.Errorf("MoreSummary = %q", got)
	}
}
