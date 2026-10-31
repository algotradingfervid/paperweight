package expense_test

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// E4: advance lifecycle and settlement (spec §3.1, §4 Settlement, §5),
// EXP-024 to EXP-030. Clock 2026-10-24 10:30 IST. Every advance a claim sets
// off is marked as paid (CD-271 G1) unless the case says otherwise.

// TestAdv_Lifecycle: request, approve, number (EXP-024).
func TestAdv_Lifecycle(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)

	adv := e.DraftAdvance(t, deepak, expensetest.Values{"purpose": "Pune Metro site visit"}, expensetest.Values{"line_amount": "10000.00"})
	if res := e6Do(t, e, deepak, expense.AdvanceDocType, adv.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit: state %q, want pending", res.State)
	}
	var eligible []types.UserID
	err := e.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		eligible, err = e.Approvals.EligibleApprovers(t.Context(), tx, expense.AdvanceDocType, adv.ID)
		return err
	})
	if err != nil || !slices.Contains(eligible, priya.ID) {
		t.Errorf("eligible approvers = %v (err %v), want priya (%d) among them", eligible, err, priya.ID)
	}

	res := e6Do(t, e, priya, expense.AdvanceDocType, adv.ID, doc.ActionApprove, "")
	if res.State != doc.StateApproved || res.Number != e4EA1 {
		t.Errorf("approve: state %q, number %q; want approved, %s", res.State, res.Number, e4EA1)
	}
	if a := e2ReadAdvance(t, e, adv.ID); e2Int(a.amount) != 1000000 {
		t.Errorf("amount = %d, want 1000000 (₹10,000.00)", e2Int(a.amount))
	}
	e4WantUsed(t, e, adv.ID, "after approve", 0, e4Rs(10000))
}

// TestAdv_ClaimBelowAdvance: ₹10,000 advance, ₹6,000 claim with less advance
// blank (EXP-025).
func TestAdv_ClaimBelowAdvance(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	e4WantUsed(t, e, adv.ID, "paid advance", 0, e4Rs(10000))

	c := e4Claim(t, e, deepak, "6000.00", e4On(adv.ID, ""))
	got := e2ReadClaim(t, e, c.ID)
	if got.less != nil || e2Int(got.total) != 600000 || e2Int(got.net) != 0 {
		t.Errorf("draft: less_advance %d, total %d, net_payable %d; want NULL (stored blank), 600000, 0 (computed)",
			e2Int(got.less), e2Int(got.total), e2Int(got.net))
	}

	e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
	if got := e2ReadClaim(t, e, c.ID); e2Int(got.less) != 600000 || e2Int(got.net) != 0 {
		t.Errorf("after submit: less_advance %d, net_payable %d; want 600000 written, 0", e2Int(got.less), e2Int(got.net))
	}
	e4WantUsed(t, e, adv.ID, "claim pending", 0, e4Rs(10000))

	v := e4Version(t, e, expense.AdvanceDocType, adv.ID)
	e6Do(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, "")
	e4WantUsed(t, e, adv.ID, "claim approved", e4Rs(6000), e4Rs(4000))
	if got := e4Version(t, e, expense.AdvanceDocType, adv.ID); got != v+1 {
		t.Errorf("advance version after the claim's approve = %d, want %d", got, v+1)
	}
	e4WantEffect(t, e, e4Effects(t, e, adv.ID), c.ID, "approve")
	if by := e4UpdatedBy(t, e, c.ID); by != priya.ID {
		t.Errorf("claim updated_by after approve = %d, want priya %d", by, priya.ID)
	}
}

