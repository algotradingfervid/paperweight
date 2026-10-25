package gstcalc_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/types"
)

func TestTotals_NetIsSumOfLines(t *testing.T) { // NUM-020
	lines := []gstcalc.Line{{Amount: 7380}, {Amount: 4115}, {Amount: 12340}}
	got := gstcalc.Totals(lines, false)
	if got.Net != 23835 {
		t.Errorf("Net = %d, want 23835", got.Net)
	}
	if len(got.Lines) != 3 {
		t.Fatalf("len(Lines) = %d, want 3", len(got.Lines))
	}
	for i, lt := range got.Lines {
		if lt.Taxable != lines[i].Amount {
			t.Errorf("Lines[%d].Taxable = %d, want %d", i, lt.Taxable, lines[i].Amount)
		}
	}

	rev := slices.Clone(lines)
	slices.Reverse(rev)
	gotRev := gstcalc.Totals(rev, false)
	slices.Reverse(gotRev.Lines)
	if !reflect.DeepEqual(got, gotRev) {
		t.Errorf("reordered lines: totals %+v, want %+v", gotRev, got)
	}
}

func TestTax_RoundPerLine(t *testing.T) { // NUM-021
	inter := gstcalc.Totals([]gstcalc.Line{{1025, 18000, true}, {1025, 18000, true}, {1025, 18000, true}}, false)
	if len(inter.Lines) != 3 {
		t.Fatalf("inter len(Lines) = %d, want 3", len(inter.Lines))
	}
	for i, lt := range inter.Lines {
		if lt.IGST != 185 || lt.CGST != 0 || lt.SGST != 0 {
			t.Errorf("inter Lines[%d] = %+v, want IGST 185, CGST 0, SGST 0", i, lt)
		}
	}
	if inter.IGST != 555 {
		t.Errorf("inter IGST = %d, want 555 (never 554)", inter.IGST)
	}

	intra := gstcalc.Totals([]gstcalc.Line{{1025, 18000, false}, {1025, 18000, false}, {1025, 18000, false}}, false)
	if len(intra.Lines) != 3 {
		t.Fatalf("intra len(Lines) = %d, want 3", len(intra.Lines))
	}
	for i, lt := range intra.Lines {
		if lt.CGST != 92 || lt.SGST != 92 || lt.IGST != 0 {
			t.Errorf("intra Lines[%d] = %+v, want CGST 92, SGST 92, IGST 0", i, lt)
		}
	}
	if intra.CGST != 276 || intra.SGST != 276 || intra.IGST != 0 {
		t.Errorf("intra CGST/SGST/IGST = %d/%d/%d, want 276/276/0", intra.CGST, intra.SGST, intra.IGST)
	}
}

func TestTax_HeaderEqualsSumOfLines(t *testing.T) { // NUM-022
	amt, err := gstcalc.LineAmount(1000, 123450)
	if err != nil || amt != 1235 {
		t.Fatalf("LineAmount(1000, 123450) = %d, %v; want 1235, nil", amt, err)
	}
	lines := make([]gstcalc.Line, 250)
	for i := range lines {
		lines[i] = gstcalc.Line{Amount: amt, GSTRate: 18000, InterState: false}
	}
	got := gstcalc.Totals(lines, false)
	if len(got.Lines) != 250 {
		t.Fatalf("len(Lines) = %d, want 250", len(got.Lines))
	}
	var sumC, sumS types.Money
	for i, lt := range got.Lines {
		if lt.CGST != 111 || lt.SGST != 111 {
			t.Fatalf("Lines[%d] CGST/SGST = %d/%d, want 111/111", i, lt.CGST, lt.SGST)
		}
		// Within half a paisa of the line's own taxable × half-rate.
		for _, half := range []types.Money{lt.CGST, lt.SGST} {
			if d := int64(half)*200000 - int64(lt.Taxable)*18000; d < -100000 || d > 100000 {
				t.Errorf("Lines[%d] half %d is more than ₹0.005 from %d × 9%%", i, half, lt.Taxable)
			}
		}
		sumC += lt.CGST
		sumS += lt.SGST
	}
	if got.CGST != 27750 || got.CGST != sumC {
		t.Errorf("header CGST = %d, want 27750 == Σ lines %d (never 27788)", got.CGST, sumC)
	}
	if got.SGST != 27750 || got.SGST != sumS {
		t.Errorf("header SGST = %d, want 27750 == Σ lines %d (never 27788)", got.SGST, sumS)
	}
}

