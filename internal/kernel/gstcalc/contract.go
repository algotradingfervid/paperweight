// Package gstcalc computes line amounts and GST (spec §5, CD-31). NUM owns it.
package gstcalc

import (
	"github.com/vardhan/internaltools/internal/types"
)

type Line struct {
	Amount     types.Money
	GSTRate    types.Percent
	InterState bool
}

type LineTax struct{ Taxable, CGST, SGST, IGST types.Money }

type DocTotals struct {
	Lines                                                      []LineTax
	Net, CGST, SGST, IGST, Grand, Rounded, Adjustment, Payable types.Money
}

// LineAmount returns q × r in paise, rounded half-up; too large a result is an *types.InputError.
func LineAmount(q types.Qty, r types.Rate) (types.Money, error) { return q.Times(r) }

// SplitGST returns the tax on taxable at rate: all IGST when interState,
// otherwise CGST = SGST = taxable × rate/2, rounded once. rate must be in
// 0..MaxPercent; anything else is a programming error and panics.
func SplitGST(taxable types.Money, rate types.Percent, interState bool) (cgst, sgst, igst types.Money) {
	if rate < 0 || rate > types.MaxPercent {
		panic("gstcalc: GST rate out of range")
	}
	if interState {
		return 0, 0, tax(taxable, rate, 100_000)
	}
	half := tax(taxable, rate, 200_000)
	return half, half, 0
}

func IsInterState(supplierState, receiverState string) bool { return false }

// Totals taxes each line on its own and sums the lines into the header (Line carries InterState).
func Totals(lines []Line, rounded bool) DocTotals { return totals(lines, rounded) }

func StateName(code string) (string, bool) { return "", false }
