package gstcalc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
)

// RR1c 3: with no supplier state (no GSTIN and a blank address state) the
// place text claims neither inter- nor intra-state, and says why: in every
// category but SEZ, which is inter-state whatever the state (spec §7.1).
func TestPlaceReason_NoSupplierState(t *testing.T) {
	const note = "GST can't be worked out until the supplier's address has a state."
	for _, cat := range []string{"Registered", "Unregistered", "Composition", "", "sez", "Overseas"} {
		for _, place := range []string{"29", "27"} {
			if got := gstcalc.PlaceReason(cat, "Shree Steel Traders", "", place); got != note {
				t.Errorf("PlaceReason(%q, supplier state blank, %q) = %q, want %q", cat, place, got, note)
			}
		}
	}
	if got := gstcalc.PlaceReason("SEZ", "Shree Steel Traders", "", "29"); got != "Inter-state: Shree Steel Traders is an SEZ supplier." {
		t.Errorf("SEZ with no supplier state = %q, want the SEZ text", got)
	}
}
