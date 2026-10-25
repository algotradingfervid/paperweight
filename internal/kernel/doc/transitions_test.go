package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// trRejectReason is DOC-009's reject reason.
const trRejectReason = "Rate for MS-ANGLE-50 is ₹62.5000, agreed rate is ₹60.0000."

// trMsgPendingLocked is the guard's save refusal on a pending document; %s is
// the display name.
const trMsgPendingLocked = "%s is waiting for approval and can't be changed. Withdraw it to make changes."

// trDraft is the display name of a document without a number.
func trDraft(id int64) string { return fmt.Sprintf("Draft %d", id) }

// trIn is a request carrying only version v (and reason, when given).
func trIn(v int, reason ...string) doc.ActionInput {
	in := doc.ActionInput{Version: v}
	if len(reason) > 0 {
		in.Reason = reason[0]
	}
	return in
}

// trSave saves draft d with remarks changed to remarks (and edit applied when
// non-nil) and fails the test on an error.
func trSave(t *testing.T, h *fixturetest.H, p auth.Principal, d doc.Doc, remarks string, edit func(in *doc.ActionInput)) doc.Result {
	t.Helper()
	in := h.In(d)
	in.Values["remarks"] = remarks
	if edit != nil {
		edit(&in)
	}
	return h.MustDo(t, t.Context(), p, d.DocType, d.ID, doc.ActionSave, in)
}

// trRefPOAt creates the reference PO by Priya and saves it until it is a draft
// at version v (v >= 1).
func trRefPOAt(t *testing.T, h *fixturetest.H, v int) doc.Doc {
	t.Helper()
	res := h.RefPO(t)
	d := h.Load(t, fixture.Order, res.ID)
	for i := 2; i <= v; i++ {
		trSave(t, h, h.Priya, d, "edit "+strconv.Itoa(i), nil)
		d = h.Load(t, fixture.Order, res.ID)
	}
	if d.Version != v {
		t.Fatalf("setup: draft Version = %d, want %d", d.Version, v)
	}
	return d
}

// trPendingAt3 is the reference PO submitted by Priya at version 2, so it is
// pending_approval at version 3 (DOC-095, DOC-096).
func trPendingAt3(t *testing.T, h *fixturetest.H) doc.Doc {
	t.Helper()
	d := trRefPOAt(t, h, 2)
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit, trIn(2))
	d = h.Load(t, fixture.Order, d.ID)
	if d.State != doc.StatePending || d.Version != 3 {
		t.Fatalf("setup: State %q Version %d, want pending_approval at 3", d.State, d.Version)
	}
	return d
}

// trReceipt creates Suresh's draft receipt for qty on the one line (qty
// 10.000) of an order forced to approved as PO/26-27/00001. It returns the
// receipt and the order line id.
func trReceipt(t *testing.T, h *fixturetest.H, qty string) (doc.Doc, int64) {
	t.Helper()
	o := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
	h.ForceState(t, fixture.Order, o.ID, doc.StateApproved, "PO/26-27/00001")
	ol := h.Load(t, fixture.Order, o.ID).Lines[0].ID
	r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: ol, Qty: qty})
	return h.Load(t, fixture.Receipt, r.ID), ol
}

// trReceivedQty reads fixture_order_line.received_qty.
func trReceivedQty(t *testing.T, h *fixturetest.H, orderLineID int64) int64 {
	t.Helper()
	var v int64
	if err := h.DB.SQL().QueryRowContext(t.Context(),
		`SELECT received_qty FROM fixture_order_line WHERE id = ?`, orderLineID).Scan(&v); err != nil {
		t.Fatalf("read received_qty of line %d: %v", orderLineID, err)
	}
	return v
}

// trCount counts the entries equal to s.
func trCount(calls []string, s string) int {
	n := 0
	for _, c := range calls {
		if c == s {
			n++
		}
	}
	return n
}

