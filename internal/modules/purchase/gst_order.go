package purchase

import (
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/types"
)

// orderTotals taxes each line and sums the PO (PO spec §3.1, §7): the split
// is inter-state for an SEZ vendor or when the vendor's state differs from
// the delivery site's branch state (gstcalc.IsInterStateSupply). Under
// reverse charge the tax is still computed and shown, but the grand and
// rounded totals are the taxable value alone.
func orderTotals(category, supplierState, placeState string, reverseCharge bool, amounts []types.Money, rates []types.Percent) gstcalc.DocTotals {
	inter := gstcalc.IsInterStateSupply(category, supplierState, placeState)
	lines := make([]gstcalc.Line, len(amounts))
	for i := range amounts {
		lines[i] = gstcalc.Line{Amount: amounts[i], GSTRate: rates[i], InterState: inter}
	}
	t := gstcalc.Totals(lines, true)
	if reverseCharge {
		t.Grand, t.Rounded = t.Net, t.Net.RoundRupee()
	}
	return t
}
