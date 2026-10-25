package components_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A grid with no taxable total (a DocType without one) shows no "Taxable
// total" label and no empty #grid-total paragraph; with a total it shows
// both.
func TestLineGrid_NoTotalNoTotalRow(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-9"})

	doc := uitest.RenderCtx(t, ctx, components.LineGrid(gridRows(2), ""))
	if doc.Find("form#grid-row-1").Length() != 1 {
		t.Fatal("grid did not render its rows")
	}
	if strings.Contains(doc.Text(), "Taxable total") {
		t.Error(`an empty TaxableTotal still renders the "Taxable total" label`)
	}
	if n := doc.Find("#grid-total").Length(); n != 0 {
		t.Errorf("an empty TaxableTotal renders %d #grid-total elements, want none", n)
	}

	with := uitest.RenderCtx(t, ctx, components.LineGrid(gridRows(2), "₹2,25,000.00"))
	if got := text(with.Find("#grid-total")); got != "Taxable total ₹2,25,000.00" {
		t.Errorf("#grid-total = %q, want Taxable total ₹2,25,000.00", got)
	}
}
