package types

import "testing"

func TestQty_WholeNumberUoM(t *testing.T) { // NUM-007
	// BOLT-M12 in Nos (whole-number): 3.4 fails; 3 and 3.000 (both Qty(3000)) pass.
	wantInputErr(t, Qty(3400).CheckUoM("Nos", true), "Quantity for Nos must be a whole number (you entered 3.4).")
	if err := Qty(3000).CheckUoM("Nos", true); err != nil {
		t.Fatalf("Qty(3000).CheckUoM(Nos, true) = %v, want nil", err)
	}
	// MS-ANGLE-50 in kg (not whole-number): 3.4 passes.
	if err := Qty(3400).CheckUoM("kg", false); err != nil {
		t.Fatalf("Qty(3400).CheckUoM(kg, false) = %v, want nil", err)
	}
}

func TestFactor_StockQtyRounding(t *testing.T) { // NUM-014
	// 3,333.333 kg × 0.6 = 1,999.9998 exact → 2,000.000 half-up.
	q, err := StockQty(Qty(3333333), Factor(600_000_000))
	if err != nil || q != Qty(2000000) {
		t.Fatalf("StockQty(3333333, 600000000) = %d, %v; want 2000000, nil", q, err)
	}
	_, err = StockQty(MaxQty, Factor(2_000_000_000))
	wantInputErr(t, err, "Quantity is too large. The maximum is 1,00,00,000.")
	_, err = ParseFactor("0.0000000001")
	wantInputErr(t, err, "Conversion factor can have at most 9 decimal places.")
	_, err = ParseFactor("0")
	wantInputErr(t, err, "Conversion factor must be more than 0.")
}
