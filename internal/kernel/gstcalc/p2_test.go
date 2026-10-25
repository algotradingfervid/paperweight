package gstcalc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/types"
)

func TestGST_SEZIsInterState(t *testing.T) { // NUM-042
	// SHREE (27) with its GST category changed to SEZ, branch PUNE (27): IGST applies.
	if !gstcalc.IsInterStateSupply("SEZ", "27", "27") {
		t.Errorf(`IsInterStateSupply("SEZ", "27", "27") = false, want true`)
	}
	if gstcalc.IsInterStateSupply("Registered Regular", "27", "27") {
		t.Errorf(`IsInterStateSupply("Registered Regular", "27", "27") = true, want false`)
	}
	// Any other category falls back to IsInterState: BHARAT (29) to PUNE (27).
	if !gstcalc.IsInterStateSupply("Registered Regular", "29", "27") {
		t.Errorf(`IsInterStateSupply("Registered Regular", "29", "27") = false, want true`)
	}
}

func TestGST_EnteredAmountTolerance(t *testing.T) { // NUM-043
	// Line 2: taxable ₹100.00 at 18% intra-state; computed CGST = SGST = ₹9.00.
	const line, taxable, rate = 2, types.Money(10000), types.Percent(18000)

	accept := []struct{ cgst, sgst types.Money }{
		{940, 940},   // ₹0.40 off: the case's example
		{1000, 1000}, // exactly ₹1.00 off: still within tolerance
		{800, 800},   // ₹1.00 under
	}
	for _, a := range accept {
		if err := gstcalc.CheckEnteredGST(line, taxable, rate, false, a.cgst, a.sgst, 0); err != nil {
			t.Errorf("CheckEnteredGST(2, 10000, 18000, false, %d, %d, 0) = %v, want nil", a.cgst, a.sgst, err)
		}
	}

	wantMsg(t, gstcalc.CheckEnteredGST(line, taxable, rate, false, 1250, 1250, 0),
		"CGST on line 2 is ₹12.50 but 9% of ₹100.00 is ₹9.00. Check the rate or the amount.")
	wantMsg(t, gstcalc.CheckEnteredGST(line, taxable, rate, false, 1001, 1001, 0),
		"CGST on line 2 is ₹10.01 but 9% of ₹100.00 is ₹9.00. Check the rate or the amount.")
	wantMsg(t, gstcalc.CheckEnteredGST(line, taxable, rate, false, 900, 940, 0),
		"CGST and SGST must be the same amount.")
}

func TestGST_InclusiveBackCalculation(t *testing.T) { // NUM-044
	cases := []struct {
		gross                     types.Money
		rate                      types.Percent
		inter                     bool
		taxable, cgst, sgst, igst types.Money
	}{
		{1186, 18000, false, 1006, 90, 90, 0},         // ₹11.86 intra: 10.0508 → 10.05, +₹0.01 shortfall to taxable
		{1186, 18000, true, 1005, 0, 0, 181},          // ₹11.86 inter
		{118000, 18000, false, 100000, 9000, 9000, 0}, // ₹1,180.00 intra
	}
	for _, c := range cases {
		tx, cg, sg, ig := gstcalc.FromInclusive(c.gross, c.rate, c.inter)
		if tx != c.taxable || cg != c.cgst || sg != c.sgst || ig != c.igst {
			t.Errorf("FromInclusive(%d, %d, %v) = (%d, %d, %d, %d), want (%d, %d, %d, %d)",
				c.gross, c.rate, c.inter, tx, cg, sg, ig, c.taxable, c.cgst, c.sgst, c.igst)
		}
		if sum := tx + cg + sg + ig; sum != c.gross {
			t.Errorf("FromInclusive(%d, %d, %v): parts sum to %d, want exactly %d", c.gross, c.rate, c.inter, sum, c.gross)
		}
	}
}
