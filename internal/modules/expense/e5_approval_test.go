package expense_test

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The E5 approval cases run on expensetest.Env: the real engine, the real
// auth.Service and APR's real service, with both approval rules (₹0,
// reporting manager, fallback role Fallback Approver). No fixture user holds
// Fallback Approver; each case gives it in its own setup (spec §6, CD-238).

// EXP-002: an Administrator enters a claim for Deepak; the claimant and the
// creator are both owners, so neither may approve it, and Priya (Deepak's
// manager) is the only eligible approver.
func TestExp_AdminEntersForOther(t *testing.T) {
	e := expensetest.New(t)
	admin, deepak, priya := e.P(t, e5Admin), e.P(t, e5Deepak), e.P(t, e5Priya)

	res := e.DraftClaim(t, admin, expensetest.Values{"employee_id": strconv.FormatInt(e5Emp(t, e, "EMP-007"), 10)})
	res = e5MustSubmit(t, e, admin, expense.ClaimDocType, res)
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StatePending, "")
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Priya)

	r := e5Request(t, e, expense.ClaimDocType, res.ID)
	if r.Claimant != deepak.ID || r.Creator != admin.ID || r.ManagerUser != priya.ID {
		t.Errorf("request claimant, creator, manager = %d, %d, %d; want Deepak %d, admin %d, Priya %d",
			r.Claimant, r.Creator, r.ManagerUser, deepak.ID, admin.ID, priya.ID)
	}

	refused := e5NotEligible("Priya Shah", fmt.Sprintf("Draft %d", res.ID), "₹1,000.00")
	for _, p := range []string{e5Deepak, e5Admin} {
		e.WantRefused(t, refused, func() error {
			_, err := e5Do(e, e.P(t, p), expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
			return err
		})
	}

	res = e5Must(t, e, priya, expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateApproved, "EX/26-27/00001")
}

// EXP-015: a claim goes to the reporting manager, has no number while
// pending and is numbered on approve. The approval amount is the claim's
// total (not net payable) and the number's FY is the claim date's (spec §7;
// the Amount and Date hooks E1 made real).
func TestExp_ManagerApproves(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e5Deepak), e.P(t, e5Priya)

	res := e.DraftClaim(t, deepak, nil, nil, expensetest.Values{"amount": "1500.00"})
	res = e5MustSubmit(t, e, deepak, expense.ClaimDocType, res)
	if res.Number != "" {
		t.Errorf("pending claim number = %q, want none", res.Number)
	}
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StatePending, "")
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Priya)
	if r := e5Request(t, e, expense.ClaimDocType, res.ID); r.Amount != types.Money(250000) || r.ManagerUser != priya.ID {
		t.Errorf("request amount, manager = %s, %d; want ₹2,500.00 (the total), Priya %d", types.FormatINR(r.Amount), r.ManagerUser, priya.ID)
	}

	res = e5Must(t, e, priya, expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	if res.State != doc.StateApproved || res.Number != "EX/26-27/00001" {
		t.Errorf("approve = %s %q, want approved EX/26-27/00001", res.State, res.Number)
	}
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateApproved, "EX/26-27/00001")

	t.Run("the claim date sets the FY", func(t *testing.T) {
		old := e.DraftClaim(t, deepak, expensetest.Values{"claim_date": "2026-03-31"}, expensetest.Values{"expense_date": "2026-03-31"})
		old = e5MustSubmit(t, e, deepak, expense.ClaimDocType, old)
		old = e5Must(t, e, priya, expense.ClaimDocType, old.ID, doc.ActionApprove, doc.ActionInput{Version: old.Version})
		if old.Number != "EX/25-26/00001" {
			t.Errorf("claim dated 31-03-2026 numbered %q, want EX/25-26/00001", old.Number)
		}
	})

	t.Run("Amount is the total, not net payable; Date is the claim date", func(t *testing.T) {
		d := e.DraftClaim(t, deepak, expensetest.Values{"claim_date": "2026-10-20"}, expensetest.Values{"expense_date": "2026-10-20"})
		e5Exec(t, e, `UPDATE expense_claim SET total = 250000, less_advance = 150000, net_payable = 100000 WHERE id = ?`, d.ID)
		hooks := lookupDT(t, expense.ClaimDocType).Hooks
		var amount types.Money
		var date types.Date
		if err := e.DB.Read(context.Background(), func(tx *db.Tx) (err error) {
			if amount, err = hooks.Amount(context.Background(), tx, d.ID); err != nil {
				return err
			}
			date, err = hooks.Date(context.Background(), tx, d.ID)
			return err
		}); err != nil {
			t.Fatalf("Amount, Date: %v", err)
		}
		if amount != types.Money(250000) {
			t.Errorf("Amount = %s, want the total ₹2,500.00 (net payable is ₹1,000.00)", types.FormatINR(amount))
		}
		if date.ISO() != "2026-10-20" {
			t.Errorf("Date = %s, want the claim date 2026-10-20", date.ISO())
		}
	})
}