// TestAdv_ClaimAboveAdvance: ₹10,000 advance, ₹12,000 claim (EXP-026).
func TestAdv_ClaimAboveAdvance(t *testing.T) {
	t.Run("blank defaults to the advance", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))

		c := e4Claim(t, e, deepak, "12000.00", e4On(adv.ID, ""))
		if got := e2ReadClaim(t, e, c.ID); got.less != nil || e2Int(got.net) != 200000 {
			t.Errorf("draft: less_advance %d, net_payable %d; want NULL, 200000 (₹2,000.00)", e2Int(got.less), e2Int(got.net))
		}
		e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		if got := e2ReadClaim(t, e, c.ID); e2Int(got.less) != 1000000 || e2Int(got.net) != 200000 {
			t.Errorf("after submit: less_advance %d, net_payable %d; want 1000000, 200000", e2Int(got.less), e2Int(got.net))
		}
		e6Do(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, "")
		e4WantUsed(t, e, adv.ID, "claim approved", e4Rs(10000), 0)
	})

	// The advance is picked before any line exists, so above the total is a
	// submit check (CD-264 6).
	t.Run("typed above the total", func(t *testing.T) {
		e := expensetest.New(t)
		deepak := e.P(t, e4Deepak)
		adv := e.PaidAdvance(t, deepak, e4Rs(20000))
		c := e4Claim(t, e, deepak, "12000.00", e4On(adv.ID, "12000.01"))
		if got := e2ReadClaim(t, e, c.ID); got.state != "draft" || e2Int(got.less) != 1200001 {
			t.Errorf("draft: state %q, less_advance %d; want draft, 1200001", got.state, e2Int(got.less))
		}
		e.WantRefused(t, e4MsgAboveTotal, func() error {
			_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
			return err
		})
		e4WantUsed(t, e, adv.ID, "after the refusal", 0, e4Rs(20000))
	})
}

// TestAdv_ConcurrentClaims: two claims approved at once can't overspend one
// advance (EXP-027). Non-admin approvers on two connections (CD-94, CD-213).
func TestAdv_ConcurrentClaims(t *testing.T) {
	e := expensetest.New(t)
	e.AddRoles(t, e4Meena, expense.RoleFallbackApprover)
	deepak := e.P(t, e4Deepak)
	approvers := []auth.Principal{e.P(t, e4Priya), e.P(t, e4Meena)}
	for _, p := range approvers {
		if slices.Contains(p.Roles, auth.RoleAdministrator) {
			t.Fatalf("%s is an Administrator; this case needs non-admin users (CD-213)", p.Name)
		}
	}
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	claims := make([]int64, 2)
	for i := range claims {
		c := e4Claim(t, e, deepak, "6000.00", e4On(adv.ID, "6000.00"))
		e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		claims[i] = c.ID
	}
	versions := []int{e4Version(t, e, expense.ClaimDocType, claims[0]), e4Version(t, e, expense.ClaimDocType, claims[1])}
	e4WantUsed(t, e, adv.ID, "both pending", 0, e4Rs(10000))

	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, p := range approvers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = e.Engine.Do(t.Context(), p, expense.ClaimDocType, claims[i], doc.ActionApprove, doc.ActionInput{Version: versions[i]})
		}()
	}
	close(start)
	wg.Wait()

	want := e4AboveLeft(e4Rs(4000), e4EA1)
	var won, refused int
	for i, err := range errs {
		switch {
		case err == nil:
			won++
			if s := e4State(t, e, expense.ClaimDocType, claims[i]); s != doc.StateApproved {
				t.Errorf("claim %d approved without error but stored %q", claims[i], s)
			}
		case e2Has(err, want):
			refused++
			if s, v := e4State(t, e, expense.ClaimDocType, claims[i]), e4Version(t, e, expense.ClaimDocType, claims[i]); s != doc.StatePending || v != versions[i] {
				t.Errorf("refused claim %d: state %q, version %d; want pending, %d", claims[i], s, v, versions[i])
			}
		default:
			t.Errorf("approve claim %d as %s: %v (messages %q), want nil or %q", claims[i], approvers[i].Name, err, e2Messages(err), want)
		}
	}
	if won != 1 || refused != 1 {
		t.Errorf("%d approved and %d refused, want 1 and 1", won, refused)
	}
	e4WantUsed(t, e, adv.ID, "after both approves", e4Rs(6000), e4Rs(4000))
}

