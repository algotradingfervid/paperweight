package types

import (
	"math"
	"math/big"
	"testing"
)

func TestMulDivRound_RoundUpOverflow(t *testing.T) {
	// 31 × 1190112520884487201 / 2: the quotient is MaxUint64 and its round-up wrapped to 0.
	for _, a := range []int64{31, -31} {
		if got, ok := MulDivRound(a, 1190112520884487201, 2); ok {
			t.Errorf("MulDivRound(%d, 1190112520884487201, 2) = %d, true; want ok = false", a, got)
		}
	}
}

// bigMulDivRound is the math/big reference: a×b/d, half away from zero.
func bigMulDivRound(a, b, d int64) *big.Int {
	n := new(big.Int).Mul(big.NewInt(a), big.NewInt(b))
	neg := n.Sign() < 0
	n.Abs(n)
	bd := big.NewInt(d)
	q, r := new(big.Int).QuoRem(n, bd, new(big.Int))
	if r.Lsh(r, 1).Cmp(bd) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	if neg {
		q.Neg(q)
	}
	return q
}

func FuzzMulDivRound_MatchesBig(f *testing.F) {
	for _, s := range [][3]int64{
		{31, 1190112520884487201, 2},
		{-31, 1190112520884487201, 2},
		{math.MaxInt64, math.MaxInt64, 1},
		{math.MinInt64, math.MinInt64, 1_000_000_000},
		{math.MinInt64, 1, 1},
		{math.MaxInt64, 1, 1},
		{math.MaxInt64, -1, 1},
		{math.MinInt64, -1, 1},
		{math.MaxInt64, 2, 2},
		{math.MaxInt64, 1, 2},
		{math.MinInt64, 1, 2},
		{1, 1, 2},
		{-1, 1, 2},
		{0, math.MinInt64, 1},
	} {
		f.Add(s[0], s[1], s[2])
	}
	minOK := big.NewInt(math.MinInt64 + 1)
	maxOK := big.NewInt(math.MaxInt64)
	f.Fuzz(func(t *testing.T, a, b, d int64) {
		if d < 1 || d > 1_000_000_000 { // fold into [1, 1e9]
			d = (d%1_000_000_000+1_000_000_000)%1_000_000_000 + 1
		}
		want := bigMulDivRound(a, b, d)
		got, ok := MulDivRound(a, b, d)
		if want.Cmp(minOK) >= 0 && want.Cmp(maxOK) <= 0 {
			if !ok || got != want.Int64() {
				t.Fatalf("MulDivRound(%d, %d, %d) = %d, %v; want %s, true", a, b, d, got, ok, want)
			}
		} else if ok {
			t.Fatalf("MulDivRound(%d, %d, %d) = %d, true; want ok = false (exact %s)", a, b, d, got, want)
		}
	})
}
