package gstcalc_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
)

// mustState fails the test unless err is nil and got equals want.
func mustState(t *testing.T, call string, got string, err error, want string) {
	t.Helper()
	if err != nil || got != want {
		t.Fatalf("%s = %q, %v; want %q, nil", call, got, err, want)
	}
}

func TestGST_IntraStateSplit(t *testing.T) { // NUM-030
	s, err := gstcalc.SupplierState("27AABCS1429B1ZU", "") // SHREE, Mumbai
	mustState(t, `SupplierState("27AABCS1429B1ZU", "")`, s, err, "27")
	b, err := gstcalc.BranchState("Pune Works", "27AAFCV2449G1Z7", "") // PUNE
	mustState(t, `BranchState("Pune Works", "27AAFCV2449G1Z7", "")`, b, err, "27")
	if gstcalc.IsInterState(s, b) {
		t.Errorf("IsInterState(%q, %q) = true, want false (SHREE to PUNE is intra-state)", s, b)
	}

	if c, sg, i := gstcalc.SplitGST(100000, 18000, false); c != 9000 || sg != 9000 || i != 0 {
		t.Errorf("SplitGST(100000, 18000, false) = (%d, %d, %d), want (9000, 9000, 0)", c, sg, i)
	}
	if h := gstcalc.HalfRate(18000); h != 9000 {
		t.Errorf("HalfRate(18000) = %d, want 9000", h)
	}

	// Reference PO: MS-ANGLE-50 1,200.000 kg @ ₹62.5000 and WIRE-18G 50.000 kg @ ₹96.0000.
	a1, err := gstcalc.LineAmount(1200000, 625000)
	if err != nil || a1 != 7500000 {
		t.Fatalf("LineAmount(1200000, 625000) = %d, %v; want 7500000, nil", a1, err)
	}
	a2, err := gstcalc.LineAmount(50000, 960000)
	if err != nil || a2 != 480000 {
		t.Fatalf("LineAmount(50000, 960000) = %d, %v; want 480000, nil", a2, err)
	}
	inter := gstcalc.IsInterState(s, b)
	got := gstcalc.Totals([]gstcalc.Line{
		{Amount: a1, GSTRate: 18000, InterState: inter},
		{Amount: a2, GSTRate: 18000, InterState: inter},
	}, true)
	if got.Net != 7980000 || got.CGST != 718200 || got.SGST != 718200 || got.IGST != 0 || got.Grand != 9416400 {
		t.Errorf("reference PO totals: Net %d, CGST %d, SGST %d, IGST %d, Grand %d; want 7980000, 718200, 718200, 0, 9416400",
			got.Net, got.CGST, got.SGST, got.IGST, got.Grand)
	}
}

func TestGST_InterStateIGST(t *testing.T) { // NUM-031
	pune, err := gstcalc.BranchState("Pune Works", "27AAFCV2449G1Z7", "")
	mustState(t, `BranchState("Pune Works", "27AAFCV2449G1Z7", "")`, pune, err, "27")
	bharat, err := gstcalc.SupplierState("29AABCT1332L1ZA", "")
	mustState(t, `SupplierState("29AABCT1332L1ZA", "")`, bharat, err, "29")
	if !gstcalc.IsInterState(bharat, pune) {
		t.Errorf("IsInterState(%q, %q) = false, want true (BHARAT to PUNE is inter-state)", bharat, pune)
	}
	if c, s, i := gstcalc.SplitGST(100000, 28000, true); c != 0 || s != 0 || i != 28000 {
		t.Errorf("SplitGST(100000, 28000, true) = (%d, %d, %d), want (0, 0, 28000)", c, s, i)
	}

	shree, err := gstcalc.SupplierState("27AABCS1429B1ZU", "")
	mustState(t, `SupplierState("27AABCS1429B1ZU", "")`, shree, err, "27")
	blr, err := gstcalc.BranchState("Bengaluru Site Office", "29AAFCV2449G1Z3", "")
	mustState(t, `BranchState("Bengaluru Site Office", "29AAFCV2449G1Z3", "")`, blr, err, "29")
	if !gstcalc.IsInterState(shree, blr) {
		t.Errorf("IsInterState(%q, %q) = false, want true (SHREE to BLR is inter-state)", shree, blr)
	}
}