// trActions returns the audit entries of d whose Action is action.
func trActions(entries []audit.Entry, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range entries {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// trChange returns the change of field in e, or fails the test.
func trChange(t *testing.T, e audit.Entry, field string) audit.Change {
	t.Helper()
	for _, c := range e.Changes {
		if c.Field == field {
			return c
		}
	}
	t.Errorf("%s audit row has no %q change (Changes %+v)", e.Action, field, e.Changes)
	return audit.Change{}
}

// trState fails unless d's stored state and version are as given.
func trState(t *testing.T, h *fixturetest.H, dt string, id int64, s doc.State, v int) doc.Doc {
	t.Helper()
	d := h.Load(t, dt, id)
	if d.State != s {
		t.Errorf("State = %q, want %q", d.State, s)
	}
	if v != 0 && d.Version != v {
		t.Errorf("Version = %d, want %d", d.Version, v)
	}
	return d
}

// DOC-007
func TestDoc_Submit_WithRule_PendingNoEffects(t *testing.T) {
	h := fixturetest.New(t)
	h.Approvals.Needed = func(string, types.Money) bool { return true }
	r, ol := trReceipt(t, h, "4")
	h.Approvals.Calls = nil
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)

	res, err := h.Do(t, ctx, h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if res.State != doc.StatePending || res.Number != "" {
		t.Errorf("Result State %q Number %q, want pending_approval and \"\"", res.State, res.Number)
	}
	d := trState(t, h, fixture.Receipt, r.ID, doc.StatePending, r.Version+1)
	if d.Number != "" {
		t.Errorf("Number = %q, want \"\"", d.Number)
	}
	if got := h.Display(t, fixture.Receipt, r.ID); got != trDraft(r.ID) {
		t.Errorf("Display = %q, want %q", got, trDraft(r.ID))
	}
	for _, c := range []string{"Receipt.ApplyEffects", "NAM.Assign"} {
		if slices.Contains(probe.Calls, c) {
			t.Errorf("probe has %q, want it absent (probe %q)", c, probe.Calls)
		}
	}
	fixturetest.Eq(t, h.Approvals.Calls, []string{"APR.Evaluate", "APR.Open"})
	if got := trReceivedQty(t, h, ol); got != 0 {
		t.Errorf("order line received_qty = %d, want 0", got)
	}
	subs := trActions(h.Audit(t, fixture.Receipt, r.ID), "submit")
	if len(subs) != 1 || subs[0].From != string(doc.StateDraft) || subs[0].To != string(doc.StatePending) {
		t.Errorf("submit audit rows = %+v, want one draft -> pending_approval", subs)
	}
}

// DOC-009
func TestDoc_Reject_ThenEditToDraft(t *testing.T) {
	h := fixturetest.New(t)
	d := h.PendingOrder(t)
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)

	res, err := h.Do(t, ctx, h.Ravi, fixture.Order, d.ID, doc.ActionReject, trIn(d.Version, trRejectReason))
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if res.State != doc.StateRejected {
		t.Errorf("Result.State = %q, want rejected", res.State)
	}
	rj := trState(t, h, fixture.Order, d.ID, doc.StateRejected, d.Version+1)
	if rj.StateReason != trRejectReason {
		t.Errorf("StateReason = %q, want %q", rj.StateReason, trRejectReason)
	}
	if rj.Number != "" || res.Number != "" {
		t.Errorf("Number = %q (Result %q), want \"\"", rj.Number, res.Number)
	}
	if got := h.Display(t, fixture.Order, d.ID); got != trDraft(d.ID) {
		t.Errorf("Display = %q, want %q", got, trDraft(d.ID))
	}
	for _, c := range []string{"Order.ApplyEffects", "NAM.Assign"} {
		if slices.Contains(probe.Calls, c) {
			t.Errorf("reject: probe has %q, want it absent (probe %q)", c, probe.Calls)
		}
	}
	rows := trActions(h.Audit(t, fixture.Order, d.ID), "reject")
	if len(rows) != 1 {
		t.Fatalf("reject audit rows = %d, want 1", len(rows))
	}
	if rows[0].Reason != trRejectReason || rows[0].From != string(doc.StatePending) || rows[0].To != string(doc.StateRejected) {
		t.Errorf("reject row = %+v, want pending_approval -> rejected with Reason %q", rows[0], trRejectReason)
	}

	rv, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionRevise, trIn(rj.Version))
	if err != nil {
		t.Fatalf("revise: %v", err)
	}
	if rv.State != doc.StateDraft || rv.Number != "" {
		t.Errorf("revise Result State %q Number %q, want draft and \"\"", rv.State, rv.Number)
	}
	dr := trState(t, h, fixture.Order, d.ID, doc.StateDraft, rj.Version+1)
	if dr.Number != "" || dr.StateReason != "" {
		t.Errorf("after revise Number %q StateReason %q, want both \"\"", dr.Number, dr.StateReason)
	}
	trSave(t, h, h.Priya, dr, "Rate corrected", nil)
	if n := len(trActions(h.Audit(t, fixture.Order, d.ID), "reject")); n != 1 {
		t.Errorf("reject rows in ForDoc after revise and save = %d, want 1", n)
	}
}

