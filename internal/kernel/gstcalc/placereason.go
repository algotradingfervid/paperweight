package gstcalc

// PlaceReason explains why a supply is intra- or inter-state, in the words of
// spec §7.1. States are GST state codes, shown by name (the code when unknown).
func PlaceReason(category, supplierName, supplierState, placeState string) string {
	switch {
	case category == "SEZ":
		return "Inter-state: " + supplierName + " is an SEZ supplier."
	case IsInterStateSupply(category, supplierState, placeState):
		return "Inter-state: the supplier is in " + stateLabel(supplierState) +
			" and the delivery site is in " + stateLabel(placeState) + "."
	default:
		return "Intra-state: the supplier and the delivery site are both in " + stateLabel(placeState) + "."
	}
}
