package expense_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// TestAdv_NoStaleNetPayable (CD-302, for E4's settleOnSave): a step save
// (ActionInput.Only, CD-205/207) keeps Validate's writes when the failing
// field is outside its scope. When the advance can no longer be set off,
// net_payable is computed from the current inputs (the total) or cleared,
// never left at the figure the old advance gave, and never computed from
// another employee's advance.
func TestAdv_NoStaleNetPayable(t *testing.T) {
	// stepSave saves draft claim id as p with the header names in values as
	// its scope (Only), sending the stored lines unchanged. The settle check's text must be reported, as a refusal
	// or as a problem outside the scope; on a refusal nothing may be written.
	// It reports whether the write was kept.
	stepSave := func(t *testing.T, e *expensetest.Env, p auth.Principal, id int64, values expensetest.Values, want string) (kept bool) {
		t.Helper()
		only := make([]string, 0, len(values))
		for k := range values {
			only = append(only, k)
		}
		in := e.ClaimEdit(t, id, values, nil) // every stored line in full, unchanged: a save without lines drops them
		in.Only = only
		before := e.Fingerprint(t)
		res, err := e.Engine.Do(t.Context(), p, expense.ClaimDocType, id, doc.ActionSave, in)
		if err != nil {
			if !e2Has(err, want) {
				t.Errorf("step save %v: %v (messages %q), want %q", only, err, e2Messages(err), want)
			}
			if after := e.Fingerprint(t); after != before {
				t.Errorf("refused step save wrote:\nbefore %s\nafter  %s", before, after)
			}
			return false
		}
		if !slices.ContainsFunc(res.Problems, func(f doc.FieldError) bool { return f.Message == want }) {
			t.Errorf("step save %v: problems %+v, want one with %q", only, res.Problems, want)
		}
		return true
	}
	// wantNotStale fails when the claim's stored net_payable is anything but
	// NULL or its total.
	wantNotStale := func(t *testing.T, e *expensetest.Env, id int64) {
		t.Helper()
		c := e2ReadClaim(t, e, id)
		if c.net != nil && e2Int(c.net) != e2Int(c.total) {
			t.Errorf("net_payable %d with total %d: stale; want NULL or the total (CD-302)", e2Int(c.net), e2Int(c.total))
		}
	}

	// Step 1 drops the advance and posts only advance_id; the typed less
	// advance stays stored with no advance (its error is on less_advance,
	// outside the scope).
	t.Run("advance removed, less advance kept", func(t *testing.T) {
		e := expensetest.New(t)
		deepak := e.P(t, e4Deepak)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		c := e4Claim(t, e, deepak, "3000.00", e4On(adv.ID, "1000.00"))
		if got := e2ReadClaim(t, e, c.ID); e2Int(got.net) != 200000 {
			t.Fatalf("setup: net_payable %d, want 200000", e2Int(got.net))
		}
		if stepSave(t, e, deepak, c.ID, expensetest.Values{"advance_id": ""}, e4MsgChooseAdvance) {
			wantNotStale(t, e, c.ID)
		}
	})

	// The Administrator's step 1 changes only the employee, so Deepak's
	// advance (less advance blank, so computed from it) now belongs to
	// another employee: net_payable can't come from it any more.
	t.Run("employee changed, advance now another employee's", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, admin := e.P(t, e4Deepak), e.P(t, e2Admin)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		c := e4Claim(t, e, deepak, "3000.00", e4On(adv.ID, ""))
		if got := e2ReadClaim(t, e, c.ID); e2Int(got.net) != 0 {
			t.Errorf("setup: net_payable %d, want 0 (less advance computed from the advance)", e2Int(got.net))
		}
		if stepSave(t, e, admin, c.ID, expensetest.Values{"employee_id": e4ID(e4Employee(t, e, "EMP-004"))}, "EA/26-27/00001 belongs to another employee.") {
			wantNotStale(t, e, c.ID)
		}
	})
}