// EXP-016: with no reporting manager, or a manager whose employee has no
// user, the claim goes to the Fallback Approver holders, and Priya isn't
// eligible. On the real auth.Service.
func TestExp_Fallback(t *testing.T) {
	cases := []struct {
		name, fixture string
	}{
		{"manager cleared", `UPDATE employee SET manager_id = NULL WHERE code = 'EMP-007'`},
		{"manager's employee has no user", `UPDATE employee SET user_id = NULL WHERE code = 'EMP-004'`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := expensetest.New(t)
			e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
			e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
			e5Exec(t, e, c.fixture)
			deepak := e.P(t, e5Deepak)

			res := e5MustSubmit(t, e, deepak, expense.ClaimDocType, e.DraftClaim(t, deepak, nil))
			e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Meena, e5Kavya)
			if r := e5Request(t, e, expense.ClaimDocType, res.ID); r.ManagerUser != 0 || len(r.ApproverRoles) != 1 || r.ApproverRoles[0] != expense.RoleFallbackApprover {
				t.Errorf("request manager, roles = %d, %q; want none, [Fallback Approver]", r.ManagerUser, r.ApproverRoles)
			}

			res = e5Must(t, e, e.P(t, e5Kavya), expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
			e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateApproved, "EX/26-27/00001")
		})
	}
}

// EXP-019: reject needs APR's reason; a rejected claim is revised to draft,
// edited and submitted again to the same manager.
func TestExp_RejectRevise(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e5Deepak), e.P(t, e5Priya)
	food := strconv.FormatInt(e.F.ExpenseTypeIDs["FOOD"], 10)

	res := e.DraftClaim(t, deepak, nil,
		expensetest.Values{"description": "Taxi to Metro site"},
		expensetest.Values{"expense_type_id": food, "description": "Dinner", "amount": "600.00"})
	res = e5MustSubmit(t, e, deepak, expense.ClaimDocType, res)

	for _, blank := range []string{"", "   "} {
		e.WantRefused(t, e5ReasonRequired, func() error {
			_, err := e5Do(e, priya, expense.ClaimDocType, res.ID, doc.ActionReject, doc.ActionInput{Version: res.Version, Reason: blank})
			return err
		})
	}

	const why = "Line 2 is a personal meal"
	res = e5Must(t, e, priya, expense.ClaimDocType, res.ID, doc.ActionReject, doc.ActionInput{Version: res.Version, Reason: why})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateRejected, "")
	if r := e5Request(t, e, expense.ClaimDocType, res.ID); r.Outcome != approval.OutcomeRejected || r.Reason != why || r.DecidedBy != priya.ID {
		t.Errorf("request outcome, reason, by = %q, %q, %d; want rejected, %q, Priya %d", r.Outcome, r.Reason, r.DecidedBy, why, priya.ID)
	}

	res = e5Must(t, e, deepak, expense.ClaimDocType, res.ID, doc.ActionRevise, doc.ActionInput{Version: res.Version})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateDraft, "")

	rec := e5Load(t, e, expense.ClaimDocType, res.ID)
	if len(rec.Lines) != 2 {
		t.Fatalf("revised claim has %d lines, want 2", len(rec.Lines))
	}
	in := doc.ActionInput{Version: rec.Version, Values: expensetest.Values{"remarks": "Dinner reduced to my share"}}
	for i, l := range rec.Lines {
		l = expensetest.Values(l)
		if i == 1 {
			l["amount"], l["description"] = "350.00", "Dinner (my share)"
		}
		in.Lines = append(in.Lines, l)
	}
	res = e5Must(t, e, deepak, expense.ClaimDocType, res.ID, doc.ActionSave, in)

	res = e5MustSubmit(t, e, deepak, expense.ClaimDocType, res)
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Priya)
	if r := e5Request(t, e, expense.ClaimDocType, res.ID); r.Outcome != "" || r.Amount != types.Money(135000) || r.ManagerUser != priya.ID {
		t.Errorf("new request outcome, amount, manager = %q, %s, %d; want open, ₹1,350.00, Priya", r.Outcome, types.FormatINR(r.Amount), r.ManagerUser)
	}
}

