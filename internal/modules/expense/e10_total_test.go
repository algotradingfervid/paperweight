package expense_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-308: a claim's total (and so its net payable) and an advance's amount
// are sums of lines, each line at most the money maximum. Validate refuses a
// sum over the maximum with a field error on the lines and clears the total
// (CD-302: never a stale value).
const (
	msgClaimTotalTooLarge   = "The total is too large. The most an expense claim can be is ₹10,00,00,00,000.00. Split it into more than one expense claim."
	msgAdvanceTotalTooLarge = "The total is too large. The most an employee advance can be is ₹10,00,00,00,000.00. Split it into more than one employee advance."
	e10Max                  = "10000000000.00" // types.MaxMoney as a form value
	msgLessNegative         = "Less advance can't be negative."
)

// e10OnLines fails the test unless err carries text as a field error on the
// lines as a whole (Field "lines", Line 0).
func e10OnLines(t *testing.T, err error, text string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) || !slices.Contains(he.FieldErrors(), doctype.FieldError{Field: "lines", Message: text}) {
		t.Errorf("err = %v (messages %q); want a field error on lines with %q", err, e2Messages(err), text)
	}
}

// TestExp_TotalTooLarge: three max-value lines are refused on create and on
// an edit of a stored draft, and nothing is written.
func TestExp_TotalTooLarge(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	t.Run("claim", func(t *testing.T) {
		big := V{"amount": e10Max}
		e.WantRefused(t, msgClaimTotalTooLarge, func() error {
			_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, big, big, big))
			e10OnLines(t, err, msgClaimTotalTooLarge)
			return err
		})
		e.WantRefused(t, msgClaimTotalTooLarge, func() error { // one paisa over
			_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil,
				V{"amount": "4000000000.00"}, V{"amount": "3000000000.00"}, V{"amount": "3000000000.01"}))
			return err
		})
		c := e.DraftClaim(t, deepak, nil, nil, nil, nil)
		e.WantRefused(t, msgClaimTotalTooLarge, func() error {
			_, err := e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil, big, big, big)
			return err
		})
		if got := e2ReadClaim(t, e, c.ID); e2Int(got.total) != 300000 || e2Int(got.net) != 300000 {
			t.Errorf("claim total %d, net %d after the refused edit; want 300000, 300000", e2Int(got.total), e2Int(got.net))
		}
	})

	t.Run("advance", func(t *testing.T) {
		big := V{"line_amount": e10Max}
		e.WantRefused(t, msgAdvanceTotalTooLarge, func() error {
			_, err := e.Create(deepak, expense.AdvanceDocType, e.AdvanceInput(t, deepak, nil, big, big, big))
			e10OnLines(t, err, msgAdvanceTotalTooLarge)
			return err
		})
		e.WantRefused(t, msgAdvanceTotalTooLarge, func() error { // one paisa over
			_, err := e.Create(deepak, expense.AdvanceDocType, e.AdvanceInput(t, deepak, nil,
				V{"line_amount": "4000000000.00"}, V{"line_amount": "3000000000.00"}, V{"line_amount": "3000000000.01"}))
			return err
		})
		a := e.DraftAdvance(t, deepak, nil, nil, nil, nil)
		e.WantRefused(t, msgAdvanceTotalTooLarge, func() error {
			_, err := e.Edit(t, deepak, expense.AdvanceDocType, a.ID, nil, big, big, big)
			return err
		})
		if got := e2ReadAdvance(t, e, a.ID); e2Int(got.amount) != 1500000 {
			t.Errorf("advance amount %d after the refused edit; want 1500000", e2Int(got.amount))
		}
	})
}

// TestExp_TotalTooLargeStep: a step save keeps Validate's writes (CD-205,
// CD-207), so the stored total is cleared, not left from the earlier save
// and not over the maximum (CD-302).
func TestExp_TotalTooLargeStep(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	purpose := V{"purpose": "Pune Metro site visit, day 2"}

	c := e.DraftClaim(t, deepak, nil, nil, nil, nil)
	big := V{"amount": e10Max}
	e2bKept(t, "claim with three max lines", msgClaimTotalTooLarge)(e2bStep(t, e, deepak, expense.ClaimDocType, c.ID,
		purpose, map[int]V{1: big, 2: big, 3: big}, 0, "purpose"))
	if got := e2ReadClaim(t, e, c.ID); got.total != nil || got.net != nil {
		t.Errorf("claim total %d, net %d; want both cleared (NULL)", e2Int(got.total), e2Int(got.net))
	}
	e.WantRefused(t, msgClaimTotalTooLarge, func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})

	a := e.DraftAdvance(t, deepak, nil, nil, nil, nil)
	big = V{"line_amount": e10Max}
	e2bKept(t, "advance with three max lines", msgAdvanceTotalTooLarge)(e2bStep(t, e, deepak, expense.AdvanceDocType, a.ID,
		purpose, map[int]V{1: big, 2: big, 3: big}, 0, "purpose"))
	if got := e2ReadAdvance(t, e, a.ID); got.amount != nil {
		t.Errorf("advance amount %d; want cleared (NULL)", e2Int(got.amount))
	}
	e.WantRefused(t, msgAdvanceTotalTooLarge, func() error {
		_, err := e6Try(t, e, deepak, expense.AdvanceDocType, a.ID, doc.ActionSubmit, "")
		return err
	})
}