func TestTotals_RoundedTotalHalfUp(t *testing.T) { // NUM-023
	cases := []struct{ grand, rounded, adj types.Money }{
		{7380, 7400, 20},
		{118050, 118100, 50},
		{118049, 118000, -49},
	}
	for _, c := range cases {
		got := gstcalc.Totals([]gstcalc.Line{{Amount: c.grand}}, true)
		if got.Grand != c.grand || got.Rounded != c.rounded || got.Adjustment != c.adj {
			t.Errorf("grand %d: Grand/Rounded/Adjustment = %d/%d/%d, want %d/%d/%d",
				c.grand, got.Grand, got.Rounded, got.Adjustment, c.grand, c.rounded, c.adj)
		}
	}

	// Reference PO (00-fixtures): SHREE to PUNE, intra-state.
	po := gstcalc.Totals([]gstcalc.Line{{7500000, 18000, false}, {480000, 18000, false}}, true)
	if po.Net != 7980000 || po.CGST != 718200 || po.SGST != 718200 || po.IGST != 0 {
		t.Errorf("reference PO Net/CGST/SGST/IGST = %d/%d/%d/%d, want 7980000/718200/718200/0", po.Net, po.CGST, po.SGST, po.IGST)
	}
	if po.Grand != 9416400 || po.Grand != po.Net+po.CGST+po.SGST+po.IGST {
		t.Errorf("reference PO Grand = %d, want 9416400 == Net+CGST+SGST+IGST", po.Grand)
	}
	if po.Rounded != 9416400 || po.Adjustment != 0 || po.Payable != 9416400 {
		t.Errorf("reference PO Rounded/Adjustment/Payable = %d/%d/%d, want 9416400/0/9416400", po.Rounded, po.Adjustment, po.Payable)
	}
}

func TestTotals_RoundingDisabledForDocType(t *testing.T) { // NUM-024
	lines := []gstcalc.Line{{Amount: 1411280}}
	off := gstcalc.Totals(lines, false)
	if off.Grand != 1411280 || off.Rounded != 1411280 || off.Adjustment != 0 || off.Payable != 1411280 {
		t.Errorf("rounding off: Grand/Rounded/Adjustment/Payable = %d/%d/%d/%d, want 1411280/1411280/0/1411280",
			off.Grand, off.Rounded, off.Adjustment, off.Payable)
	}
	on := gstcalc.Totals(lines, true)
	if on.Rounded != 1411300 || on.Adjustment != 20 || on.Payable != 1411300 {
		t.Errorf("rounding on: Rounded/Adjustment/Payable = %d/%d/%d, want 1411300/20/1411300", on.Rounded, on.Adjustment, on.Payable)
	}
}

func TestTax_ZeroRatedLine(t *testing.T) { // NUM-025
	got := gstcalc.Totals([]gstcalc.Line{{500000, 0, false}, {100000, 18000, false}}, false)
	if len(got.Lines) != 2 {
		t.Fatalf("len(Lines) = %d, want 2", len(got.Lines))
	}
	if want := (gstcalc.LineTax{Taxable: 500000}); got.Lines[0] != want {
		t.Errorf("0%% line = %+v, want %+v", got.Lines[0], want)
	}
	if got.CGST != 9000 || got.SGST != 9000 || got.IGST != 0 || got.Grand != 618000 {
		t.Errorf("CGST/SGST/IGST/Grand = %d/%d/%d/%d, want 9000/9000/0/618000", got.CGST, got.SGST, got.IGST, got.Grand)
	}
}