// EXP-039: the Director's claim goes to the other Fallback Approver; the
// Director can't approve her own. On the real auth.Service.
func TestExp_DirectorClaim(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
	e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
	meena := e.P(t, e5Meena)

	res := e5MustSubmit(t, e, meena, expense.ClaimDocType, e.DraftClaim(t, meena, nil))
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Kavya)

	e.WantRefused(t, e5OwnClaimFallback, func() error {
		_, err := e5Do(e, meena, expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
		return err
	})
	res = e5Must(t, e, e.P(t, e5Kavya), expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateApproved, "EX/26-27/00001")
}

// EXP-040: a claim from the only active Fallback Approver is refused with
// APR's text and the draft stays as it was; a second holder lets the same
// draft submit. On the real auth.Service.
func TestExp_OnlyFallback(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
	meena := e.P(t, e5Meena)

	res := e.DraftClaim(t, meena, nil)
	e5SetComputed(t, e, expense.ClaimDocType, res.ID)
	e.WantRefused(t, e5NoOneElseClaim, func() error {
		_, err := e5Do(e, meena, expense.ClaimDocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
		return err
	})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateDraft, "")

	e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
	res = e5MustSubmit(t, e, meena, expense.ClaimDocType, res)
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Kavya)
}

// EXP-041: when the second holder is disabled, the Director's pending claim
// shows on Stuck approvals (APR-043) and Deepak's doesn't; a new holder
// takes it off and approves it. On the real auth.Service.
func TestExp_FallbackLeaves(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
	e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
	meena, deepak := e.P(t, e5Meena), e.P(t, e5Deepak)

	mine := e5MustSubmit(t, e, meena, expense.ClaimDocType, e.DraftClaim(t, meena, nil))
	theirs := e5MustSubmit(t, e, deepak, expense.ClaimDocType, e.DraftClaim(t, deepak, nil))
	e5WantEligible(t, e, expense.ClaimDocType, mine.ID, e5Kavya)
	if got := e5Stuck(t, e, expense.ClaimDocType); len(got) != 0 {
		t.Errorf("stuck before the disable = %v, want none", got)
	}

	if err := e.Auth.DisableUserByEmail(context.Background(), e5Kavya, "Left the company"); err != nil {
		t.Fatalf("disable kavya: %v", err)
	}
	e5WantEligible(t, e, expense.ClaimDocType, mine.ID)
	if got := e5Stuck(t, e, expense.ClaimDocType); len(got) != 1 || got[0] != mine.ID {
		t.Errorf("stuck after kavya is disabled = %v, want only Meena's claim %d (not Deepak's %d)", got, mine.ID, theirs.ID)
	}
	items, err := e.Approvals.Stuck(context.Background(), e.P(t, e5Admin))
	if err != nil {
		t.Fatalf("Stuck: %v", err)
	}
	for _, it := range items {
		if it.DocID == mine.ID && it.Message != "No active approver: needs Fallback Approver." {
			t.Errorf("stuck message = %q, want APR's %q", it.Message, "No active approver: needs Fallback Approver.")
		}
	}

	e.AddRoles(t, e5Ravi, expense.RoleFallbackApprover)
	if got := e5Stuck(t, e, expense.ClaimDocType); len(got) != 0 {
		t.Errorf("stuck after ravi gets the role = %v, want none", got)
	}
	e5WantEligible(t, e, expense.ClaimDocType, mine.ID, e5Ravi)
	mine = e5Must(t, e, e.P(t, e5Ravi), expense.ClaimDocType, mine.ID, doc.ActionApprove, doc.ActionInput{Version: mine.Version})
	e5WantState(t, e, expense.ClaimDocType, mine.ID, doctype.StateApproved, "EX/26-27/00001")
}

// EXP-050: the manager's user is disabled while a claim is pending; a
// Fallback Approver approves it. Before the disable both the manager and the
// holders were eligible. On the real auth.Service.
func TestExp_ManagerDisabledFallback(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
	e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
	deepak := e.P(t, e5Deepak)

	res := e5MustSubmit(t, e, deepak, expense.ClaimDocType, e.DraftClaim(t, deepak, nil))
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Priya, e5Meena, e5Kavya)

	if err := e.Auth.DisableUserByEmail(context.Background(), e5Priya, "Left the company"); err != nil {
		t.Fatalf("disable priya: %v", err)
	}
	e5WantEligible(t, e, expense.ClaimDocType, res.ID, e5Meena, e5Kavya)

	res = e5Must(t, e, e.P(t, e5Meena), expense.ClaimDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
	e5WantState(t, e, expense.ClaimDocType, res.ID, doctype.StateApproved, "EX/26-27/00001")
}