// trRejectRevise rejects the pending order d (Ravi), revises it (Priya) and
// saves MS-ANGLE-50's rate as 60.0000. It returns the saved draft.
func trRejectRevise(t *testing.T, h *fixturetest.H, d doc.Doc) doc.Doc {
	t.Helper()
	rj := h.MustDo(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionReject, trIn(d.Version, trRejectReason))
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionRevise, trIn(rj.Version))
	dr := h.Load(t, fixture.Order, d.ID)
	item := strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10)
	trSave(t, h, h.Priya, dr, "", func(in *doc.ActionInput) {
		for _, l := range in.Lines {
			if l["item_id"] == item {
				l["rate"] = "60.0000"
			}
		}
	})
	return trState(t, h, fixture.Order, d.ID, doc.StateDraft, 0)
}

// DOC-010
func TestDoc_Resubmit_AfterReject(t *testing.T) {
	t.Run("rule", func(t *testing.T) {
		h := fixturetest.New(t)
		dr := trRejectRevise(t, h, h.PendingOrder(t))
		probe := &fixture.Probe{}
		ctx := fixture.WithProbe(t.Context(), probe)
		res, err := h.Do(t, ctx, h.Priya, fixture.Order, dr.ID, doc.ActionSubmit, trIn(dr.Version))
		if err != nil {
			t.Fatalf("resubmit: %v", err)
		}
		fixturetest.Eq(t, probe.Hooks(), []string{"Validate", "BeforeSubmit"})
		if n := trCount(h.Approvals.Calls, "APR.Evaluate"); n != 2 {
			t.Errorf("APR.Evaluate calls = %d, want 2 (Calls %q)", n, h.Approvals.Calls)
		}
		if res.State != doc.StatePending || res.Number != "" {
			t.Errorf("Result State %q Number %q, want pending_approval and \"\"", res.State, res.Number)
		}
		if d := trState(t, h, fixture.Order, dr.ID, doc.StatePending, dr.Version+1); d.Number != "" {
			t.Errorf("Number = %q, want \"\"", d.Number)
		}
	})
	t.Run("no rule on the second submit", func(t *testing.T) {
		h := fixturetest.New(t)
		dr := trRejectRevise(t, h, h.PendingOrder(t))
		h.Approvals.Needed = func(string, types.Money) bool { return false }
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, dr.ID, doc.ActionSubmit, trIn(dr.Version))
		if err != nil {
			t.Fatalf("resubmit: %v", err)
		}
		if n := trCount(h.Approvals.Calls, "APR.Evaluate"); n != 2 {
			t.Errorf("APR.Evaluate calls = %d, want 2 (Calls %q)", n, h.Approvals.Calls)
		}
		if res.State != doc.StateApproved || res.Number != "PO/26-27/00001" {
			t.Errorf("Result State %q Number %q, want approved PO/26-27/00001", res.State, res.Number)
		}
		if d := trState(t, h, fixture.Order, dr.ID, doc.StateApproved, 0); d.Number != "PO/26-27/00001" {
			t.Errorf("Number = %q, want PO/26-27/00001", d.Number)
		}
	})
}

