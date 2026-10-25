// Package gstcalc computes line amounts and GST (spec §5, CD-31). NUM owns it.
package gstcalc

import (
	"github.com/vardhan/internaltools/internal/platform/errs"
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

func LineAmount(q types.Qty, r types.Rate) (types.Money, error) { return 0, errs.ErrNotImplemented }

func SplitGST(taxable types.Money, rate types.Percent, interState bool) (cgst, sgst, igst types.Money) {
	return 0, 0, 0
}

func IsInterState(supplierState, receiverState string) bool { return false }

func Totals(lines []Line, rounded bool) DocTotals { return DocTotals{} } // Line carries InterState

func StateName(code string) (string, bool) { return "", false }
