package gstcalc_test

import (
	"math"
	"math/big"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/types"
)

// wantMsg fails unless err is non-nil and its text is exactly msg.
func wantMsg(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil || err.Error() != msg {
		t.Fatalf("err = %v, want %q", err, msg)
	}
}

// abs returns |x|, with MinInt64 mapped to MaxInt64 so fuzz inputs never stay negative.
func abs(x int64) int64 {
	if x == math.MinInt64 {
		return math.MaxInt64
	}
	if x < 0 {
		return -x
	}
	return x
}

// roundHalfUpRat rounds a non-negative rational to the nearest integer, ties up.
func roundHalfUpRat(r *big.Rat) *big.Int {
	num := new(big.Int).Mul(r.Num(), big.NewInt(2))
	num.Add(num, r.Denom())
	den := new(big.Int).Mul(r.Denom(), big.NewInt(2))
	return num.Quo(num, den)
}

// refTax is round-half-up(taxable × rate / div), computed exactly.
func refTax(taxable types.Money, rate types.Percent, div int64) int64 {
	n := new(big.Int).Mul(big.NewInt(int64(taxable)), big.NewInt(int64(rate)))
	return roundHalfUpRat(new(big.Rat).SetFrac(n, big.NewInt(div))).Int64()
}

func FuzzLineAmount_MatchesBigRat(f *testing.F) { // NUM-005
	f.Add(int64(2500), int64(333300))
	f.Add(int64(3332), int64(123449))
	f.Add(int64(types.MaxQty-1), int64(types.MaxRate-1))
	f.Fuzz(func(t *testing.T, q, r int64) {
		q = 1 + abs(q)%int64(types.MaxQty)
		r = 1 + abs(r)%int64(types.MaxRate)
		want := roundHalfUpRat(new(big.Rat).SetFrac(new(big.Int).Mul(big.NewInt(q), big.NewInt(r)), big.NewInt(100000)))
		got, err := gstcalc.LineAmount(types.Qty(q), types.Rate(r))
		if want.Cmp(big.NewInt(int64(types.MaxMoney))) > 0 {
			wantMsg(t, err, "Line amount is too large. The most a line can be is ₹10,00,00,00,000.00.")
			return
		}
		if err != nil || int64(got) != want.Int64() {
			t.Fatalf("LineAmount(%d, %d) = %d, %v; want %s, nil", q, r, got, err, want)
		}
	})
}

func TestLineValues_SignRules(t *testing.T) { // NUM-011
	wantMsg(t, gstcalc.CheckLine(1000, -50000), "Rate cannot be negative.")
	wantMsg(t, gstcalc.CheckLine(0, 10000), "Quantity must be more than 0.")
	wantMsg(t, gstcalc.CheckLine(-1000, 10000), "Quantity must be more than 0.")
	if err := gstcalc.CheckLine(1000, 0); err != nil {
		t.Errorf("CheckLine(1000, 0) = %v, want nil (free item)", err)
	}
}

func TestLineAmount_Table(t *testing.T) { // NUM-013
	cases := []struct {
		q    types.Qty
		r    types.Rate
		want types.Money
	}{
		{6000, 123000, 7380},   // 6 × 12.30 = 73.80
		{3333, 123450, 4115},   // 3.333 × 12.345 = 41.145885
		{1000000, 1234, 12340}, // 1,000 × 0.1234 = 123.40
		{2500, 333300, 8333},   // 2.5 × 33.33 = 83.325
	}
	for _, c := range cases {
		got, err := gstcalc.LineAmount(c.q, c.r)
		if err != nil || got != c.want {
			t.Errorf("LineAmount(%d, %d) = %d, %v; want %d, nil", c.q, c.r, got, err, c.want)
		}
	}
}

func TestGST_HalfPaisaSplit(t *testing.T) { // NUM-032
	wantSplit(t, 1005, 18000, false, 90, 90, 0)
	wantSplit(t, 1005, 18000, true, 0, 0, 181)
}

