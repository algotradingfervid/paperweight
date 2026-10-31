package goodsreceipt_test

import (
	"maps"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// g5LockPO is the reference PO's lines on a PO ordered 01-09-2026, so GRNs
// dated in late September aren't before its order date.
func g5LockPO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"order_date": "01-09-2026"}),
		purchasetest.OrderLines(
			map[string]string{"qty": "1200", "price": "62.5"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
		))
}

// g5Dated creates a draft GRN as Suresh on po dated date (ISO) receiving
// 100 kg of line 1, with change merged over g2Header.
func g5Dated(t *testing.T, env *grntest.Env, po doc.Doc, date string, change map[string]string) int64 {
	t.Helper()
	h := g2Header(t, env, po)
	h["receipt_date"] = date
	maps.Copy(h, change)
	return g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "", "")).ID
}

// g5Cancel cancels GRN id as Ravi (Purchase Manager) at its stored version.
func g5Cancel(t *testing.T, env *grntest.Env, id int64) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), env.Ravi, goodsreceipt.DocType, id, doctype.ActionCancel,
		doc.ActionInput{Version: env.Load(t, goodsreceipt.DocType, id).Version, Reason: "Entered twice"})
}

// g5WantState asserts GRN id is stored in state want.
func g5WantState(t *testing.T, what string, env *grntest.Env, id int64, want doctype.State) {
	t.Helper()
	if got := env.Load(t, goodsreceipt.DocType, id).State; got != want {
		t.Errorf("%s: GRN %d is %s, want %s", what, id, got, want)
	}
}

// GRN-023 (GRN spec §5; PO spec §14.2; CD-234, CD-282 Q9, CD-142): the
// lock is off by default, so a GRN dated 30-09-2026 submits. Locked up to
// 30-09-2026, submitting a GRN dated 30-09-2026 is refused (422) with
// modkit's text naming it "Draft {id}", as a field error on receipt_date,
// joined with submitFields' errors in the same doc.Invalid; cancelling an
// approved one with that date is refused with the same text naming its
// number, as a whole-document refusal. Neither writes anything. A GRN dated
// 01-10-2026 submits and cancels. Once the lock is cleared the refused
// actions pass. The clock is 18:31 UTC on 23 October, just after IST
// midnight (24-10-2026 IST).
func TestGRN_PeriodLock(t *testing.T) {
	env := grntest.New(t)
	env.SetNow(t, time.Date(2026, 10, 23, 18, 31, 0, 0, time.UTC))
	po := g5LockPO(t, env)

	approved := g5Dated(t, env, po, "2026-09-30", nil)
	if _, err := g2Submit(t, env, env.Suresh, approved); err != nil {
		t.Fatalf("no lock: submit the GRN dated 30-09-2026: %v %+v", err, g1FieldErrors(err))
	}
	g5WantState(t, "no lock: GRN dated 30-09-2026", env, approved, doctype.StateApproved)
	number := env.Load(t, goodsreceipt.DocType, approved).Number

	onLock := g5Dated(t, env, po, "2026-09-30", nil)
	noChallan := g5Dated(t, env, po, "2026-09-30", map[string]string{"challan_no": ""})
	after := g5Dated(t, env, po, "2026-10-01", nil)

	env.Lock(t, types.Date{Year: 2026, Month: time.September, Day: 30})

	before := g5Facts(t, env)
	_, err := g2Submit(t, env, env.Suresh, onLock)
	g2WantInvalid(t, "locked: submit the GRN dated 30-09-2026", err,
		doctype.FieldError{Field: "receipt_date", Message: g5LockText(naming.Label("", onLock), "30-09-2026")})
	_, err = g2Submit(t, env, env.Suresh, noChallan)
	g2WantInvalid(t, "locked: submit the GRN dated 30-09-2026 with no challan no.", err,
		doctype.FieldError{Field: "receipt_date", Message: g5LockText(naming.Label("", noChallan), "30-09-2026")},
		doctype.FieldError{Field: "challan_no", Message: g2MsgChallanNo})
	_, err = g5Cancel(t, env, approved)
	g5WantRefusal(t, "locked: cancel the approved GRN dated 30-09-2026", err, g5LockText(number, "30-09-2026"))
	if got := g5Facts(t, env); got != before {
		t.Errorf("the refusals wrote:\nbefore %s\nafter  %s", before, got)
	}
	g5WantState(t, "locked: refused submit", env, onLock, doctype.StateDraft)
	g5WantState(t, "locked: refused cancel", env, approved, doctype.StateApproved)

	if _, err := g2Submit(t, env, env.Suresh, after); err != nil {
		t.Errorf("locked: submit the GRN dated 01-10-2026: %v %+v, want it approved", err, g1FieldErrors(err))
	}
	g5WantState(t, "locked: GRN dated 01-10-2026", env, after, doctype.StateApproved)
	if _, err := g5Cancel(t, env, after); err != nil {
		t.Errorf("locked: cancel the GRN dated 01-10-2026: %v, want it cancelled", err)
	}
	g5WantState(t, "locked: cancelled GRN dated 01-10-2026", env, after, doctype.StateCancelled)

	env.Unlock(t)
	if _, err := g2Submit(t, env, env.Suresh, onLock); err != nil {
		t.Errorf("unlocked: submit the GRN dated 30-09-2026: %v %+v, want it approved", err, g1FieldErrors(err))
	}
	g5WantState(t, "unlocked: GRN dated 30-09-2026", env, onLock, doctype.StateApproved)
	if _, err := g5Cancel(t, env, approved); err != nil {
		t.Errorf("unlocked: cancel the GRN dated 30-09-2026: %v, want it cancelled", err)
	}
	g5WantState(t, "unlocked: cancelled GRN dated 30-09-2026", env, approved, doctype.StateCancelled)
}

// GRN-024 (GRN spec §5; DOC-077; CD-94): a cancel the lock refuses rolls
// back whole. The approved GRN dated 15-09-2026 under the lock 30-09-2026
// stays approved at the same version; the PO's received_qty and
// receipt_status are unchanged; no audit, approval history, number or
// comment row is added. The clock is just after IST midnight.
func TestGRN_LockRollsBack(t *testing.T) {
	env := grntest.New(t)
	env.SetNow(t, time.Date(2026, 10, 23, 18, 31, 0, 0, time.UTC))
	po := g5LockPO(t, env)
	id := g5Dated(t, env, po, "2026-09-15", nil)
	if _, err := g2Submit(t, env, env.Suresh, id); err != nil {
		t.Fatalf("submit: %v %+v", err, g1FieldErrors(err))
	}
	grn := env.Load(t, goodsreceipt.DocType, id)
	env.Lock(t, types.Date{Year: 2026, Month: time.September, Day: 30})
	want := g3Want{received: []string{"100", "0"}, pending: []string{"1100", "50"}, status: "part_received"}
	g3CheckPO(t, "before the cancel", env, po.ID, want)

	before := g5Facts(t, env)
	_, err := g5Cancel(t, env, id)
	g5WantRefusal(t, "cancel the GRN dated 15-09-2026", err, g5LockText(grn.Number, "15-09-2026"))
	if got := g5Facts(t, env); got != before {
		t.Errorf("the refused cancel wrote:\nbefore %s\nafter  %s", before, got)
	}
	after := env.Load(t, goodsreceipt.DocType, id)
	if after.State != doctype.StateApproved || after.Version != grn.Version {
		t.Errorf("GRN after the refused cancel: %s v%d, want approved v%d", after.State, after.Version, grn.Version)
	}
	g3CheckPO(t, "after the refused cancel", env, po.ID, want)
}
