package expense_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// TestExp_DateRules runs through the public save path (no pure function is
// named): with the clock at 2026-10-24, a future claim date, a future line
// date and a line after the claim date are refused with nothing written; past
// dates pass; a future advance date is refused.
func TestExp_DateRules(t *testing.T) { // EXP-005
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	refusals := []struct {
		name, want string
		header     V
		lines      []V
	}{
		{"future claim date", "Claim date can't be in the future.", V{"claim_date": "2026-10-25"}, nil},
		{"future line date", "Line 1: Date can't be in the future.", nil, []V{{"expense_date": "2026-10-25"}}},
		{"future date on line 2", "Line 2: Date can't be in the future.", nil, []V{nil, {"expense_date": "2026-10-25"}}},
		{"line after the claim date", "Line 1: Date can't be after the claim date.",
			V{"claim_date": "2026-10-22"}, []V{{"expense_date": "2026-10-23"}}},
	}
	for _, c := range refusals {
		t.Run(c.name, func(t *testing.T) {
			e.WantRefused(t, c.want, func() error {
				_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, c.header, c.lines...))
				return err
			})
		})
	}

	t.Run("past dates pass", func(t *testing.T) {
		res := e2Must(t, "claim dated 2026-03-31 with a line of 2026-03-30")(e.Create(deepak, expense.ClaimDocType,
			e.ClaimInput(t, deepak, V{"claim_date": "2026-03-31"}, V{"expense_date": "2026-03-30"})))
		if c := e2ReadClaim(t, e, res.ID); e2Str(c.periodFrom) != "2026-03-30" {
			t.Errorf("past claim's period_from = %s, want 2026-03-30", e2Str(c.periodFrom))
		}
	})

	t.Run("future advance date", func(t *testing.T) {
		e.WantRefused(t, "Advance date can't be in the future.", func() error {
			_, err := e.Create(deepak, expense.AdvanceDocType, e.AdvanceInput(t, deepak, V{"advance_date": "2026-10-25"}))
			return err
		})
	})
}