// DOC-011
func TestDoc_Submit_Twice(t *testing.T) {
	t.Run("order with rule", func(t *testing.T) {
		h := fixturetest.New(t)
		d := trRefPOAt(t, h, 3)
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit, trIn(3))
		if err != nil {
			t.Fatalf("first submit: %v", err)
		}
		if res.State != doc.StatePending {
			t.Fatalf("first submit State = %q, want pending_approval", res.State)
		}
		count := h.AuditCount(t)
		_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit, trIn(3))
		fixturetest.ErrIs(t, err, doc.KindRefused, fmt.Sprintf("%s has already been submitted.", trDraft(d.ID)))
		trState(t, h, fixture.Order, d.ID, doc.StatePending, 4)
		if h.Naming.Assigned != 0 {
			t.Errorf("Naming.Assigned = %d, want 0", h.Naming.Assigned)
		}
		if n := trCount(h.Approvals.Calls, "APR.Open"); n != 1 {
			t.Errorf("APR.Open calls = %d, want 1 (Calls %q)", n, h.Approvals.Calls)
		}
		if n := len(trActions(h.Audit(t, fixture.Order, d.ID), "submit")); n != 1 {
			t.Errorf("submit audit rows = %d, want 1", n)
		}
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d after the refused submit, want %d", got, count)
		}
	})
	t.Run("receipt without rule", func(t *testing.T) {
		h := fixturetest.New(t)
		r, _ := trReceipt(t, h, "4")
		res, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
		if err != nil {
			t.Fatalf("first submit: %v", err)
		}
		if res.State != doc.StateApproved || res.Number != "GR/26-27/00001" {
			t.Fatalf("first submit Result State %q Number %q, want approved GR/26-27/00001", res.State, res.Number)
		}
		_, err = h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
		fixturetest.ErrIs(t, err, doc.KindRefused, "GR/26-27/00001 has already been submitted.")
		if h.Naming.Assigned != 1 {
			t.Errorf("Naming.Assigned = %d, want 1", h.Naming.Assigned)
		}
		if n := trCount(h.Approvals.Calls, "APR.Open"); n != 1 {
			t.Errorf("APR.Open calls = %d, want 1 (Calls %q)", n, h.Approvals.Calls)
		}
		subs := trActions(h.Audit(t, fixture.Receipt, r.ID), "submit")
		if len(subs) != 1 {
			t.Fatalf("submit audit rows = %d, want 1", len(subs))
		}
		if subs[0].From != string(doc.StateDraft) || subs[0].To != string(doc.StateApproved) {
			t.Errorf("submit row %s -> %s, want draft -> approved", subs[0].From, subs[0].To)
		}
		if c := trChange(t, subs[0], "auto_approved"); c.After != "true" {
			t.Errorf("auto_approved change After = %q, want \"true\"", c.After)
		}
		if c := trChange(t, subs[0], "number"); c.After != "GR/26-27/00001" {
			t.Errorf("number change After = %q, want GR/26-27/00001", c.After)
		}
		if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_effect_log WHERE receipt_id = ?`, r.ID); n != 1 {
			t.Errorf("fixture_effect_log rows = %d, want 1 (one line, effects once)", n)
		}
	})
	// CD-122: DOC owns the no-rule GR path; a submit whose effects fail stays
	// a draft without a number, and the next submit takes GR/26-27/00001.
	t.Run("receipt without rule, failed submit stays draft", func(t *testing.T) {
		h := fixturetest.New(t)
		r, ol := trReceipt(t, h, "4")
		count := h.AuditCount(t)
		probe := &fixture.Probe{FailAfterWrite: map[string]error{"ApplyEffects": errors.New("effects down")}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
		if err == nil {
			t.Fatal("submit with failing ApplyEffects: err = nil, want an error")
		}
		if d := trState(t, h, fixture.Receipt, r.ID, doc.StateDraft, r.Version); d.Number != "" {
			t.Errorf("Number = %q, want \"\"", d.Number)
		}
		if got := trReceivedQty(t, h, ol); got != 0 {
			t.Errorf("received_qty = %d, want 0 (rolled back)", got)
		}
		if got := h.AuditCount(t); got != count {
			t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
		}
		res, err := h.Do(t, t.Context(), h.Suresh, fixture.Receipt, r.ID, doc.ActionSubmit, trIn(r.Version))
		if err != nil {
			t.Fatalf("second submit: %v", err)
		}
		if res.State != doc.StateApproved || res.Number != "GR/26-27/00001" {
			t.Errorf("Result State %q Number %q, want approved GR/26-27/00001", res.State, res.Number)
		}
	})
}

// DOC-022 engine half
func TestDoc_Submit_NeedsALine(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya)
	if res.Version != 1 {
		t.Fatalf("empty draft Version = %d, want 1", res.Version)
	}
	count := h.AuditCount(t)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, trIn(1))
	fixturetest.ErrIs(t, err, doc.KindInvalid, "")
	var de *doc.Error
	if errors.As(err, &de) {
		fixturetest.Eq(t, de.Fields, []doc.FieldError{{Field: "lines", Line: 0, Message: "Add at least one line."}})
	}
	trState(t, h, fixture.Order, res.ID, doc.StateDraft, 1)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
}

// DOC-095
func TestDoc_Withdraw_PendingToDraft(t *testing.T) {
	h := fixturetest.New(t)
	d := trPendingAt3(t, h)
	h.Clock.Set("2026-10-24T05:00:00Z")
	h.Approvals.Calls = nil
	before := len(h.Audit(t, fixture.Order, d.ID))
	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)

	res, err := h.Do(t, ctx, h.Priya, fixture.Order, d.ID, doc.ActionWithdraw, trIn(3))
	if err != nil {
		t.Fatalf("withdraw: %v", err)
	}
	if res.State != doc.StateDraft || res.Version != 4 {
		t.Errorf("Result State %q Version %d, want draft 4", res.State, res.Version)
	}
	wd := trState(t, h, fixture.Order, d.ID, doc.StateDraft, 4)
	entries := h.Audit(t, fixture.Order, d.ID)
	if len(entries) != before+1 {
		t.Fatalf("audit rows = %d, want %d (exactly one new row)", len(entries), before+1)
	}
	e := entries[len(entries)-1]
	at := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if e.Action != "withdraw" || e.From != string(doc.StatePending) || e.To != string(doc.StateDraft) ||
		e.By != types.UserID(4) || !e.At.Equal(at) || e.Reason != "" {
		t.Errorf("withdraw row = %+v, want withdraw pending_approval -> draft By 4 At %s Reason \"\"", e, at)
	}
	fixturetest.Eq(t, h.Approvals.Calls, []string{"APR.Record:withdraw"})
	for _, c := range []string{"Order.ApplyEffects", "Order.ReverseEffects", "NAM.Assign"} {
		if slices.Contains(probe.Calls, c) {
			t.Errorf("withdraw: probe has %q, want it absent (probe %q)", c, probe.Calls)
		}
	}
	if got := h.Display(t, fixture.Order, d.ID); got != trDraft(d.ID) {
		t.Errorf("Display = %q, want %q", got, trDraft(d.ID))
	}

	sv := trSave(t, h, h.Priya, wd, "", func(in *doc.ActionInput) { in.Lines[0]["qty"] = "1100.000" })
	if sv.Version != 5 {
		t.Errorf("save after withdraw Version = %d, want 5", sv.Version)
	}
	h.Approvals.Calls = nil
	sub, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit, trIn(5))
	if err != nil {
		t.Fatalf("submit after withdraw: %v", err)
	}
	for _, c := range []string{"APR.Evaluate", "APR.Open"} {
		if !slices.Contains(h.Approvals.Calls, c) {
			t.Errorf("submit after withdraw: Calls %q lack %q", h.Approvals.Calls, c)
		}
	}
	if sub.State != doc.StatePending {
		t.Errorf("submit after withdraw State = %q, want pending_approval", sub.State)
	}
}

// DOC-096
func TestDoc_Withdraw_Atomic(t *testing.T) {
	h := fixturetest.New(t)
	d := trPendingAt3(t, h)
	aprDown := errors.New("apr down")
	h.Approvals.Fail = map[string]error{"Record:withdraw": aprDown}
	count := h.AuditCount(t)

	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionWithdraw, trIn(3))
	if !errors.Is(err, aprDown) {
		t.Errorf("withdraw err = %v, want the APR error %q", err, aprDown)
	}
	if !slices.Contains(h.Approvals.Calls, "APR.Record:withdraw") {
		t.Errorf("Calls %q lack APR.Record:withdraw", h.Approvals.Calls)
	}
	after := trState(t, h, fixture.Order, d.ID, doc.StatePending, 3)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
	in := h.In(after)
	in.Values["remarks"] = "after failed withdraw"
	_, err = h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	fixturetest.ErrIs(t, err, doc.KindRefused, fmt.Sprintf(trMsgPendingLocked, trDraft(d.ID)))
}

// DOC-099
func TestDoc_Claim_SubmitAndApprove(t *testing.T) {
	h := fixturetest.New(t)
	c := h.NewClaim(t, h.Deepak, "EMP-007",
		fixturetest.CL{Type: "TRAVEL", Amount: "1250.00"}, fixturetest.CL{Type: "FOOD", Amount: "480.50"})

	sub, err := h.Do(t, t.Context(), h.Deepak, fixture.Claim, c.ID, doc.ActionSubmit, trIn(c.Version))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if sub.State != doc.StatePending {
		t.Errorf("submit State = %q, want pending_approval", sub.State)
	}
	trState(t, h, fixture.Claim, c.ID, doc.StatePending, 0)
	if len(h.Approvals.Opened) != 1 {
		t.Fatalf("Approvals.Opened = %+v, want one requirement", h.Approvals.Opened)
	}
	if got := h.Approvals.Opened[0]; got.Amount != types.Money(173050) || got.Requester != types.UserID(7) {
		t.Errorf("Opened[0] Amount %d Requester %d, want 173050 and 7 (deepak)", got.Amount, got.Requester)
	}

	ap, err := h.Do(t, t.Context(), h.Priya, fixture.Claim, c.ID, doc.ActionApprove, trIn(sub.Version))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if ap.State != doc.StateApproved || ap.Number != "EX/26-27/00001" {
		t.Errorf("approve Result State %q Number %q, want approved EX/26-27/00001", ap.State, ap.Number)
	}
	if d := trState(t, h, fixture.Claim, c.ID, doc.StateApproved, 0); d.Number != "EX/26-27/00001" {
		t.Errorf("Number = %q, want EX/26-27/00001", d.Number)
	}
	if n := len(h.Approvals.Calls); n == 0 || h.Approvals.Calls[n-1] != "APR.Record:approve" {
		t.Errorf("Calls = %q, want them to end with APR.Record:approve", h.Approvals.Calls)
	}
}

// DOC-104
func TestDoc_ApproveReject_CheckOrder(t *testing.T) {
	decisions := []doc.Action{doc.ActionApprove, doc.ActionReject}
	t.Run("hidden record is not found before CheckApprover", func(t *testing.T) {
		for _, a := range decisions {
			h := fixturetest.New(t)
			d := h.PendingOrder(t)
			h.Approvals.Calls = nil
			h.Auth.Hide = func(_ auth.Principal, _ string, id int64) bool { return id == d.ID }
			_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, a, trIn(d.Version, trRejectReason))
			if !errors.Is(err, doctype.ErrNotFound) {
				t.Errorf("%s hidden: err = %v, want doctype.ErrNotFound", a, err)
			}
			fixturetest.Eq(t, h.Approvals.Calls, []string(nil))
			trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
		}
	})
	t.Run("no approve right in AUTH, CheckApprover allows", func(t *testing.T) {
		h := fixturetest.New(t)
		d := h.PendingOrder(t)
		h.Auth.Deny = func(_ auth.Principal, _ string, r auth.Right) bool { return r == "approve" }
		h.Auth.Calls = nil
		res, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, trIn(d.Version))
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		if res.State != doc.StateApproved || res.Number != "PO/26-27/00001" {
			t.Errorf("Result State %q Number %q, want approved PO/26-27/00001", res.State, res.Number)
		}
		fixturetest.Eq(t, h.Auth.Calls, []string{"AUTH.Can:read"})
	})
	t.Run("CheckApprover refusal is returned unchanged", func(t *testing.T) {
		notYours := errors.New("not your approval")
		for _, a := range decisions {
			h := fixturetest.New(t)
			d := h.PendingOrder(t)
			h.Approvals.Deny = func(auth.Principal) error { return notYours }
			count := h.AuditCount(t)
			_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, a, trIn(d.Version, trRejectReason))
			if !errors.Is(err, notYours) {
				t.Errorf("%s pending: err = %v, want %q", a, err, notYours)
			}
			trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
			if got := h.AuditCount(t); got != count {
				t.Errorf("%s pending: AuditCount = %d, want %d", a, got, count)
			}

			h.ForceState(t, fixture.Order, d.ID, doc.StateDraft, "")
			_, err = h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, a, trIn(d.Version, trRejectReason))
			if !errors.Is(err, notYours) {
				t.Errorf("%s on a draft: err = %v, want %q (CheckApprover before the guard)", a, err, notYours)
			}
		}
	})
	t.Run("CheckApprover refusal wins over a stale version", func(t *testing.T) {
		notYours := errors.New("not your approval")
		for _, a := range decisions {
			h := fixturetest.New(t)
			d := h.PendingOrder(t)
			h.Approvals.Deny = func(auth.Principal) error { return notYours }
			_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, a, trIn(d.Version-1, trRejectReason))
			if !errors.Is(err, notYours) {
				t.Errorf("%s stale: err = %v, want %q (APR before the version check)", a, err, notYours)
			}
			trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
		}
	})
}

func TestDoc_Withdraw_NeedsSubmitRight(t *testing.T) {
	h := fixturetest.New(t)
	d := h.PendingOrder(t)
	h.Approvals.Calls = nil
	h.Auth.Deny = func(_ auth.Principal, _ string, r auth.Right) bool { return r == "submit" }
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionWithdraw, trIn(d.Version))
	if !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("withdraw without submit right: err = %v, want auth.ErrForbidden", err)
	}
	trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
	fixturetest.Eq(t, h.Approvals.Calls, []string(nil))

	// With the right back, the same request withdraws.
	h.Auth.Deny = nil
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionWithdraw, trIn(d.Version))
	if err != nil {
		t.Fatalf("withdraw with submit right: %v", err)
	}
	if res.State != doc.StateDraft {
		t.Errorf("State = %q, want draft", res.State)
	}
}

func TestDoc_Reject_RequiresReason(t *testing.T) {
	h := fixturetest.New(t)
	d := h.PendingOrder(t)
	count := h.AuditCount(t)
	for _, tc := range []struct{ name, reason, msg string }{
		{"empty", "", "Enter a reason for rejecting."},
		{"spaces", "   ", "Enter a reason for rejecting."},
		{"1001 runes", strings.Repeat("a", 1001), "Keep the reason under 1,000 characters."},
		{"1001 Devanagari runes", strings.Repeat("क", 1001), "Keep the reason under 1,000 characters."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionReject, trIn(d.Version, tc.reason))
			fixturetest.ErrIs(t, err, doc.KindInvalid, tc.msg)
			trState(t, h, fixture.Order, d.ID, doc.StatePending, d.Version)
		})
	}
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
	// Lengths are counted in characters: 1,000 Devanagari runes (3,000 bytes)
	// are accepted.
	ok := strings.Repeat("क", 1000)
	if _, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionReject, trIn(d.Version, ok)); err != nil {
		t.Fatalf("reject with 1,000 runes: %v", err)
	}
	if rj := trState(t, h, fixture.Order, d.ID, doc.StateRejected, 0); rj.StateReason != ok {
		t.Errorf("StateReason has %d runes, want the 1,000-rune reason", len([]rune(rj.StateReason)))
	}
}

func TestDoc_Approve_AssignsNumber(t *testing.T) {
	h := fixturetest.New(t)
	for i, want := range []string{"PO/26-27/00001", "PO/26-27/00002"} {
		d := h.PendingOrder(t)
		if d.State != doc.StatePending || d.Number != "" {
			t.Fatalf("order %d: State %q Number %q, want pending_approval and \"\"", i+1, d.State, d.Number)
		}
		res, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, trIn(d.Version))
		if err != nil {
			t.Fatalf("order %d: approve: %v", i+1, err)
		}
		if res.State != doc.StateApproved || res.Number != want {
			t.Errorf("order %d: Result State %q Number %q, want approved %s", i+1, res.State, res.Number, want)
		}
		ap := trState(t, h, fixture.Order, d.ID, doc.StateApproved, d.Version+1)
		if ap.Number != want {
			t.Errorf("order %d: Number = %q, want %s", i+1, ap.Number, want)
		}
		if got := h.Display(t, fixture.Order, d.ID); got != want {
			t.Errorf("order %d: Display = %q, want %s", i+1, got, want)
		}
		rows := trActions(h.Audit(t, fixture.Order, d.ID), "approve")
		if len(rows) != 1 {
			t.Fatalf("order %d: approve audit rows = %d, want 1", i+1, len(rows))
		}
		if rows[0].From != string(doc.StatePending) || rows[0].To != string(doc.StateApproved) {
			t.Errorf("order %d: approve row %s -> %s, want pending_approval -> approved", i+1, rows[0].From, rows[0].To)
		}
		if c := trChange(t, rows[0], "number"); c.After != want {
			t.Errorf("order %d: number change After = %q, want %s", i+1, c.After, want)
		}
	}
}