// NUM-037: the rule only; the refusal on DOC's save path is WP3's (CD-40).
func TestGST_NoMixedTaxTypes(t *testing.T) { // NUM-037
	wantMsg(t, gstcalc.CheckTaxType(false, gstcalc.TaxIGST, "27", "27"),
		"This bill is intra-state (supplier and branch are both in Maharashtra), so use CGST and SGST, not IGST.")
	wantMsg(t, gstcalc.CheckTaxType(true, gstcalc.TaxCGSTSGST, "29", "27"),
		"This bill is inter-state (Karnataka to Maharashtra), so use IGST, not CGST and SGST.")
	if err := gstcalc.CheckTaxType(false, gstcalc.TaxCGSTSGST, "27", "27"); err != nil {
		t.Errorf("CheckTaxType(false, TaxCGSTSGST, 27, 27) = %v, want nil", err)
	}
	if err := gstcalc.CheckTaxType(true, gstcalc.TaxIGST, "29", "27"); err != nil {
		t.Errorf("CheckTaxType(true, TaxIGST, 29, 27) = %v, want nil", err)
	}
}

func TestGST_UnregisteredSupplierState(t *testing.T) { // NUM-038
	local, err := gstcalc.SupplierState("", "27") // LOCAL: no GSTIN, address in Maharashtra
	mustState(t, `SupplierState("", "27")`, local, err, "27")
	if !gstcalc.IsInterState(local, "29") {
		t.Errorf("IsInterState(%q, \"29\") = false, want true (LOCAL to BLR)", local)
	}
	if gstcalc.IsInterState(local, "27") {
		t.Errorf("IsInterState(%q, \"27\") = true, want false (LOCAL to PUNE)", local)
	}

	_, err = gstcalc.SupplierState("", "")
	wantMsg(t, err, "Set the state on the supplier's address so GST can be worked out.")
}

func TestGST_BranchStateFallback(t *testing.T) { // NUM-039
	nashik, err := gstcalc.BranchState("Nashik Store", "", "27")
	mustState(t, `BranchState("Nashik Store", "", "27")`, nashik, err, "27")
	shree, err := gstcalc.SupplierState("27AABCS1429B1ZU", "")
	mustState(t, `SupplierState("27AABCS1429B1ZU", "")`, shree, err, "27")
	if gstcalc.IsInterState(shree, nashik) {
		t.Errorf("IsInterState(%q, %q) = true, want false (SHREE to NASHIK is intra-state)", shree, nashik)
	}

	_, err = gstcalc.BranchState("Nashik Store", "", "")
	wantMsg(t, err, "Set the state for branch Nashik Store so GST can be worked out.")
}

func TestGST_StateFromGSTIN(t *testing.T) { // NUM-040
	// RAJKOT's GSTIN says Gujarat; its address record says Maharashtra. The GSTIN wins.
	got, err := gstcalc.SupplierState("24AAACR5055K1ZD", "27")
	mustState(t, `SupplierState("24AAACR5055K1ZD", "27")`, got, err, "24")

	known := []struct{ code, name string }{
		{"24", "Gujarat"},
		{"27", "Maharashtra"},
		{"29", "Karnataka"},
		{"33", "Tamil Nadu"},
	}
	for _, k := range known {
		if name, ok := gstcalc.StateName(k.code); name != k.name || !ok {
			t.Errorf("StateName(%q) = (%q, %v), want (%q, true)", k.code, name, ok, k.name)
		}
	}
	// D-MST-10: legacy 28 (old Andhra Pradesh) is rejected; 96 (Other Countries) is not in gstcalc's table.
	for _, code := range []string{"25", "99", "", "28", "96"} {
		if name, ok := gstcalc.StateName(code); name != "" || ok {
			t.Errorf("StateName(%q) = (%q, %v), want (\"\", false)", code, name, ok)
		}
	}
}
