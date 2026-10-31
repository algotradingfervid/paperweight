package gstcalc_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
)

func TestPlaceReason_Texts(t *testing.T) { // PO-005
	const (
		sameMH = "Intra-state: the supplier and the delivery site are both in Maharashtra."
		kaToMH = "Inter-state: the supplier is in Karnataka and the delivery site is in Maharashtra."
		sezSST = "Inter-state: Shree Steel Traders is an SEZ supplier."
	)
	cases := []struct {
		name                                string
		category, supplier, supState, place string
		want                                string
	}{
		{"same state", "Registered", "Shree Steel Traders", "27", "27", sameMH},
		{"different state", "Registered", "Shree Steel Traders", "29", "27", kaToMH},
		{"SEZ same state", "SEZ", "Shree Steel Traders", "27", "27", sezSST},
		{"SEZ different state", "SEZ", "Shree Steel Traders", "29", "27", sezSST},
		{"SEZ names the supplier", "SEZ", "Metro Cables", "29", "27", "Inter-state: Metro Cables is an SEZ supplier."},
		{"unregistered same state", "Unregistered", "Shree Steel Traders", "27", "27", sameMH},
		{"unregistered different state", "Unregistered", "Shree Steel Traders", "29", "27", kaToMH},
		{"composition same state", "Composition", "Shree Steel Traders", "27", "27", sameMH},
		{"composition different state", "Composition", "Shree Steel Traders", "29", "27", kaToMH},
		{"blank category same state", "", "Shree Steel Traders", "27", "27", sameMH},
		{"blank category different state", "", "Shree Steel Traders", "29", "27", kaToMH},
		{"unknown category same state", "Overseas", "Shree Steel Traders", "27", "27", sameMH},
		{"unknown category different state", "Overseas", "Shree Steel Traders", "29", "27", kaToMH},
		{"lower-case sez same state is not SEZ", "sez", "Shree Steel Traders", "27", "27", sameMH},
		{"lower-case sez different state is not SEZ", "sez", "Shree Steel Traders", "29", "27", kaToMH},
		{"other direction", "Registered", "Shree Steel Traders", "27", "29",
			"Inter-state: the supplier is in Maharashtra and the delivery site is in Karnataka."},
		{"unknown supplier state prints the code", "Registered", "Shree Steel Traders", "99", "27",
			"Inter-state: the supplier is in 99 and the delivery site is in Maharashtra."},
		{"unknown place state prints the code", "Registered", "Shree Steel Traders", "27", "99",
			"Inter-state: the supplier is in Maharashtra and the delivery site is in 99."},
		{"unknown state on both sides prints the code", "Registered", "Shree Steel Traders", "99", "99",
			"Intra-state: the supplier and the delivery site are both in 99."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := gstcalc.PlaceReason(c.category, c.supplier, c.supState, c.place); got != c.want {
				t.Fatalf("PlaceReason(%q, %q, %q, %q) = %q, want %q", c.category, c.supplier, c.supState, c.place, got, c.want)
			}
		})
	}
}

// knownStateCodes returns every two-digit code StateName knows.
func knownStateCodes() []string {
	var codes []string
	for i := range 100 {
		code := fmt.Sprintf("%02d", i)
		if _, ok := gstcalc.StateName(code); ok {
			codes = append(codes, code)
		}
	}
	return codes
}

// label is the state's name, or the code when StateName doesn't know it.
func label(code string) string {
	if name, ok := gstcalc.StateName(code); ok {
		return name
	}
	return code
}

func TestPlaceReason_NeverBlank(t *testing.T) { // PO-005
	codes := knownStateCodes()
	if len(codes) != 37 {
		t.Fatalf("state table has %d codes, want 37", len(codes))
	}
	codes = append(codes, "99")
	categories := []string{"SEZ", "Registered", "Unregistered", "Composition", "", "sez", "Overseas"}
	const supplier = "Shree Steel Traders"
	for _, cat := range categories {
		for _, s := range codes {
			for _, p := range codes {
				got := gstcalc.PlaceReason(cat, supplier, s, p)
				if got == "" {
					t.Fatalf("PlaceReason(%q, %q, %q, %q) is blank", cat, supplier, s, p)
				}
				inter := gstcalc.IsInterStateSupply(cat, s, p)
				if strings.HasPrefix(got, "Intra-state: ") == inter {
					t.Fatalf("PlaceReason(%q, %q, %q, %q) = %q; IsInterStateSupply = %v", cat, supplier, s, p, got, inter)
				}
				var want string
				switch {
				case cat == "SEZ":
					want = "Inter-state: " + supplier + " is an SEZ supplier."
				case inter:
					want = "Inter-state: the supplier is in " + label(s) + " and the delivery site is in " + label(p) + "."
				default:
					want = "Intra-state: the supplier and the delivery site are both in " + label(p) + "."
				}
				if got != want {
					t.Fatalf("PlaceReason(%q, %q, %q, %q) = %q, want %q", cat, supplier, s, p, got, want)
				}
			}
		}
	}
}
