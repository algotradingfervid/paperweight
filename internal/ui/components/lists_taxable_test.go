package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The document page has no taxable total to show; an empty TaxableTotal
// renders no row rather than a label with nothing beside it.
func TestLineList_TaxableTotalOnlyWhenSet(t *testing.T) {
	cards := []components.LineCard{{No: 1, Item: "MS Angle 50x50x5", Code: "MS-ANGLE", Amount: "₹75,000.00", QtyRate: "1,200 kg × ₹62.50"}}

	doc := uitest.Render(t, components.LineList(components.LineListProps{Cards: cards, Total: 1}))
	if strings.Contains(doc.Text(), "Taxable total") {
		t.Error("TaxableTotal \"\": renders a \"Taxable total\" row")
	}
	if doc.Find(".lines-total").Length() != 0 {
		t.Error("TaxableTotal \"\": renders .lines-total")
	}

	doc = uitest.Render(t, components.LineList(components.LineListProps{Cards: cards, Total: 1, TaxableTotal: "₹1,000.00"}))
	row := doc.Find(".lines-total")
	if row.Length() != 1 {
		t.Fatalf("TaxableTotal set: found %d .lines-total rows, want 1", row.Length())
	}
	if text := strings.Join(strings.Fields(row.Text()), " "); !strings.Contains(text, "Taxable total") || !strings.Contains(text, "₹1,000.00") {
		t.Errorf("taxable total row = %q, want \"Taxable total\" and ₹1,000.00", text)
	}
}