// EXP-051: the advance rule works like the claim's: the Director's advance
// goes to the other Fallback Approver, and with Meena the only holder the
// submit is refused with APR's text. The advance's Amount is its amount and
// its number's FY is the advance date's. On the real auth.Service.
func TestAdv_DirectorAdvance(t *testing.T) {
	t.Run("two holders", func(t *testing.T) {
		e := expensetest.New(t)
		e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
		e.AddRoles(t, e5Kavya, expense.RoleFallbackApprover)
		meena, kavya := e.P(t, e5Meena), e.P(t, e5Kavya)

		res := e5MustSubmit(t, e, meena, expense.AdvanceDocType, e.DraftAdvance(t, meena, nil))
		e5WantEligible(t, e, expense.AdvanceDocType, res.ID, e5Kavya)
		if r := e5Request(t, e, expense.AdvanceDocType, res.ID); r.Amount != types.Money(500000) {
			t.Errorf("request amount = %s, want the advance's ₹5,000.00", types.FormatINR(r.Amount))
		}
		e.WantRefused(t, "You can't approve your own Employee Advance. Ask another Fallback Approver to approve it.", func() error {
			_, err := e5Do(e, meena, expense.AdvanceDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
			return err
		})
		res = e5Must(t, e, kavya, expense.AdvanceDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
		e5WantState(t, e, expense.AdvanceDocType, res.ID, doctype.StateApproved, "EA/26-27/00001")

		old := e5MustSubmit(t, e, meena, expense.AdvanceDocType, e.DraftAdvance(t, meena, expensetest.Values{"advance_date": "2026-03-31"}))
		old = e5Must(t, e, kavya, expense.AdvanceDocType, old.ID, doc.ActionApprove, doc.ActionInput{Version: old.Version})
		if old.Number != "EA/25-26/00001" {
			t.Errorf("advance dated 31-03-2026 numbered %q, want EA/25-26/00001", old.Number)
		}
	})

	t.Run("only holder", func(t *testing.T) {
		e := expensetest.New(t)
		e.AddRoles(t, e5Meena, expense.RoleFallbackApprover)
		meena := e.P(t, e5Meena)

		res := e.DraftAdvance(t, meena, nil)
		e5SetComputed(t, e, expense.AdvanceDocType, res.ID)
		e.WantRefused(t, e5NoOneElseAdvance, func() error {
			_, err := e5Do(e, meena, expense.AdvanceDocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
			return err
		})
		e5WantState(t, e, expense.AdvanceDocType, res.ID, doctype.StateDraft, "")
	})
}

// EXP-053: Priya approves her report's claim, not her own; Ravi, her
// manager, approves hers, and her inbox lists Deepak's claim only.
func TestExp_MiddleManagerOwnClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, ravi := e.P(t, e5Deepak), e.P(t, e5Priya), e.P(t, e5Ravi)

	his := e5MustSubmit(t, e, deepak, expense.ClaimDocType, e.DraftClaim(t, deepak, nil))
	hers := e5MustSubmit(t, e, priya, expense.ClaimDocType, e.DraftClaim(t, priya, nil))
	e5WantEligible(t, e, expense.ClaimDocType, hers.ID, e5Ravi)

	inbox, err := e.Approvals.Inbox(context.Background(), priya, types.Page{Number: 1, Size: 25})
	if err != nil {
		t.Fatalf("Inbox(priya): %v", err)
	}
	if len(inbox.Rows) != 1 || inbox.Rows[0].DocType != expense.ClaimDocType || inbox.Rows[0].DocID != his.ID {
		got := []string{}
		for _, r := range inbox.Rows {
			got = append(got, fmt.Sprintf("%s %d", r.DocType, r.DocID))
		}
		t.Errorf("Priya's inbox = %v, want only Deepak's claim %d (not her own %d)", got, his.ID, hers.ID)
	}

	e.WantRefused(t, e5NotEligible("Ravi Kulkarni", fmt.Sprintf("Draft %d", hers.ID), "₹1,000.00"), func() error {
		_, err := e5Do(e, priya, expense.ClaimDocType, hers.ID, doc.ActionApprove, doc.ActionInput{Version: hers.Version})
		return err
	})

	his = e5Must(t, e, priya, expense.ClaimDocType, his.ID, doc.ActionApprove, doc.ActionInput{Version: his.Version})
	e5WantState(t, e, expense.ClaimDocType, his.ID, doctype.StateApproved, "EX/26-27/00001")
	hers = e5Must(t, e, ravi, expense.ClaimDocType, hers.ID, doc.ActionApprove, doc.ActionInput{Version: hers.Version})
	e5WantState(t, e, expense.ClaimDocType, hers.ID, doctype.StateApproved, "EX/26-27/00002")
}
