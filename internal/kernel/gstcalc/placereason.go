package gstcalc

// msgNoSupplierState is the place text while the supplier has no state (no
// GSTIN and a blank address state): neither side can be claimed (RR1c).
const msgNoSupplierState = "GST can't be worked out until the supplier's address has a state."

// PlaceReason explains why a supply is intra- or inter-state, in the words of
// spec §7.1. States are GST state codes, shown by name (the code when unknown).
// With no supplier state only an SEZ supply is known to be inter-state.
func PlaceReason(category, supplierName, supplierState, placeState string) string {
	switch {
	case category == "SEZ":
		return "Inter-state: " + supplierName + " is an SEZ supplier."
	case supplierState == "":
		return msgNoSupplierState
	case IsInterStateSupply(category, supplierState, placeState):
		return "Inter-state: the supplier is in " + stateLabel(supplierState) +
			" and the delivery site is in " + stateLabel(placeState) + "."
	default:
		return "Intra-state: the supplier and the delivery site are both in " + stateLabel(placeState) + "."
	}
}
