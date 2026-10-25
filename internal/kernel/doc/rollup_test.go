package doc_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/types"
)

// ruCancelReason is DOC-012's cancel reason.
const ruCancelReason = "Wrong vehicle, goods returned at gate."

// ruOrderCancelReason is the order's cancel reason in DOC-054 (any reason of
// 3 to 500 characters).
const ruOrderCancelReason = "Supplier can't deliver the balance."

// ruOrder creates and auto-approves Priya's order (amount 0) with one
// MS-ANGLE-50 line per qty and returns it with its line ids in line order.
func ruOrder(t *testing.T, h *fixturetest.H, qtys ...string) (doc.Doc, []int64) {
	t.Helper()
	var lines []fixturetest.OL
	for _, q := range qtys {
		lines = append(lines, fixturetest.OL{Item: "MS-ANGLE-50", Qty: q, Rate: "62.5000"})
	}
	o := h.ApprovedOrder(t, lines...)
	if o.State != doc.StateApproved {
		t.Fatalf("setup: order State = %q, want approved", o.State)
	}
	var ids []int64
	for _, l := range o.Lines {
		ids = append(ids, l.ID)
	}
	return o, ids
}

// ruReceiptsNeedApproval makes APR require approval for receipts only.
func ruReceiptsNeedApproval(h *fixturetest.H) {
	h.Approvals.Needed = func(dt string, _ types.Money) bool { return dt == fixture.Receipt }
}

