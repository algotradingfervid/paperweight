package gstcalc

import (
	"strconv"

	"github.com/vardhan/internaltools/internal/types"
)

// enteredTolerance is how far a typed GST amount may be from the computed one: ₹1.00.
const enteredTolerance types.Money = 100

// IsInterStateSupply reports whether a supply is inter-state: always for an
// SEZ party, otherwise when supplier and branch are in different states.
func IsInterStateSupply(category, supplierState, branchState string) bool {
	return category == "SEZ" || IsInterState(supplierState, branchState)
}

// CheckEnteredGST accepts GST amounts typed from a bill when each is within
// ₹1.00 of SplitGST's value and CGST equals SGST.
func CheckEnteredGST(line int, taxable types.Money, rate types.Percent, interState bool, cgst, sgst, igst types.Money) error {
	wantC, wantS, wantI := SplitGST(taxable, rate, interState)
	half := HalfRate(rate)
	checks := []struct {
		name      string
		got, want types.Money
		rate      types.Percent
	}{
		{"CGST", cgst, wantC, half},
		{"SGST", sgst, wantS, half},
		{"IGST", igst, wantI, rate},
	}
	for _, c := range checks {
		if d := c.got - c.want; d > enteredTolerance || d < -enteredTolerance {
			return &types.InputError{Msg: c.name + " on line " + strconv.Itoa(line) + " is " +
				types.FormatINR(c.got) + " but " + types.FormatPercent(c.rate) + " of " + types.FormatINR(taxable) +
				" is " + types.FormatINR(c.want) + ". Check the rate or the amount."}
		}
	}
	if cgst != sgst {
		return &types.InputError{Msg: "CGST and SGST must be the same amount."}
	}
	return nil
}

// FromInclusive splits a tax-inclusive amount into taxable value and GST. Any
// rounding shortfall goes to the taxable value, so the parts sum to gross.
func FromInclusive(gross types.Money, rate types.Percent, interState bool) (taxable, cgst, sgst, igst types.Money) {
	t, ok := types.MulDivRound(int64(gross), 100_000, 100_000+int64(rate))
	if !ok {
		panic("gstcalc: inclusive amount overflows")
	}
	taxable = types.Money(t)
	cgst, sgst, igst = SplitGST(taxable, rate, interState)
	taxable = gross - cgst - sgst - igst
	return taxable, cgst, sgst, igst
}
