package types

import (
	"math"
	"math/bits"
)

// Every rounding here is half-up with ties away from zero (D-NUM-07): the
// magnitude is rounded and the sign reapplied, so Round(-x) == -Round(x).

// abs returns |x| as a uint64 (exact for math.MinInt64) and whether x < 0.
func abs(x int64) (uint64, bool) {
	if x < 0 {
		return -uint64(x), true //nolint:gosec // G115: two's-complement negation gives |x|, exact for MinInt64
	}
	return uint64(x), false
}

// signed applies neg to a magnitude.
func signed(u uint64, neg bool) int64 {
	if neg {
		return -int64(u) //nolint:gosec // G115: callers pass magnitudes <= MaxInt64 (or 1<<63 for MinInt64)
	}
	return int64(u) //nolint:gosec // G115: callers pass magnitudes <= MaxInt64
}

// DivRound returns n/d rounded half away from zero. d must be > 0.
func DivRound(n, d int64) int64 {
	u, neg := abs(n)
	ud := uint64(d) //nolint:gosec // G115: d > 0 by contract
	q, r := u/ud, u%ud
	if r >= ud-r { // 2r >= d without overflow
		q++
	}
	return signed(q, neg)
}

// RoundTo returns the multiple of unit nearest to x, in the same scale.
// unit must be > 0 and |x| <= MaxInt64 - unit/2.
func RoundTo(x, unit int64) int64 {
	return DivRound(x, unit) * unit
}

// Rescale converts x from fromDP decimals to toDP decimals
// (0 <= fromDP - toDP <= 18).
func Rescale(x int64, fromDP, toDP int) int64 {
	d := int64(1)
	for range fromDP - toDP {
		d *= 10
	}
	return DivRound(x, d)
}

// MulDivRound returns a×b/d rounded half away from zero, computing the
// product in 128 bits. d must be > 0. It reports false on overflow, and
// also for an exact result of -2^63, which it rejects deliberately (callers
// are bounded far below it).
func MulDivRound(a, b, d int64) (int64, bool) {
	ua, na := abs(a)
	ub, nb := abs(b)
	ud := uint64(d) //nolint:gosec // G115: d > 0 by contract
	hi, lo := bits.Mul64(ua, ub)
	if hi >= ud {
		return 0, false
	}
	q, r := bits.Div64(hi, lo, ud)
	if q > math.MaxInt64 { // checked before q++, which could wrap at MaxUint64
		return 0, false
	}
	if r >= ud-r {
		q++
	}
	if q > math.MaxInt64 {
		return 0, false
	}
	return signed(q, na != nb), true
}

// Times returns the line amount q × r in paise (1e-3 × 1e-4 → 1e-2).
func (q Qty) Times(r Rate) (Money, error) {
	m, ok := MulDivRound(int64(q), int64(r), 100_000)
	if !ok || m > int64(MaxMoney) || m < -int64(MaxMoney) {
		return 0, &InputError{Msg: "Line amount is too large. The most a line can be is ₹10,00,00,00,000.00."}
	}
	return Money(m), nil
}

// RoundRupee rounds m to whole rupees.
func (m Money) RoundRupee() Money { return Money(RoundTo(int64(m), 100)) }

// ToMoney rounds a 4-decimal rate to 2-decimal money.
func (r Rate) ToMoney() Money { return Money(Rescale(int64(r), 4, 2)) }
