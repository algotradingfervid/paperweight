package expense_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// TestExp_AmountPositive: a claim line or advance line of ₹0.00 or -₹1.00 is
// refused on a draft save with DOC's Positive text and nothing written;
// ₹0.01 saves and is the document's total (claim) or amount (advance).
func TestExp_AmountPositive(t *testing.T) { // EXP-042
	const msgPositive = "Line 1: Amount must be more than 0."
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	for _, amount := range []string{"0.00", "-1.00"} {
		t.Run("claim "+amount, func(t *testing.T) {
			e.WantRefused(t, msgPositive, func() error {
				_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, V{"amount": amount}))
				return err
			})
		})
		t.Run("advance "+amount, func(t *testing.T) {
			e.WantRefused(t, msgPositive, func() error {
				_, err := e.Create(deepak, expense.AdvanceDocType, e.AdvanceInput(t, deepak, nil, V{"line_amount": amount}))
				return err
			})
		})
	}

	c := e.DraftClaim(t, deepak, nil, V{"amount": "0.01"})
	if got := e2ReadClaim(t, e, c.ID); e2Int(got.total) != 1 || e2Int(got.net) != 1 {
		t.Errorf("claim of ₹0.01: total %d, net_payable %d; want 1, 1", e2Int(got.total), e2Int(got.net))
	}
	a := e.DraftAdvance(t, deepak, nil, V{"line_amount": "0.01"})
	if got := e2ReadAdvance(t, e, a.ID); e2Int(got.amount) != 1 {
		t.Errorf("advance of ₹0.01: amount %d, want 1", e2Int(got.amount))
	}
}