// TestExp_TotalAtMaximum: a total exactly at the maximum saves and is stored.
func TestExp_TotalAtMaximum(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	want := int64(types.MaxMoney)

	c := e.DraftClaim(t, deepak, nil,
		V{"amount": "4000000000.00"}, V{"amount": "3000000000.00"}, V{"amount": "3000000000.00"})
	if got := e2ReadClaim(t, e, c.ID); e2Int(got.total) != want || e2Int(got.net) != want {
		t.Errorf("claim total %d, net %d; want %d, %d", e2Int(got.total), e2Int(got.net), want, want)
	}

	a := e.DraftAdvance(t, deepak, nil,
		V{"line_amount": "4000000000.00"}, V{"line_amount": "3000000000.00"}, V{"line_amount": "3000000000.00"})
	if got := e2ReadAdvance(t, e, a.ID); e2Int(got.amount) != want {
		t.Errorf("advance amount %d; want %d", e2Int(got.amount), want)
	}
}

// TestExp_LessAdvanceNegative: a negative less advance would raise the
// payout above the total (and so above the maximum), so it is refused on
// save against a paid advance, and nothing is written.
func TestExp_LessAdvanceNegative(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	adv := e.PaidAdvance(t, deepak, 500000)
	against := expensetest.Values{"advance_id": e2ID(adv.ID), "less_advance": "-1.00"}

	e.WantRefused(t, msgLessNegative, func() error {
		_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, against))
		return err
	})
	c := e.DraftClaim(t, deepak, nil)
	e.WantRefused(t, msgLessNegative, func() error {
		_, err := e.Edit(t, deepak, expense.ClaimDocType, c.ID, against)
		return err
	})
}

// TestExp_LessAdvanceNegativeStep: a step save keeps the engine's errors
// outside Only with the values written, so a negative less advance can be
// stored; net payable is then cleared rather than written over the total
// (here one paisa over the maximum).
func TestExp_LessAdvanceNegativeStep(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	adv := e.PaidAdvance(t, deepak, 500000)
	c := e.DraftClaim(t, deepak, nil, V{"amount": e10Max})
	e2bKept(t, "less advance -0.01", msgLessNegative)(e2bStep(t, e, deepak, expense.ClaimDocType, c.ID,
		V{"purpose": "Pune Metro site visit, day 2", "advance_id": e2ID(adv.ID), "less_advance": "-0.01"}, nil, 0, "purpose"))
	if got := e2ReadClaim(t, e, c.ID); got.net != nil || e2Int(got.total) != int64(types.MaxMoney) {
		t.Errorf("total %d, net %d (less %d); want %d, NULL", e2Int(got.total), e2Int(got.net), e2Int(got.less), int64(types.MaxMoney))
	}
}

// TestExp_GSTNegative: GST on the bill is a money input the claim's GST
// figures are built from, so it is NonNegative like less advance: a negative
// value, down to the boundary -0.01, is refused on save with the kernel's
// text on gst_amount and its line, and nothing is written. 0.00 and a
// positive amount save; above the line amount is still refused.
func TestExp_GSTNegative(t *testing.T) {
	const msgGSTNegative = "Line 2: GST on the bill can't be negative."
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	for _, gst := range []string{"-1.00", "-0.01"} {
		var err error
		e.WantRefused(t, msgGSTNegative, func() error {
			_, err = e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil,
				V{"amount": "1500.00"}, V{"amount": "1500.00", "gst_amount": gst}))
			return err
		})
		e3WantField(t, err, 2, "gst_amount", msgGSTNegative)
		c := e.DraftClaim(t, deepak, nil, V{"amount": "1500.00"}, V{"amount": "1500.00"})
		e.WantRefused(t, msgGSTNegative, func() error {
			_, err = e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil, nil, V{"gst_amount": gst})
			return err
		})
		e3WantField(t, err, 2, "gst_amount", msgGSTNegative)
	}

	for _, gst := range []struct {
		value string
		paise int64
	}{{"0.00", 0}, {"160.71", 16071}} {
		res := e2Must(t, "GST "+gst.value)(e.Create(deepak, expense.ClaimDocType,
			e.ClaimInput(t, deepak, nil, V{"amount": "1500.00"}, V{"amount": "1500.00", "gst_amount": gst.value})))
		if l := e2ReadLine(t, e, res.ID, 2); l.gstAmount == nil || *l.gstAmount != gst.paise {
			t.Errorf("GST %s: stored gst_amount = %v, want %d", gst.value, l.gstAmount, gst.paise)
		}
	}

	const msgAbove = "Line 2: GST can't be more than the amount."
	var err error
	e.WantRefused(t, msgAbove, func() error {
		_, err = e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil,
			V{"amount": "1500.00"}, V{"amount": "1500.00", "gst_amount": "1500.01"}))
		return err
	})
	e3WantField(t, err, 2, "gst_amount", msgAbove)
}