// ruSubmit creates Suresh's receipt with lines and submits it as Suresh. It
// returns the receipt as stored after the submit.
func ruSubmit(t *testing.T, h *fixturetest.H, lines ...fixturetest.RL) doc.Doc {
	t.Helper()
	r := h.NewReceipt(t, h.Suresh, lines...)
	h.MustDo(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
	return h.Load(t, fixture.Receipt, r.ID)
}

// ruApproved creates Suresh's receipt with lines and takes it to approved:
// submit by Suresh, then approve by approver when the submit left it pending.
func ruApproved(t *testing.T, h *fixturetest.H, approver auth.Principal, lines ...fixturetest.RL) doc.Doc {
	t.Helper()
	r := ruSubmit(t, h, lines...)
	if r.State == doc.StatePending {
		h.MustDo(t, t.Context(), approver, fixture.Receipt, r.ID, doc.ActionApprove, trIn(r.Version))
		r = h.Load(t, fixture.Receipt, r.ID)
	}
	if r.State != doc.StateApproved {
		t.Fatalf("setup: receipt %d State = %q, want approved", r.ID, r.State)
	}
	return r
}

// ruCancel cancels the stored document id by p with reason and returns the
// result.
func ruCancel(t *testing.T, h *fixturetest.H, p auth.Principal, dt string, id int64, reason string) doc.Result {
	t.Helper()
	d := h.Load(t, dt, id)
	return h.MustDo(t, t.Context(), p, dt, id, doc.ActionCancel, trIn(d.Version, reason))
}

// ruWantQty fails unless the order line's received_qty is want.
func ruWantQty(t *testing.T, h *fixturetest.H, orderLineID, want int64, when string) {
	t.Helper()
	if got := trReceivedQty(t, h, orderLineID); got != want {
		t.Errorf("%s: order line %d received_qty = %d, want %d", when, orderLineID, got, want)
	}
}

// ruEffects returns the order's "effect" audit rows, oldest first.
func ruEffects(t *testing.T, h *fixturetest.H, orderID int64) []audit.Entry {
	t.Helper()
	return trActions(h.Audit(t, fixture.Order, orderID), "effect")
}

// ruWantEffect fails unless e is an effect row by `by` caused by cause with
// exactly changes.
func ruWantEffect(t *testing.T, e audit.Entry, by types.UserID, cause audit.Ref, changes []audit.Change) {
	t.Helper()
	if e.Action != "effect" {
		t.Errorf("Action = %q, want effect", e.Action)
	}
	if e.By != by {
		t.Errorf("effect By = %d, want %d", e.By, by)
	}
	if e.CausedBy == nil {
		t.Errorf("effect CausedBy = nil, want %+v", cause)
	} else {
		fixturetest.Eq(t, *e.CausedBy, cause)
	}
	fixturetest.Eq(t, e.Changes, changes)
}

// DOC-006
func TestDoc_Submit_NoRule_AutoApproves(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	h.Approvals.Calls = nil
	assigned := h.Naming.Assigned

	res, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.State != doc.StateApproved || res.Number != "GR/26-27/00001" {
		t.Errorf("Result State %q Number %q, want approved GR/26-27/00001", res.State, res.Number)
	}
	if d := trState(t, h, fixture.Receipt, r.ID, doc.StateApproved, r.Version+1); d.Number != "GR/26-27/00001" {
		t.Errorf("Number = %q, want GR/26-27/00001", d.Number)
	}
	if n := h.Naming.Assigned - assigned; n != 1 {
		t.Errorf("Naming.Assign calls = %d, want 1", n)
	}
	ruWantQty(t, h, ol[0], 4000, "after submit")
	fixturetest.Eq(t, h.Approvals.Calls, []string{"APR.Evaluate", "APR.Open", "APR.Record:approve"})

	entries := h.Audit(t, fixture.Receipt, r.ID)
	subs := trActions(entries, "submit")
	if len(subs) != 1 {
		t.Fatalf("submit audit rows = %+v, want exactly one", subs)
	}
	if subs[0].From != string(doc.StateDraft) || subs[0].To != string(doc.StateApproved) {
		t.Errorf("submit row %s -> %s, want draft -> approved", subs[0].From, subs[0].To)
	}
	if c := trChange(t, subs[0], "auto_approved"); c.After != "true" {
		t.Errorf("auto_approved change After = %q, want \"true\"", c.After)
	}
	if a := trActions(entries, "approve"); len(a) != 0 {
		t.Errorf("approve audit rows = %+v, want none (the submit row is the only transition)", a)
	}
}

// DOC-008
func TestDoc_Approve_RevalidatesThenApplies(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	ruReceiptsNeedApproval(h)
	r := ruSubmit(t, h, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.State != doc.StatePending {
		t.Fatalf("setup: receipt State = %q, want pending_approval", r.State)
	}
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)

	res, err := h.Do(t, ctx, h.Ravi, fixture.Receipt, r.ID, doc.ActionApprove, trIn(r.Version))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	// AUTH.Can:read is DOC's visibility check, first in DOC-008's order.
	fixturetest.Eq(t, probe.Calls, []string{
		"AUTH.Can:read", "APR.CheckApprover", "Receipt.Validate", "Receipt.BeforeSubmit",
		"NAM.Assign", "Receipt.ApplyEffects", "APR.Record:approve",
	})
	if got, ok := probe.Seen["received_qty_at_apply"]; !ok || got != 0 {
		t.Errorf("Seen[received_qty_at_apply] = %d (set %v), want 0 (roll-up runs after ApplyEffects)", got, ok)
	}
	if res.State != doc.StateApproved || res.Number != "GR/26-27/00001" {
		t.Errorf("Result State %q Number %q, want approved GR/26-27/00001", res.State, res.Number)
	}
	if d := trState(t, h, fixture.Receipt, r.ID, doc.StateApproved, r.Version+1); d.Number != "GR/26-27/00001" {
		t.Errorf("Number = %q, want GR/26-27/00001", d.Number)
	}
	ruWantQty(t, h, ol[0], 4000, "after approve")
}

// DOC-012
func TestDoc_Cancel_ReversesEffects(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	r1 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	r2 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "8"})
	ruWantQty(t, h, ol[0], 12000, "R1 and R2 approved")

	at := time.Date(2026, 10, 24, 6, 15, 0, 0, time.UTC)
	h.Clock.SetTime(at)
	res := ruCancel(t, h, h.Admin, fixture.Receipt, r2.ID, ruCancelReason)
	if res.State != doc.StateCancelled {
		t.Errorf("cancel R2: Result State = %q, want cancelled", res.State)
	}
	trState(t, h, fixture.Receipt, r2.ID, doc.StateCancelled, r2.Version+1)
	ruWantQty(t, h, ol[0], 4000, "R2 cancelled")

	cancels := trActions(h.Audit(t, fixture.Receipt, r2.ID), "cancel")
	if len(cancels) != 1 {
		t.Fatalf("R2 cancel audit rows = %+v, want exactly one", cancels)
	}
	c := cancels[0]
	if c.From != string(doc.StateApproved) || c.To != string(doc.StateCancelled) {
		t.Errorf("cancel row %s -> %s, want approved -> cancelled", c.From, c.To)
	}
	if c.Reason != ruCancelReason {
		t.Errorf("cancel row Reason = %q, want %q", c.Reason, ruCancelReason)
	}
	if c.By != h.Admin.ID {
		t.Errorf("cancel row By = %d, want %d (admin)", c.By, h.Admin.ID)
	}
	if !c.At.Equal(at) {
		t.Errorf("cancel row At = %v, want %v", c.At, at)
	}

	ruCancel(t, h, h.Admin, fixture.Receipt, r1.ID, ruCancelReason)
	trState(t, h, fixture.Receipt, r1.ID, doc.StateCancelled, 0)
	ruWantQty(t, h, ol[0], 0, "R1 cancelled too")
}

