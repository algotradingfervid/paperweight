package gstcalc

import "github.com/vardhan/internaltools/internal/types"

// CheckLine rejects a line whose quantity is not positive or whose rate is negative.
func CheckLine(q types.Qty, r types.Rate) error {
	if q <= 0 {
		return &types.InputError{Msg: "Quantity must be more than 0."}
	}
	if r < 0 {
		return &types.InputError{Msg: "Rate cannot be negative."}
	}
	return nil
}

// HalfRate returns the CGST (or SGST) rate for a GST rate: 18000 → 9000.
func HalfRate(p types.Percent) types.Percent { return types.Percent(types.DivRound(int64(p), 2)) }

// tax returns taxable × rate / div rounded half-up; it panics on overflow,
// which bounded inputs never reach.
func tax(taxable types.Money, rate types.Percent, div int64) types.Money {
	t, ok := types.MulDivRound(int64(taxable), int64(rate), div)
	if !ok {
		panic("gstcalc: tax overflows")
	}
	return types.Money(t)
}
