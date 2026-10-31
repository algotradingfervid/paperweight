package expense_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// e6LockText is M0-7's period-lock refusal (modkit, PO spec §14.2) for a
// document dated 15-09-2026 or 30-09-2026 under the lock 30-09-2026 (CD-142:
// expected by the producing package's text).
func e6LockText(label, display, date string) string {
	return label + " " + display + " is dated " + date + ", which is locked. The period is locked up to 30-09-2026."
}

// EXP-023: the period lock is off by default; set to 30-09-2026 by admin, it
// refuses submit, approve and cancel of a claim and an advance dated on or
// before it, by the claim date or advance date, and writes nothing; close
// still passes (no hook runs). Expense spec §6, §8 (G11), PO spec §14.2,
// CD-234; plan MOD-EXP E6 Do 1.
func TestExp_PeriodLock(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, "deepak@vardhan.test")
	priya := e.P(t, "priya@vardhan.test") // Deepak's reporting manager
	meena := e.P(t, "meena@vardhan.test") // Director: cancel and close
	const (
		claimLabel   = "Expense Claim"
		advanceLabel = "Employee Advance"
		cancelWhy    = "Entered twice by mistake."
		closeWhy     = "Paid by NEFT 1234 on 30-09-2026."
	)
	claim := func(t *testing.T, date string) doc.Result {
		t.Helper()
		return e.DraftClaim(t, deepak, expensetest.Values{"claim_date": date}, expensetest.Values{"expense_date": date})
	}
	advance := func(t *testing.T, date string) doc.Result {
		t.Helper()
		return e.DraftAdvance(t, deepak, expensetest.Values{"advance_date": date})
	}
	wantState := func(t *testing.T, res doc.Result, want doc.State, what string) {
		t.Helper()
		if res.State != want {
			t.Errorf("%s: state %q, want %q", what, res.State, want)
		}
	}

	// No lock: documents dated 2026-03-31 (FY 25-26) submit, approve and cancel.
	for _, c := range []struct {
		dt  string
		new func(*testing.T, string) doc.Result
	}{{expense.ClaimDocType, claim}, {expense.AdvanceDocType, advance}} {
		id := c.new(t, "2026-03-31").ID
		wantState(t, e6Do(t, e, deepak, c.dt, id, doc.ActionSubmit, ""), doc.StatePending, "no lock: submit "+c.dt)
		wantState(t, e6Do(t, e, priya, c.dt, id, doc.ActionApprove, ""), doc.StateApproved, "no lock: approve "+c.dt)
		wantState(t, e6Do(t, e, meena, c.dt, id, doc.ActionCancel, cancelWhy), doc.StateCancelled, "no lock: cancel "+c.dt)
	}

	// Documents in each state, made before the lock is set.
	claimDraft := claim(t, "2026-09-15").ID
	claimOnLock := claim(t, "2026-09-30").ID // on the lock date: locked too
	claimPending := claim(t, "2026-09-15").ID
	e6Do(t, e, deepak, expense.ClaimDocType, claimPending, doc.ActionSubmit, "")
	claimApproved := claim(t, "2026-09-15").ID
	claimApprovedNo := e6Approve(t, e, deepak, priya, expense.ClaimDocType, claimApproved)
	claimToClose := claim(t, "2026-09-15").ID
	e6Approve(t, e, deepak, priya, expense.ClaimDocType, claimToClose)
	// September bills on an October claim: the lock follows claim_date (G11).
	claimOctober := e.DraftClaim(t, deepak, expensetest.Values{"claim_date": "2026-10-01"},
		expensetest.Values{"expense_date": "2026-09-20"}).ID

	advanceDraft := advance(t, "2026-09-15").ID
	advancePending := advance(t, "2026-09-15").ID
	e6Do(t, e, deepak, expense.AdvanceDocType, advancePending, doc.ActionSubmit, "")
	advanceApproved := advance(t, "2026-09-15").ID
	advanceApprovedNo := e6Approve(t, e, deepak, priya, expense.AdvanceDocType, advanceApproved)
	advanceToClose := advance(t, "2026-09-15").ID
	e6Approve(t, e, deepak, priya, expense.AdvanceDocType, advanceToClose)

	e6SetLock(t, e, "2026-09-30")

	for _, c := range []struct {
		name   string
		by     string
		dt     string
		id     int64
		action doc.Action
		reason string
		want   string
	}{
		{"claim_submit", "deepak", expense.ClaimDocType, claimDraft, doc.ActionSubmit, "",
			e6LockText(claimLabel, naming.Label("", claimDraft), "15-09-2026")},
		{"claim_submit_on_the_lock_date", "deepak", expense.ClaimDocType, claimOnLock, doc.ActionSubmit, "",
			e6LockText(claimLabel, naming.Label("", claimOnLock), "30-09-2026")},
		{"claim_approve", "priya", expense.ClaimDocType, claimPending, doc.ActionApprove, "",
			e6LockText(claimLabel, naming.Label("", claimPending), "15-09-2026")},
		{"claim_cancel", "meena", expense.ClaimDocType, claimApproved, doc.ActionCancel, cancelWhy,
			e6LockText(claimLabel, claimApprovedNo, "15-09-2026")},
		{"advance_submit", "deepak", expense.AdvanceDocType, advanceDraft, doc.ActionSubmit, "",
			e6LockText(advanceLabel, naming.Label("", advanceDraft), "15-09-2026")},
		{"advance_approve", "priya", expense.AdvanceDocType, advancePending, doc.ActionApprove, "",
			e6LockText(advanceLabel, naming.Label("", advancePending), "15-09-2026")},
		{"advance_cancel", "meena", expense.AdvanceDocType, advanceApproved, doc.ActionCancel, cancelWhy,
			e6LockText(advanceLabel, advanceApprovedNo, "15-09-2026")},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := e.P(t, c.by+"@vardhan.test")
			e.WantRefused(t, c.want, func() error {
				_, err := e6Try(t, e, p, c.dt, c.id, c.action, c.reason)
				return err
			})
		})
	}

	// Close has no hook, so the lock doesn't stop it.
	wantState(t, e6Do(t, e, meena, expense.ClaimDocType, claimToClose, doc.ActionClose, closeWhy), doc.StateClosed,
		"locked: close the claim dated 2026-09-15")
	wantState(t, e6Do(t, e, meena, expense.AdvanceDocType, advanceToClose, doc.ActionClose, closeWhy), doc.StateClosed,
		"locked: close the advance dated 2026-09-15")

	// After the lock date: submits.
	wantState(t, e6Do(t, e, deepak, expense.ClaimDocType, claimOctober, doc.ActionSubmit, ""), doc.StatePending,
		"locked: submit the claim dated 2026-10-01 with a line dated 2026-09-20")
}
