package grnweb_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4c: each step's Check card has its own tile, the Order one matching
// the step's form card.
func TestCheckTiles_Receipt(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.s2RefPO())
	w.s2Delivery(id)
	uitest.WantCheckTiles(t, s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").doc,
		uitest.CheckTile{Title: "Order", Icon: "shopping-cart"},
		uitest.CheckTile{Title: "Delivery", Icon: "truck"},
		uitest.CheckTile{Title: "Quantities", Icon: "package-check"})
}