// TestAdv_CancelRestores: cancelling a claim recomputes used (EXP-028).
func TestAdv_CancelRestores(t *testing.T) {
	t.Run("EXP-025's claim", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		c := e.ApprovedClaim(t, deepak, e4On(adv.ID, ""), expensetest.Values{"amount": "6000.00"})
		e4WantUsed(t, e, adv.ID, "claim approved", e4Rs(6000), e4Rs(4000))
		if left := e4LeftOn(t, e, deepak, adv.ID); left != e4Rs(4000) {
			t.Errorf("LeftOn after approve = ₹%s, want ₹4,000.00", types.FormatMoney(left))
		}

		v := e4Version(t, e, expense.AdvanceDocType, adv.ID)
		e6Do(t, e, meena, expense.ClaimDocType, c.ID, doc.ActionCancel, "Booked twice.")
		e4WantUsed(t, e, adv.ID, "claim cancelled", 0, e4Rs(10000))
		if left := e4LeftOn(t, e, meena, adv.ID); left != e4Rs(10000) {
			t.Errorf("LeftOn after cancel = ₹%s, want ₹10,000.00", types.FormatMoney(left))
		}
		if got := e4Version(t, e, expense.AdvanceDocType, adv.ID); got != v+1 {
			t.Errorf("advance version after the claim's cancel = %d, want %d", got, v+1)
		}
		e4WantEffect(t, e, e4Effects(t, e, adv.ID), c.ID, "cancel")
	})

	// Recomputed from the remaining approved and closed claims, never
	// decremented (D-DOC-13): a closed claim still counts.
	t.Run("the other claims still count", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		closed := e.ApprovedClaim(t, deepak, e4On(adv.ID, "1000.00"), expensetest.Values{"amount": "1000.00"})
		e6Do(t, e, meena, expense.ClaimDocType, closed.ID, doc.ActionClose, "Settled in full.")
		kept := e.ApprovedClaim(t, deepak, e4On(adv.ID, "1500.50"), expensetest.Values{"amount": "1500.50"})
		c := e.ApprovedClaim(t, deepak, e4On(adv.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})
		e4WantUsed(t, e, adv.ID, "three claims", types.Money(850050), types.Money(149950))

		e6Do(t, e, meena, expense.ClaimDocType, c.ID, doc.ActionCancel, "Booked twice.")
		e4WantUsed(t, e, adv.ID, "one cancelled", types.Money(250050), types.Money(749950))
		if s := e4State(t, e, expense.ClaimDocType, kept.ID); s != doc.StateApproved {
			t.Errorf("the other claim is %q, want approved", s)
		}
	})
}

// TestAdv_CancelBlocked: an advance's cancel is blocked by a live claim, then
// by the paid mark (EXP-029, CD-281 b).
func TestAdv_CancelBlocked(t *testing.T) {
	e := expensetest.New(t)
	deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	c := e.ApprovedClaim(t, deepak, e4On(adv.ID, ""), expensetest.Values{"amount": "6000.00"})
	if n := e4Number(t, e, expense.ClaimDocType, c.ID); n != e4EX1 {
		t.Fatalf("claim number %q, want %s", n, e4EX1)
	}
	e4WantUsed(t, e, adv.ID, "claim approved", e4Rs(6000), e4Rs(4000))

	e.WantRefused(t, fmt.Sprintf(e4FmtBlocked, e4EA1, e4EX1, e4EX1), func() error {
		_, err := e6Try(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, "Trip called off.")
		return err
	})
	if s := e4State(t, e, expense.AdvanceDocType, adv.ID); s != doc.StateApproved {
		t.Errorf("advance after the refusal: %q, want approved", s)
	}

	e6Do(t, e, meena, expense.ClaimDocType, c.ID, doc.ActionCancel, "Booked twice.")
	e4WantUsed(t, e, adv.ID, "claim cancelled", 0, e4Rs(10000))
	e.WantRefused(t, e4MsgPaidCancel, func() error {
		_, err := e6Try(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, "Trip called off.")
		return err
	})

	if err := e.Service.UndoPaid(t.Context(), meena, adv.ID, e4Version(t, e, expense.AdvanceDocType, adv.ID)); err != nil {
		t.Fatalf("UndoPaid: %v", err)
	}
	if res := e6Do(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, "Trip called off."); res.State != doc.StateCancelled {
		t.Errorf("cancel after Undo paid: %q, want cancelled", res.State)
	}
	e4WantUsed(t, e, adv.ID, "advance cancelled", 0, e4Rs(10000))
}

