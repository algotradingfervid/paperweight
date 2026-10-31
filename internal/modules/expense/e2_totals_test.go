package expense_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/types"
)

// TestExp_Totals runs through the public save path (no pure function is
// named): Validate computes total, net_payable and the period from the lines
// on every save, and an advance's amount from its lines.
func TestExp_Totals(t *testing.T) { // EXP-004
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	lines := []V{
		{"amount": "1250.00", "expense_date": "2026-10-20"},
		{"amount": "830.50", "expense_date": "2026-10-18"},
		{"amount": "2000.00", "expense_date": "2026-10-22"},
	}

	check := func(t *testing.T, id int64, total, net int64, from, to string) {
		t.Helper()
		c := e2ReadClaim(t, e, id)
		if e2Int(c.total) != total || e2Int(c.net) != net || e2Str(c.periodFrom) != from || e2Str(c.periodTo) != to {
			t.Errorf("claim: total %d, net_payable %d, period %s to %s; want %d, %d, %s to %s",
				e2Int(c.total), e2Int(c.net), e2Str(c.periodFrom), e2Str(c.periodTo), total, net, from, to)
		}
	}

	t.Run("no advance", func(t *testing.T) {
		c := e.DraftClaim(t, deepak, nil, lines...)
		check(t, c.ID, 408050, 408050, "2026-10-18", "2026-10-22")

		// Recomputed on the next save: line 2 becomes ₹1,000.00 on 2026-10-19.
		e2Must(t, "edit line 2")(e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil,
			nil, V{"amount": "1000.00", "expense_date": "2026-10-19"}))
		check(t, c.ID, 425000, 425000, "2026-10-19", "2026-10-22")
	})

	// Less advance ₹1,000.00 against a paid ₹5,000.00 advance.
	t.Run("less advance", func(t *testing.T) {
		adv := e.PaidAdvance(t, deepak, types.Money(500000))
		c := e.DraftClaim(t, deepak, nil, lines...)
		e.Against(t, deepak, c.ID, adv.ID, types.Money(100000))
		check(t, c.ID, 408050, 308050, "2026-10-18", "2026-10-22")
	})

	t.Run("advance amount", func(t *testing.T) {
		a := e.DraftAdvance(t, deepak, nil, V{"line_amount": "2000.00"}, V{"line_amount": "1500.50"})
		if got := e2ReadAdvance(t, e, a.ID); e2Int(got.amount) != 350050 {
			t.Errorf("advance amount = %d, want 350050 (the sum of its lines)", e2Int(got.amount))
		}
	})
}
