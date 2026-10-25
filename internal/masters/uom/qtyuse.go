package uom

// QtyUse names a lines table that stores quantities in a UoM, so turning a
// UoM's whole-number flag on can look for fractional quantities (MST-092,
// Task 26).
type QtyUse struct {
	DocType, LinesTable, ParentColumn, UoMColumn, QtyColumn string
}

var qtyUses []QtyUse

// RegisterQtyUse records u. Call it from a package init().
func RegisterQtyUse(u QtyUse) { qtyUses = append(qtyUses, u) }