// TestAdv_Sources: closed, another employee's, unreadable, no advance and a
// zero amount (EXP-030).
func TestAdv_Sources(t *testing.T) {
	t.Run("closed", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		e6Do(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionClose, "₹2,000 returned by NEFT 1234 on 30-11-2026")
		e.WantRefused(t, fmt.Sprintf(e4FmtSourceClosed, e4EA1), func() error {
			_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, e4On(adv.ID, "1000.00")))
			return err
		})
	})

	// Priya can read Deepak's advance (team), so the refusal names it.
	t.Run("another employee's, readable", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		e.WantRefused(t, fmt.Sprintf(e4FmtOtherEmployee, e4EA1), func() error {
			_, err := e.Create(priya, expense.ClaimDocType, e.ClaimInput(t, priya, e4On(adv.ID, "1000.00")))
			return err
		})
		// Also on a later save of Priya's own draft.
		c := e4Claim(t, e, priya, "1000.00", nil)
		e.WantRefused(t, fmt.Sprintf(e4FmtOtherEmployee, e4EA1), func() error {
			_, err := e.Edit(t, priya, expense.ClaimDocType, c.ID, e4On(adv.ID, "1000.00"))
			return err
		})
	})

	// Kavya can't read it: AUTH's link refusal, naming nothing (CD-240).
	t.Run("another employee's, unreadable", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, kavya := e.P(t, e4Deepak), e.P(t, e4Kavya)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		e.WantRefused(t, e4MsgLinkRefused, func() error {
			_, err := e.Create(kavya, expense.ClaimDocType, e.ClaimInput(t, kavya, e4On(adv.ID, "1000.00")))
			for _, m := range e2Messages(err) {
				expensetest.WantNotNamed(t, m, e4EA1)
			}
			return err
		})
	})

	t.Run("less advance with no advance", func(t *testing.T) {
		e := expensetest.New(t)
		deepak := e.P(t, e4Deepak)
		e.WantRefused(t, e4MsgChooseAdvance, func() error {
			_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, expensetest.Values{"less_advance": "1000.00"}))
			return err
		})
		c := e4Claim(t, e, deepak, "1000.00", nil)
		e.WantRefused(t, e4MsgChooseAdvance, func() error {
			_, err := e.Edit(t, deepak, expense.ClaimDocType, c.ID, expensetest.Values{"less_advance": "500.00"})
			return err
		})
	})

	t.Run("zero against an advance", func(t *testing.T) {
		e := expensetest.New(t)
		deepak := e.P(t, e4Deepak)
		adv := e.PaidAdvance(t, deepak, e4Rs(10000))
		c := e4Claim(t, e, deepak, "1000.00", e4On(adv.ID, "0.00"))
		if got := e2ReadClaim(t, e, c.ID); got.state != "draft" || e2Int(got.less) != 0 {
			t.Errorf("draft: state %q, less_advance %d; want draft, 0", got.state, e2Int(got.less))
		}
		e.WantRefused(t, fmt.Sprintf(e4FmtZero, e4EA1), func() error {
			_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
			return err
		})
	})
}