func TestGST_TieRounding(t *testing.T) { // NUM-033
	wantSplit(t, 1025, 18000, true, 0, 0, 185)
	wantSplit(t, 1025, 18000, false, 92, 92, 0)
	wantSplit(t, 50, 5000, true, 0, 0, 3)
	wantSplit(t, 50, 5000, false, 1, 1, 0)
}

func TestGST_KnownValue6251(t *testing.T) { // NUM-034
	wantSplit(t, 6251, 18000, false, 563, 563, 0)
}

func TestGST_FractionalRates(t *testing.T) { // NUM-035
	wantSplit(t, 10000000, 250, true, 0, 0, 25000)
	wantSplit(t, 10000000, 250, false, 12500, 12500, 0)
	if got := gstcalc.HalfRate(250); got != 125 {
		t.Errorf("HalfRate(250) = %d, want 125", got)
	}
	wantSplit(t, 3333333, 3000, true, 0, 0, 100000)
	wantSplit(t, 3333333, 3000, false, 50000, 50000, 0)
	if got := gstcalc.HalfRate(3000); got != 1500 {
		t.Errorf("HalfRate(3000) = %d, want 1500", got)
	}
	if got := gstcalc.HalfRate(18000); got != 9000 {
		t.Errorf("HalfRate(18000) = %d, want 9000", got)
	}
}

func FuzzGST_HalvesEqual(f *testing.F) { // NUM-036
	rates := []types.Percent{0, 100, 250, 1000, 1500, 3000, 5000, 6000, 12000, 18000, 28000, 40000}
	f.Add(int64(1004), uint8(9)) // taxable = 1 + x: ₹10.05
	f.Add(int64(1024), uint8(9))
	f.Add(int64(49), uint8(6))
	f.Add(int64(3333332), uint8(5))
	f.Add(int64(9_999_999_999), uint8(11))
	f.Fuzz(func(t *testing.T, x int64, i uint8) {
		taxable := types.Money(1 + abs(x)%10_000_000_000) // ₹0.01 … ₹10,00,00,000.00
		rate := rates[int(i)%len(rates)]
		c, s, ig := gstcalc.SplitGST(taxable, rate, false)
		ic, is, g := gstcalc.SplitGST(taxable, rate, true)
		if c != s {
			t.Fatalf("SplitGST(%d, %d, false): CGST %d != SGST %d", taxable, rate, c, s)
		}
		if d := int64(c + s - g); d < -1 || d > 1 {
			t.Fatalf("SplitGST(%d, %d): CGST+SGST %d vs IGST %d differ by more than 1 paisa", taxable, rate, c+s, g)
		}
		if c < 0 || s < 0 || g < 0 || ig != 0 || ic != 0 || is != 0 {
			t.Fatalf("SplitGST(%d, %d): intra (%d,%d,%d) inter (%d,%d,%d); want non-negative, unused tax 0", taxable, rate, c, s, ig, ic, is, g)
		}
		// The plan's formulas: igst = round(taxable×rate/1e5); cgst = sgst = round(taxable×rate/2e5).
		if want := refTax(taxable, rate, 100000); int64(g) != want {
			t.Fatalf("SplitGST(%d, %d, true) IGST = %d, want %d", taxable, rate, g, want)
		}
		if want := refTax(taxable, rate, 200000); int64(c) != want {
			t.Fatalf("SplitGST(%d, %d, false) CGST = %d, want %d", taxable, rate, c, want)
		}
	})
}

func wantSplit(t *testing.T, taxable types.Money, rate types.Percent, inter bool, wc, ws, wi types.Money) {
	t.Helper()
	c, s, i := gstcalc.SplitGST(taxable, rate, inter)
	if c != wc || s != ws || i != wi {
		t.Errorf("SplitGST(%d, %d, %v) = (%d, %d, %d), want (%d, %d, %d)", taxable, rate, inter, c, s, i, wc, ws, wi)
	}
}