// DOC-050
func TestDoc_RollUp_OnlyViaEffects(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	ruReceiptsNeedApproval(h)
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	ruWantQty(t, h, ol[0], 4000, "R1 approved")
	v := h.Load(t, fixture.Order, o.ID).Version

	r2 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "3"})
	if r2.Number != "GR/26-27/00002" {
		t.Errorf("R2 Number = %q, want GR/26-27/00002", r2.Number)
	}
	ruWantQty(t, h, ol[0], 7000, "R2 approved")
	if d := trState(t, h, fixture.Order, o.ID, doc.StateApproved, 0); d.Version != v+1 {
		t.Errorf("order Version = %d, want %d (one bump for R2's roll-up)", d.Version, v+1)
	}
	effects := ruEffects(t, h, o.ID)
	if len(effects) != 2 {
		t.Fatalf("order effect rows = %d, want 2 (R1, R2): %+v", len(effects), effects)
	}
	ruWantEffect(t, effects[1], h.Ravi.ID,
		audit.Ref{DocType: fixture.Receipt, DocID: r2.ID, Number: "GR/26-27/00002", Action: "approve"},
		[]audit.Change{{Field: "received_qty", LineNo: 1, LineID: ol[0], Before: "4000", After: "7000", Op: "set"}})

	// No user route sets received_qty: a save on a draft order drops it.
	dr := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	d := h.Load(t, fixture.Order, dr.ID)
	in := h.In(d)
	in.Values["remarks"] = "received_qty is not editable"
	in.Lines[0]["received_qty"] = "5.000"
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	ruWantQty(t, h, d.Lines[0].ID, 0, "save with received_qty on a draft order")
}

// DOC-054
func TestDoc_Cancel_AfterBlockersCancelled(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	if o.Number != "PO/26-27/00001" {
		t.Fatalf("setup: order Number = %q, want PO/26-27/00001", o.Number)
	}
	r := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.Number != "GR/26-27/00001" {
		t.Fatalf("setup: receipt Number = %q, want GR/26-27/00001", r.Number)
	}

	if res := ruCancel(t, h, h.Admin, fixture.Receipt, r.ID, ruCancelReason); res.State != doc.StateCancelled {
		t.Errorf("cancel GR/26-27/00001: State = %q, want cancelled", res.State)
	}
	ruWantQty(t, h, ol[0], 0, "GR/26-27/00001 cancelled, before the order's cancel")

	probe := &fixture.Probe{}
	ov := h.Load(t, fixture.Order, o.ID).Version
	res, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Ravi, fixture.Order, o.ID, doc.ActionCancel, trIn(ov, ruOrderCancelReason))
	if err != nil {
		t.Fatalf("cancel PO/26-27/00001: %v", err)
	}
	if res.State != doc.StateCancelled {
		t.Errorf("cancel PO/26-27/00001: Result State = %q, want cancelled", res.State)
	}
	trState(t, h, fixture.Order, o.ID, doc.StateCancelled, ov+1)
	if !slices.Contains(probe.Hooks(), "ReverseEffects") {
		t.Errorf("order hooks = %q, want ReverseEffects", probe.Hooks())
	}
	ruWantQty(t, h, ol[0], 0, "order cancelled")
}

