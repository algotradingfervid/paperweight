package expense_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// E4: which advances a claim may set off, and the re-checks at submit and
// approve (spec §4, §5; CD-271 G1), EXP-043 to EXP-049 and EXP-055.

// TestAdv_FullyUsedNotOffered (EXP-043).
func TestAdv_FullyUsedNotOffered(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e4Deepak)
	emp := e4Employee(t, e, "EMP-007")
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	if ids, _ := e4Choices(t, e, deepak, emp); !slices.Equal(ids, []int64{adv.ID}) {
		t.Errorf("choices with ₹10,000.00 left = %v, want [%d]", ids, adv.ID)
	}
	e.ApprovedClaim(t, deepak, e4On(adv.ID, "10000.00"), expensetest.Values{"amount": "10000.00"})
	e4WantUsed(t, e, adv.ID, "fully used", e4Rs(10000), 0)

	if ids, printed := e4Choices(t, e, deepak, emp); slices.Contains(ids, adv.ID) {
		t.Errorf("choices = %s, want the fully used advance %d left out", printed, adv.ID)
	}

	c := e4Claim(t, e, deepak, "1000.00", e4On(adv.ID, "100.00"))
	e.WantRefused(t, e4AboveLeft(0, e4EA1), func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})
	e4WantUsed(t, e, adv.ID, "after the refusal", e4Rs(10000), 0)
}

// TestAdv_NotApprovedRefused: draft, pending, rejected and cancelled advances
// are refused with DOC's source text for the state, and none is offered
// (EXP-044). An advance is numbered at approval, so draft, pending and
// rejected ones are named by their `Draft {id}` label (naming.Label).
func TestAdv_NotApprovedRefused(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, meena := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Meena)
	emp := e4Employee(t, e, "EMP-007")

	cancelled := e.ApprovedAdvance(t, deepak, e4Rs(5000)) // not paid, so it can be cancelled
	e6Do(t, e, meena, expense.AdvanceDocType, cancelled.ID, doc.ActionCancel, "Trip called off.")
	draft := e.DraftAdvance(t, deepak, nil)
	pending := e.DraftAdvance(t, deepak, nil)
	e6Do(t, e, deepak, expense.AdvanceDocType, pending.ID, doc.ActionSubmit, "")
	rejected := e.DraftAdvance(t, deepak, nil)
	e6Do(t, e, deepak, expense.AdvanceDocType, rejected.ID, doc.ActionSubmit, "")
	e6Do(t, e, priya, expense.AdvanceDocType, rejected.ID, doc.ActionReject, "Not needed for this trip.")

	label := func(id int64) string { return naming.Label(e4Number(t, e, expense.AdvanceDocType, id), id) }
	if l := label(cancelled.ID); l != e4EA1 {
		t.Fatalf("cancelled advance label %q, want %s", l, e4EA1)
	}
	for _, c := range []struct {
		name string
		id   int64
		want string
	}{
		{"draft", draft.ID, fmt.Sprintf(e4FmtSourceNotApproved, label(draft.ID))},
		{"pending", pending.ID, fmt.Sprintf(e4FmtSourcePending, label(pending.ID))},
		{"rejected", rejected.ID, fmt.Sprintf(e4FmtSourceNotApproved, label(rejected.ID))},
		{"cancelled", cancelled.ID, fmt.Sprintf(e4FmtSourceCancelled, e4EA1)},
	} {
		t.Run(c.name, func(t *testing.T) {
			e.WantRefused(t, c.want, func() error {
				_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, e4On(c.id, "1000.00")))
				return err
			})
		})
	}

	if ids, printed := e4Choices(t, e, deepak, emp); len(ids) != 0 {
		t.Errorf("choices = %s, want none (no advance is approved)", printed)
	}
}

// TestAdv_LeftForOneClaim (EXP-045).
func TestAdv_LeftForOneClaim(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e4Deepak)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	e.ApprovedClaim(t, deepak, e4On(adv.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})
	e4WantUsed(t, e, adv.ID, "₹6,000.00 used", e4Rs(6000), e4Rs(4000))
	if left := e4LeftOn(t, e, deepak, adv.ID); left != e4Rs(4000) {
		t.Errorf("LeftOn = %d, want 400000 (₹4,000.00)", left)
	}

	c := e4Claim(t, e, deepak, "8000.00", e4On(adv.ID, "5000.00"))
	e.WantRefused(t, e4AboveLeft(e4Rs(4000), e4EA1), func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})
	e4WantUsed(t, e, adv.ID, "after the refusal", e4Rs(6000), e4Rs(4000))

	e2Must(t, "set ₹4,000.00")(e.Edit(t, deepak, expense.ClaimDocType, c.ID, expensetest.Values{"less_advance": "4000.00"}))
	if res := e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Errorf("submit with ₹4,000.00: %q, want pending", res.State)
	}
	if got := e2ReadClaim(t, e, c.ID); e2Int(got.less) != 400000 || e2Int(got.net) != 400000 {
		t.Errorf("submitted: less_advance %d, net_payable %d; want 400000, 400000", e2Int(got.less), e2Int(got.net))
	}
	e4WantUsed(t, e, adv.ID, "pending claims don't count", e4Rs(6000), e4Rs(4000))
}

