package uom

import (
	"testing"

	"github.com/vardhan/internaltools/internal/types"
)

func mustQty(t *testing.T, s string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("types.ParseQty(%q): %v", s, err)
	}
	return q
}

func mustFactor(t *testing.T, s string) types.Factor {
	t.Helper()
	f, err := types.ParseFactor(s)
	if err != nil {
		t.Fatalf("types.ParseFactor(%q): %v", s, err)
	}
	return f
}

func mustRate(t *testing.T, s string) types.Rate {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatalf("types.ParseRate(%q): %v", s, err)
	}
	return r
}

func TestItemUoM_ConvertToStock(t *testing.T) { // MST-084
	// MS-ANGLE-50 (stock KGS, row (TON, 1000)), BOLT-M12 (stock NOS, row
	// (BOX, 100)), CABLE-4C (stock MTR, row (DRUM, 500)). 06-masters.md wins
	// over the plan's (3, 12) example.
	for _, c := range []struct {
		line, qty, factor, want string
	}{
		{"TON → KGS", "2.5", "1000", "2500"},
		{"BOX → NOS", "3", "100", "300"},
		{"DRUM → MTR", "2", "500", "1000"},
	} {
		q, f, want := mustQty(t, c.qty), mustFactor(t, c.factor), mustQty(t, c.want)
		if got := ToStock(q, f); got != want {
			t.Errorf("%s: ToStock(%s, %s) = %s; want %s", c.line, c.qty, c.factor, types.FormatQty(got), types.FormatQty(want))
		}
	}

	// ₹54,000/TON → ₹54/KGS.
	r, f, want := mustRate(t, "54000"), mustFactor(t, "1000"), mustRate(t, "54")
	if got := RateToStock(r, f); got != want {
		t.Errorf("RateToStock(₹54,000.0000, 1000) = %s; want %s", types.FormatRate(got), types.FormatRate(want))
	}
}