// DOC-059
func TestDoc_RollUp_Recompute(t *testing.T) {
	h := fixturetest.New(t)
	_, ol := ruOrder(t, h, "10.000")
	r1 := ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "3"})
	ruWantQty(t, h, ol[0], 7000, "R1 and R2 approved")

	h.Exec(t, `UPDATE fixture_order_line SET received_qty = 99000 WHERE id = ?`, ol[0])
	ruReceiptsNeedApproval(h)
	ruApproved(t, h, h.Ravi, fixturetest.RL{OrderLineID: ol[0], Qty: "2"})
	ruWantQty(t, h, ol[0], 9000, "R3 approved after corrupting to 99000 (4+3+2, not 101)")

	ruCancel(t, h, h.Admin, fixture.Receipt, r1.ID, ruCancelReason)
	ruWantQty(t, h, ol[0], 5000, "R1 cancelled")
}

// DOC-084
func TestAudit_EffectWithCause(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	ruReceiptsNeedApproval(h)
	r := ruApproved(t, h, h.Suresh, fixturetest.RL{OrderLineID: ol[0], Qty: "4"})
	if r.Number != "GR/26-27/00001" {
		t.Errorf("receipt Number = %q, want GR/26-27/00001", r.Number)
	}

	effects := ruEffects(t, h, o.ID)
	if len(effects) != 1 {
		t.Fatalf("order effect rows = %d, want 1: %+v", len(effects), effects)
	}
	e := effects[0]
	if e.DocType != fixture.Order || e.DocID != o.ID {
		t.Errorf("effect row on %s %d, want %s %d", e.DocType, e.DocID, fixture.Order, o.ID)
	}
	ruWantEffect(t, e, 5,
		audit.Ref{DocType: fixture.Receipt, DocID: r.ID, Number: "GR/26-27/00001", Action: "approve"},
		[]audit.Change{{Field: "received_qty", LineNo: 1, LineID: ol[0], Before: "0", After: "4000", Op: "set"}})
	trState(t, h, fixture.Order, o.ID, doc.StateApproved, 0)
}

// Review Focus 4
func TestDoc_RollUp_TwoLinesSameTarget(t *testing.T) {
	h := fixturetest.New(t)
	o, ol := ruOrder(t, h, "10.000")
	v := o.Version
	ruReceiptsNeedApproval(h)
	r := ruApproved(t, h, h.Ravi,
		fixturetest.RL{OrderLineID: ol[0], Qty: "2"}, fixturetest.RL{OrderLineID: ol[0], Qty: "3"})

	ruWantQty(t, h, ol[0], 5000, "receipt with lines 2 and 3 on one order line")
	if d := trState(t, h, fixture.Order, o.ID, doc.StateApproved, 0); d.Version != v+1 {
		t.Errorf("order Version = %d, want %d (one bump)", d.Version, v+1)
	}
	effects := ruEffects(t, h, o.ID)
	if len(effects) != 1 {
		t.Fatalf("order effect rows = %d, want exactly 1: %+v", len(effects), effects)
	}
	ruWantEffect(t, effects[0], h.Ravi.ID,
		audit.Ref{DocType: fixture.Receipt, DocID: r.ID, Number: "GR/26-27/00001", Action: "approve"},
		[]audit.Change{{Field: "received_qty", LineNo: 1, LineID: ol[0], Before: "0", After: "5000", Op: "set"}})
}