// TestAdv_ClosedWhileClaimPending (EXP-046). Close has no hook, so the
// pending claim's approve meets DOC-017's closed text.
func TestAdv_ClosedWhileClaimPending(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, meena := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Meena)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	e.ApprovedClaim(t, deepak, e4On(adv.ID, "2000.00"), expensetest.Values{"amount": "2000.00"})
	c := e4Claim(t, e, deepak, "3000.00", e4On(adv.ID, "3000.00"))
	e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
	e4WantUsed(t, e, adv.ID, "one approved, one pending", e4Rs(2000), e4Rs(8000))

	e6Do(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionClose, "₹8,000 returned by NEFT 1234 on 30-11-2026")
	e4WantUsed(t, e, adv.ID, "advance closed", e4Rs(2000), e4Rs(8000))

	e.WantRefused(t, fmt.Sprintf(e4FmtSourceClosed, e4EA1), func() error {
		_, err := e6Try(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, "")
		return err
	})
	if s := e4State(t, e, expense.ClaimDocType, c.ID); s != doc.StatePending {
		t.Errorf("claim after the refusal: %q, want pending", s)
	}

	if res := e6Do(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionReject, "The advance is closed."); res.State != doc.StateRejected {
		t.Errorf("reject: %q, want rejected", res.State)
	}
	e4WantUsed(t, e, adv.ID, "claim rejected", e4Rs(2000), e4Rs(8000))
}

// TestAdv_DraftClaimDoesNotBlockCancel (EXP-047): DOC-052 counts pending,
// approved and closed documents only.
func TestAdv_DraftClaimDoesNotBlockCancel(t *testing.T) {
	e := expensetest.New(t)
	deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
	adv := e.ApprovedAdvance(t, deepak, e4Rs(10000)) // not marked as paid
	c := e4Claim(t, e, deepak, "1000.00", e4On(adv.ID, "1000.00"))
	e4WantUsed(t, e, adv.ID, "draft claim", 0, e4Rs(10000))

	if res := e6Do(t, e, meena, expense.AdvanceDocType, adv.ID, doc.ActionCancel, "Trip called off."); res.State != doc.StateCancelled {
		t.Fatalf("cancel the advance: %q, want cancelled", res.State)
	}
	e4WantUsed(t, e, adv.ID, "advance cancelled", 0, e4Rs(10000))

	e.WantRefused(t, fmt.Sprintf(e4FmtSourceCancelled, e4EA1), func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})
}

// TestAdv_RejectedClaimRechecks (EXP-048).
func TestAdv_RejectedClaimRechecks(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
	adv := e.PaidAdvance(t, deepak, e4Rs(10000))
	first := e4Claim(t, e, deepak, "6000.00", e4On(adv.ID, "6000.00"))
	e6Do(t, e, deepak, expense.ClaimDocType, first.ID, doc.ActionSubmit, "")
	e4WantUsed(t, e, adv.ID, "first pending", 0, e4Rs(10000))

	e6Do(t, e, priya, expense.ClaimDocType, first.ID, doc.ActionReject, "Attach the hotel bill.")
	e4WantUsed(t, e, adv.ID, "first rejected", 0, e4Rs(10000))

	e.ApprovedClaim(t, deepak, e4On(adv.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})
	e4WantUsed(t, e, adv.ID, "second approved", e4Rs(6000), e4Rs(4000))

	e6Do(t, e, deepak, expense.ClaimDocType, first.ID, doc.ActionRevise, "")
	e.WantRefused(t, e4AboveLeft(e4Rs(4000), e4EA1), func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, first.ID, doc.ActionSubmit, "")
		return err
	})
	e4WantUsed(t, e, adv.ID, "after the refusal", e4Rs(6000), e4Rs(4000))
}

// TestAdv_AmendKeepsAdvance (EXP-049).
func TestAdv_AmendKeepsAdvance(t *testing.T) {
	// cancelledAmended is an approved ₹6,000.00 claim against a paid ₹10,000.00
	// advance, cancelled and amended; it returns the advance and the new draft.
	cancelledAmended := func(t *testing.T, e *expensetest.Env) (adv, amended int64) {
		t.Helper()
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		a := e.PaidAdvance(t, deepak, e4Rs(10000))
		c := e.ApprovedClaim(t, deepak, e4On(a.ID, "6000.00"), expensetest.Values{"amount": "6000.00"})
		e4WantUsed(t, e, a.ID, "claim approved", e4Rs(6000), e4Rs(4000))
		e6Do(t, e, meena, expense.ClaimDocType, c.ID, doc.ActionCancel, "Wrong dates.")
		e4WantUsed(t, e, a.ID, "claim cancelled", 0, e4Rs(10000))
		res := e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionAmend, "")
		rec := e6Load(t, e, expense.ClaimDocType, res.ID)
		if rec.Values["advance_id"] != e4ID(a.ID) || rec.Values["less_advance"] != "6000.00" {
			t.Errorf("amended draft: advance_id %q, less_advance %q; want %q, 6000.00",
				rec.Values["advance_id"], rec.Values["less_advance"], e4ID(a.ID))
		}
		return a.ID, res.ID
	}

	t.Run("submits", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, priya := e.P(t, e4Deepak), e.P(t, e4Priya)
		adv, c := cancelledAmended(t, e)
		if res := e6Do(t, e, deepak, expense.ClaimDocType, c, doc.ActionSubmit, ""); res.State != doc.StatePending {
			t.Fatalf("submit the amended draft: %q, want pending", res.State)
		}
		e6Do(t, e, priya, expense.ClaimDocType, c, doc.ActionApprove, "")
		e4WantUsed(t, e, adv, "amended claim approved", e4Rs(6000), e4Rs(4000))
	})

	// The engine accepts a closed source an amendment's original held
	// (CD-272 Gap 2), so this refusal is the claim's own advance check.
	t.Run("advance closed first", func(t *testing.T) {
		e := expensetest.New(t)
		deepak, meena := e.P(t, e4Deepak), e.P(t, e4Meena)
		adv, c := cancelledAmended(t, e)
		e6Do(t, e, meena, expense.AdvanceDocType, adv, doc.ActionClose, "₹10,000 returned by NEFT 1234 on 30-11-2026")
		e.WantRefused(t, fmt.Sprintf(e4FmtSourceClosed, e4EA1), func() error {
			_, err := e6Try(t, e, deepak, expense.ClaimDocType, c, doc.ActionSubmit, "")
			return err
		})
	})
}

// TestAdv_OnlyPaidSetOff (EXP-055, CD-271 G1).
func TestAdv_OnlyPaidSetOff(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya, kavya := e.P(t, e4Deepak), e.P(t, e4Priya), e.P(t, e4Kavya)
	emp := e4Employee(t, e, "EMP-007")
	adv := e.ApprovedAdvance(t, deepak, e4Rs(10000))
	kavyas := e.PaidAdvance(t, kavya, e4Rs(3000)) // another employee's paid advance: never Deepak's choice
	kavyaNumber := e4Number(t, e, expense.AdvanceDocType, kavyas.ID)

	if ids, printed := e4Choices(t, e, deepak, emp); len(ids) != 0 {
		t.Errorf("choices before paid = %s, want none (the advance isn't paid; Kavya's isn't Deepak's)", printed)
	}

	c := e4Claim(t, e, deepak, "1000.00", e4On(adv.ID, "1000.00"))
	e.WantRefused(t, fmt.Sprintf(e4FmtNotPaid, e4EA1), func() error {
		_, err := e6Try(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return err
	})

	e4Paid(t, e, adv.ID)
	ids, printed := e4Choices(t, e, deepak, emp)
	if !slices.Equal(ids, []int64{adv.ID}) {
		t.Errorf("choices after paid = %s, want only advance %d", printed, adv.ID)
	}
	expensetest.WantNotNamed(t, printed, kavyaNumber)

	// Kavya can't read Deepak's advances: none is offered, none is named.
	kIDs, kPrinted := e4Choices(t, e, kavya, emp)
	if len(kIDs) != 0 {
		t.Errorf("Kavya's choices for Deepak = %s, want none", kPrinted)
	}
	expensetest.WantNotNamed(t, kPrinted, e4EA1)

	if res := e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit after paid: %q, want pending", res.State)
	}
	if res := e6Do(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, ""); res.State != doc.StateApproved {
		t.Errorf("approve: %q, want approved", res.State)
	}
	e4WantUsed(t, e, adv.ID, "claim approved", e4Rs(1000), e4Rs(9000))
}
