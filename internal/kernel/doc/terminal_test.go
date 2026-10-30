package doc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/types"
)

// Task 10 refusal texts (plan "Task 10: Close, cancel, delete, amend").
const (
	termCancelShort = "Enter a reason for cancelling (at least 3 characters)."
	termCloseShort  = "Enter a reason for closing (at least 3 characters)."
	termReasonLong  = "Keep the reason under 500 characters."
	// termAmendedFmt: %[1]s is the source's display name, %[2]s the amendment's.
	termAmendedFmt = "%[1]s has already been amended as %[2]s. Open that document instead."
)

// termGR is the number ForceState gives approved, closed and cancelled receipts.
const termGR = "GR/26-27/00001"

// termMsg fails unless err is a *doc.Error with Message msg.
func termMsg(t *testing.T, err error, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want a *doc.Error with Message %q", err, err, msg)
	}
	if de.Message != msg {
		t.Errorf("Message = %q, want %q", de.Message, msg)
	}
}

// termUnchanged fails unless the document, AuditCount and the fixture_order
// row count are what they were before.
func termUnchanged(t *testing.T, h *fixturetest.H, before doc.Doc, count, rows int) {
	t.Helper()
	fixturetest.Eq(t, h.Load(t, before.DocType, before.ID), before)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
	if got := orderRows(t, h); got != rows {
		t.Errorf("fixture_order rows = %d, want %d (unchanged)", got, rows)
	}
}

// termCancelledRefPO creates the reference PO by Priya and forces it to
// cancelled PO/26-27/00001 with the cancel reason "Wrong supplier GSTIN"
// (DOC-064's order A).
func termCancelledRefPO(t *testing.T, h *fixturetest.H) doc.Doc {
	t.Helper()
	res := h.RefPO(t)
	h.ForceState(t, fixture.Order, res.ID, doc.StateCancelled, guardPO)
	h.Exec(t, `UPDATE fixture_order SET state_reason = ? WHERE id = ?`, "Wrong supplier GSTIN", res.ID)
	return h.Load(t, fixture.Order, res.ID)
}

// termReceipt makes an approved order PO/26-27/00001 (CEMENT-OPC 10.000) and
// a receipt by Suresh of 4.000 on its line, forced into s (approved, closed
// and cancelled receipts get GR/26-27/00001).
func termReceipt(t *testing.T, h *fixturetest.H, s doc.State) doc.Doc {
	t.Helper()
	o := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "380.0000"})
	h.ForceState(t, fixture.Order, o.ID, doc.StateApproved, guardPO)
	od := h.Load(t, fixture.Order, o.ID)
	r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: od.Lines[0].ID, Qty: "4.000"})
	number := ""
	switch s {
	case doc.StateApproved, doc.StateClosed, doc.StateCancelled:
		number = termGR
	}
	if s != doc.StateDraft {
		h.ForceState(t, fixture.Receipt, r.ID, s, number)
	}
	return h.Load(t, fixture.Receipt, r.ID)
}

// termDo runs a on d as p with d's version and reason, failing on an error.
func termDo(t *testing.T, h *fixturetest.H, ctx context.Context, p auth.Principal, d doc.Doc, a doc.Action, reason string) doc.Result {
	t.Helper()
	res, err := h.Do(t, ctx, p, d.DocType, d.ID, a, doc.ActionInput{Version: d.Version, Reason: reason})
	if err != nil {
		t.Fatalf("%s %s %d: %v", a, d.DocType, d.ID, err)
	}
	return res
}

// DOC-013
func TestDoc_Cancel_RequiresReason(t *testing.T) {
	h, d := guardOrder(t, doc.StateApproved)
	count, rows := h.AuditCount(t), orderRows(t, h)
	for _, tc := range []struct{ reason, want string }{
		{"", termCancelShort},
		{"  ", termCancelShort},
		{strings.Repeat("a", 501), termReasonLong},
	} {
		_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionCancel, doc.ActionInput{Version: d.Version, Reason: tc.reason})
		termMsg(t, err, tc.want)
		termUnchanged(t, h, d, count, rows)
	}
	if got := h.Load(t, fixture.Order, d.ID).State; got != doc.StateApproved {
		t.Errorf("State = %q, want approved", got)
	}
}

// DOC-014
func TestDoc_Close_Approved(t *testing.T) {
	h := fixturetest.New(t)
	o := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10.000", Rate: "380.0000"})
	h.ForceState(t, fixture.Order, o.ID, doc.StateApproved, guardPO)
	h.Exec(t, `UPDATE fixture_order_line SET received_qty = 6000 WHERE parent_id = ?`, o.ID)
	d := h.Load(t, fixture.Order, o.ID)
	const why = "Supplier can't supply balance 4 bags."

	res := termDo(t, h, t.Context(), h.Priya, d, doc.ActionClose, why)

	if res.State != doc.StateClosed {
		t.Errorf("Result.State = %q, want closed", res.State)
	}
	after := h.Load(t, fixture.Order, d.ID)
	if after.State != doc.StateClosed || after.StateReason != why {
		t.Errorf("State, StateReason = %q, %q; want closed, %q", after.State, after.StateReason, why)
	}
	if after.Version != d.Version+1 {
		t.Errorf("Version = %d, want %d", after.Version, d.Version+1)
	}
	entries := h.Audit(t, fixture.Order, d.ID)
	last := entries[len(entries)-1]
	if last.Action != "close" || last.From != "approved" || last.To != "closed" || last.Reason != why {
		t.Errorf("last audit row = %s %s->%s %q; want close approved->closed %q", last.Action, last.From, last.To, last.Reason, why)
	}
	if last.By != 4 || !last.At.Equal(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)) {
		t.Errorf("By, At = %d, %v; want 4, 2026-10-24T05:00:00Z", last.By, last.At)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE parent_id = ? AND received_qty = 6000`, d.ID); n != 1 {
		t.Errorf("received_qty changed by close: %d lines still at 6000, want 1", n)
	}
}

// DOC-015
func TestDoc_Close_Guards(t *testing.T) {
	h, d := guardOrder(t, doc.StateApproved)
	count, rows := h.AuditCount(t), orderRows(t, h)
	_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionClose, doc.ActionInput{Version: d.Version, Reason: ""})
	termMsg(t, err, termCloseShort)
	termUnchanged(t, h, d, count, rows)

	h2, draft := guardOrder(t, doc.StateDraft)
	count, rows = h2.AuditCount(t), orderRows(t, h2)
	_, err = h2.Do(t, t.Context(), h2.Priya, fixture.Order, draft.ID, doc.ActionClose, doc.ActionInput{Version: draft.Version, Reason: ""})
	fixturetest.ErrIs(t, err, doc.KindRefused, "Only approved documents can be closed.")
	termUnchanged(t, h2, draft, count, rows)
}

// DOC-027
func TestDoc_Hooks_CallOrder(t *testing.T) {
	receiptRule := func(dt string, _ types.Money) bool { return dt == fixture.Receipt }
	for _, tc := range []struct {
		name   string
		state  doc.State
		action doc.Action
		actor  func(h *fixturetest.H) auth.Principal
		rule   bool
		reason string
		want   []string
	}{
		{"save", doc.StateDraft, doc.ActionSave, func(h *fixturetest.H) auth.Principal { return h.Suresh }, false, "", []string{"Validate"}},
		{"submit rule", doc.StateDraft, doc.ActionSubmit, func(h *fixturetest.H) auth.Principal { return h.Suresh }, true, "", []string{"Validate", "BeforeSubmit"}},
		{"submit no rule", doc.StateDraft, doc.ActionSubmit, func(h *fixturetest.H) auth.Principal { return h.Suresh }, false, "", []string{"Validate", "BeforeSubmit", "ApplyEffects"}},
		{"approve", doc.StatePending, doc.ActionApprove, func(h *fixturetest.H) auth.Principal { return h.Ravi }, true, "", []string{"Validate", "BeforeSubmit", "ApplyEffects"}},
		{"reject", doc.StatePending, doc.ActionReject, func(h *fixturetest.H) auth.Principal { return h.Ravi }, true, "Wrong quantity counted.", nil},
		{"withdraw", doc.StatePending, doc.ActionWithdraw, func(h *fixturetest.H) auth.Principal { return h.Suresh }, true, "", nil},
		{"revise", doc.StateRejected, doc.ActionRevise, func(h *fixturetest.H) auth.Principal { return h.Suresh }, true, "", nil},
		{"cancel", doc.StateApproved, doc.ActionCancel, func(h *fixturetest.H) auth.Principal { return h.Admin }, false, "Wrong vehicle, goods returned at gate.", []string{"ReverseEffects"}},
		{"close", doc.StateApproved, doc.ActionClose, func(h *fixturetest.H) auth.Principal { return h.Suresh }, false, "Balance not needed", nil},
		{"delete", doc.StateDraft, doc.ActionDelete, func(h *fixturetest.H) auth.Principal { return h.Suresh }, false, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixturetest.New(t)
			if tc.rule {
				h.Approvals.Needed = receiptRule
			}
			d := termReceipt(t, h, tc.state)
			in := doc.ActionInput{Version: d.Version, Reason: tc.reason}
			if tc.action == doc.ActionSave {
				in = h.In(d)
				in.Values["remarks"] = "Unloaded at gate 2"
			}
			probe := &fixture.Probe{}
			if _, err := h.Do(t, fixture.WithProbe(t.Context(), probe), tc.actor(h), fixture.Receipt, d.ID, tc.action, in); err != nil {
				t.Fatalf("%s: %v", tc.action, err)
			}
			if got := probe.Hooks(); !slices.Equal(got, tc.want) {
				t.Errorf("hooks = %q, want %q", got, tc.want)
			}
		})
	}
}

// DOC-047
func TestDoc_Delete_CascadesLines(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000", Rate: "62.5000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "50.000", Rate: "96.0000"},
		fixturetest.OL{Item: "BOLT-M12", Qty: "3.000", Rate: "12.0000"},
	)
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE parent_id = ?`, res.ID); n != 3 {
		t.Fatalf("setup: %d lines, want 3", n)
	}
	h.MustDo(t, t.Context(), h.Priya, fixture.Order, res.ID, doc.ActionDelete, doc.ActionInput{Version: res.Version})

	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE parent_id = ?`, res.ID); n != 0 {
		t.Errorf("fixture_order_line rows for parent %d = %d, want 0", res.ID, n)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order WHERE id = ?`, res.ID); n != 0 {
		t.Errorf("fixture_order row %d still present", res.ID)
	}
	if len(h.Files.Deleted) != 1 {
		t.Fatalf("Files.Deleted = %+v, want one ref", h.Files.Deleted)
	}
	ref := h.Files.Deleted[0]
	metro := h.M.Project["PRJ-METRO"]
	if ref.DocType != fixture.Order || ref.ID != res.ID || ref.CreatedBy != 4 || !slices.Equal(ref.ProjectIDs, []int64{metro}) {
		t.Errorf("Files.Deleted[0] = %+v, want {DocType %s ID %d CreatedBy 4 ProjectIDs [%d]}", ref, fixture.Order, res.ID, metro)
	}
}

// DOC-061
func TestDoc_Delete_DraftKeepsAudit(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrder(t, h.Priya,
		fixturetest.OL{Item: "MS-ANGLE-50", Qty: "1200.000", Rate: "62.5000"},
		fixturetest.OL{Item: "WIRE-18G", Qty: "50.000", Rate: "96.0000"},
	)
	d := h.Load(t, fixture.Order, res.ID)
	in := h.In(d)
	in.Values["remarks"] = "Deliver to Pune site"
	saveDo(t, h, h.Priya, fixture.Order, d.ID, in)
	d = h.Load(t, fixture.Order, res.ID)
	in = h.In(d)
	in.Values["credit_days"] = "30"
	saveDo(t, h, h.Priya, fixture.Order, d.ID, in)
	d = h.Load(t, fixture.Order, res.ID)
	before := h.Audit(t, fixture.Order, d.ID)
	if len(before) != 3 {
		t.Fatalf("setup: %d audit rows, want 3", len(before))
	}

	h.MustDo(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionDelete, doc.ActionInput{Version: d.Version})

	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order WHERE id = ?`, d.ID); n != 0 {
		t.Errorf("fixture_order row %d still present", d.ID)
	}
	if n := saveCount(t, h, `SELECT COUNT(*) FROM fixture_order_line WHERE parent_id = ?`, d.ID); n != 0 {
		t.Errorf("%d lines still present", n)
	}
	entries := h.Audit(t, fixture.Order, d.ID)
	if len(entries) != 4 {
		t.Fatalf("ForDoc = %d rows, want 4: %+v", len(entries), entries)
	}
	fixturetest.Eq(t, entries[:3], before)
	last := entries[3]
	if last.Action != "delete" || last.From != "draft" || last.To != "" || last.By != 4 {
		t.Errorf("last row = %s %q->%q By %d; want delete draft->\"\" By 4", last.Action, last.From, last.To, last.By)
	}
	var snap *audit.Change
	for i := range last.Changes {
		if last.Changes[i].Field == "snapshot" {
			snap = &last.Changes[i]
		}
	}
	if snap == nil {
		t.Fatalf("delete row has no snapshot change: %+v", last.Changes)
	}
	if snap.Op != "set" {
		t.Errorf("snapshot Op = %q, want set", snap.Op)
	}
	var got struct {
		ID    int64
		Lines []struct{ ID int64 }
	}
	if err := json.Unmarshal([]byte(snap.Before), &got); err != nil {
		t.Fatalf("snapshot Before is not JSON of the Doc: %v\n%s", err, snap.Before)
	}
	var lineIDs []int64
	for _, l := range got.Lines {
		lineIDs = append(lineIDs, l.ID)
	}
	if got.ID != d.ID || !slices.Equal(lineIDs, saveLineIDs(d)) {
		t.Errorf("snapshot ID %d, line ids %v; want %d, %v", got.ID, lineIDs, d.ID, saveLineIDs(d))
	}
}

// DOC-064
func TestDoc_Amend_CopiesToDraft(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	h.Clock.Set("2026-10-24T09:15:00Z")

	res := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")

	if res.ID == 0 || res.ID == a.ID {
		t.Fatalf("Result.ID = %d, want a new document", res.ID)
	}
	if res.State != doc.StateDraft || res.Number != "" || res.Version != 1 {
		t.Errorf("Result = %+v, want State draft, Number \"\", Version 1", res)
	}
	n := h.Load(t, fixture.Order, res.ID)
	if n.State != doc.StateDraft || n.Version != 1 || n.Number != "" || n.StateReason != "" {
		t.Errorf("amendment State %q Version %d Number %q StateReason %q; want draft, 1, \"\", \"\"", n.State, n.Version, n.Number, n.StateReason)
	}
	if n.AmendedFromID != a.ID || n.AmendmentRootID != a.ID || n.AmendmentNo != 1 {
		t.Errorf("AmendedFromID %d AmendmentRootID %d AmendmentNo %d; want %d, %d, 1", n.AmendedFromID, n.AmendmentRootID, n.AmendmentNo, a.ID, a.ID)
	}
	if got, want := h.Display(t, fixture.Order, n.ID), fmt.Sprintf("Draft %d", n.ID); got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
	// CD-67: the amender is the new draft's creator.
	if n.CreatedBy != 3 || n.UpdatedBy != 3 {
		t.Errorf("CreatedBy, UpdatedBy = %d, %d; want 3, 3", n.CreatedBy, n.UpdatedBy)
	}
	if got := n.CreatedAt.UTC().Format(tsLayout); got != "2026-10-24T09:15:00.000Z" {
		t.Errorf("CreatedAt = %s, want 2026-10-24T09:15:00.000Z", got)
	}
	if got := n.UpdatedAt.UTC().Format(tsLayout); got != "2026-10-24T09:15:00.000Z" {
		t.Errorf("UpdatedAt = %s, want 2026-10-24T09:15:00.000Z", got)
	}
	want := maps.Clone(a.Header)
	want["state"] = string(doc.StateDraft) // the read-only state column (CD-162), checked above
	fixturetest.Eq(t, n.Header, want)
	if len(n.Lines) != 2 {
		t.Fatalf("amendment has %d lines, want 2", len(n.Lines))
	}
	for i, l := range n.Lines {
		if l.LineNo != i+1 {
			t.Errorf("Lines[%d].LineNo = %d, want %d", i, l.LineNo, i+1)
		}
		if l.ID == a.Lines[i].ID || l.ID == 0 {
			t.Errorf("Lines[%d].ID = %d, want a new id (source line %d)", i, l.ID, a.Lines[i].ID)
		}
		if got := l.Values["received_qty"]; got != any(types.Qty(0)) {
			t.Errorf("Lines[%d] received_qty = %#v, want 0", i, got)
		}
		gotV, wantV := doc.Values{}, doc.Values{}
		for k, v := range l.Values {
			if k != "received_qty" {
				gotV[k] = v
			}
		}
		for k, v := range a.Lines[i].Values {
			if k != "received_qty" {
				wantV[k] = v
			}
		}
		fixturetest.Eq(t, gotV, wantV)
	}

	src := h.Load(t, fixture.Order, a.ID)
	if src.State != doc.StateCancelled || src.Version != a.Version || src.CreatedBy != 4 || src.Number != guardPO {
		t.Errorf("source State %q Version %d CreatedBy %d Number %q; want cancelled, %d, 4, %s", src.State, src.Version, src.CreatedBy, src.Number, a.Version, guardPO)
	}
	if srcAudit := h.Audit(t, fixture.Order, a.ID); len(srcAudit) == 0 || srcAudit[len(srcAudit)-1].Action != "amended" {
		t.Errorf("source's last audit row is not \"amended\": %+v", srcAudit)
	}
	if newAudit := h.Audit(t, fixture.Order, n.ID); len(newAudit) == 0 || newAudit[0].Action != "create" {
		t.Errorf("amendment's first audit row is not \"create\": %+v", newAudit)
	}

	if len(h.Files.Copied) != 1 {
		t.Fatalf("Files.Copied = %+v, want one pair", h.Files.Copied)
	}
	from, to := h.Files.Copied[0][0], h.Files.Copied[0][1]
	if from.DocType != fixture.Order || from.ID != a.ID || from.CreatedBy != 4 {
		t.Errorf("Copied[0][0] = %+v, want {%s %d CreatedBy 4}", from, fixture.Order, a.ID)
	}
	if to.DocType != fixture.Order || to.ID != n.ID || to.CreatedBy != 3 {
		t.Errorf("Copied[0][1] = %+v, want {%s %d CreatedBy 3}", to, fixture.Order, n.ID)
	}

	// Approving the amendment numbers it from the root (needs Task 9).
	sub := termDo(t, h, t.Context(), h.Priya, n, doc.ActionSubmit, "")
	if sub.State != doc.StatePending {
		t.Fatalf("submit amendment: State %q, want pending_approval", sub.State)
	}
	probe := &fixture.Probe{}
	n = h.Load(t, fixture.Order, n.ID)
	app := termDo(t, h, fixture.WithProbe(t.Context(), probe), h.Ravi, n, doc.ActionApprove, "")
	if app.Number != "PO/26-27/00001-1" {
		t.Errorf("approved amendment Number = %q, want PO/26-27/00001-1", app.Number)
	}
	if !slices.Contains(probe.Calls, "NAM.AssignAmendment") {
		t.Errorf("probe calls %q lack NAM.AssignAmendment", probe.Calls)
	}
}

// DOC-065
func TestDoc_Amend_OnlyOnce(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	res := termDo(t, h, t.Context(), h.Ravi, a, doc.ActionAmend, "")
	a = h.Load(t, fixture.Order, a.ID)

	count, rows := h.AuditCount(t), orderRows(t, h)
	_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, a.ID, doc.ActionAmend, doc.ActionInput{Version: a.Version})
	fixturetest.ErrIs(t, err, doc.KindRefused, fmt.Sprintf(termAmendedFmt, guardPO, fmt.Sprintf("Draft %d", res.ID)))
	termUnchanged(t, h, a, count, rows)

	// Approve D (needs Task 9), then amend A again.
	d := h.Load(t, fixture.Order, res.ID)
	termDo(t, h, t.Context(), h.Priya, d, doc.ActionSubmit, "")
	d = h.Load(t, fixture.Order, res.ID)
	if got := termDo(t, h, t.Context(), h.Ravi, d, doc.ActionApprove, "").Number; got != "PO/26-27/00001-1" {
		t.Fatalf("approved D Number = %q, want PO/26-27/00001-1", got)
	}
	a = h.Load(t, fixture.Order, a.ID)
	count, rows = h.AuditCount(t), orderRows(t, h)
	_, err = h.Do(t, t.Context(), h.Ravi, fixture.Order, a.ID, doc.ActionAmend, doc.ActionInput{Version: a.Version})
	fixturetest.ErrIs(t, err, doc.KindRefused, fmt.Sprintf(termAmendedFmt, guardPO, "PO/26-27/00001-1"))
	termUnchanged(t, h, a, count, rows)
}

// termEngine is h's engine with CheckAmendment replaced by check.
func termEngine(h *fixturetest.H, check func(root string, k int, docLabel string) error) doc.Engine {
	nf := fixturetest.Numbers()
	nf.CheckAmendment = check
	return doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: h.Auth, Naming: h.Naming, Numbers: nf,
		Approval: h.Approvals, Files: h.Files,
	})
}

type termAmendCall struct {
	root  string
	k     int
	label string
}

func TestDoc_Amend_CallsCheckAmendmentNo(t *testing.T) {
	h := fixturetest.New(t)
	a := termCancelledRefPO(t, h)
	var calls []termAmendCall
	e := termEngine(h, func(root string, k int, label string) error {
		calls = append(calls, termAmendCall{root, k, label})
		return nil
	})
	if _, err := e.Do(t.Context(), h.Ravi, fixture.Order, a.ID, doc.ActionAmend, doc.ActionInput{Version: a.Version}); err != nil {
		t.Fatalf("amend: %v", err)
	}
	fixturetest.Eq(t, calls, []termAmendCall{{"PO/26-27/00001", 1, "Purchase Order"}})

	h2 := fixturetest.New(t)
	a2 := termCancelledRefPO(t, h2)
	limit := errors.New("limit")
	e2 := termEngine(h2, func(string, int, string) error { return limit })
	count, rows := h2.AuditCount(t), orderRows(t, h2)
	_, err := e2.Do(t.Context(), h2.Ravi, fixture.Order, a2.ID, doc.ActionAmend, doc.ActionInput{Version: a2.Version})
	fixturetest.ErrIs(t, err, doc.KindRefused, "limit")
	if !errors.Is(err, limit) {
		t.Errorf("errors.Is(err, limit) = false (err %v)", err)
	}
	termUnchanged(t, h2, a2, count, rows)
	if len(h2.Files.Copied) != 0 {
		t.Errorf("Files.Copied = %+v, want none", h2.Files.Copied)
	}
}

// Review Focus 6
func TestDoc_AprCallProtocol(t *testing.T) {
	order := func(s doc.State) func(t *testing.T, h *fixturetest.H) doc.Doc {
		return func(t *testing.T, h *fixturetest.H) doc.Doc {
			t.Helper()
			res := h.RefPO(t)
			if s != doc.StateDraft {
				h.ForceState(t, fixture.Order, res.ID, s, guardNumber(s))
			}
			return h.Load(t, fixture.Order, res.ID)
		}
	}
	receipt := func(t *testing.T, h *fixturetest.H) doc.Doc {
		t.Helper()
		return termReceipt(t, h, doc.StateDraft)
	}
	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, h *fixturetest.H) doc.Doc
		action   doc.Action
		reason   string
		decision bool
		want     []string
	}{
		{"submit rule", order(doc.StateDraft), doc.ActionSubmit, "", false, []string{"APR.Evaluate", "APR.Open"}},
		{"submit no rule receipt", receipt, doc.ActionSubmit, "", false, []string{"APR.Evaluate", "APR.Open", "APR.Record:approve"}},
		{"approve", order(doc.StatePending), doc.ActionApprove, "", true, []string{"APR.CheckApprover", "APR.Record:approve"}},
		{"reject", order(doc.StatePending), doc.ActionReject, "Rate too high", true, []string{"APR.CheckApprover", "APR.Record:reject"}},
		{"revise", order(doc.StateRejected), doc.ActionRevise, "", false, []string{"APR.Record:revise"}},
		{"withdraw", order(doc.StatePending), doc.ActionWithdraw, "", false, []string{"APR.Record:withdraw"}},
		{"save", order(doc.StateDraft), doc.ActionSave, "", false, nil},
		{"close", order(doc.StateApproved), doc.ActionClose, "Balance not needed", false, nil},
		{"cancel", order(doc.StateApproved), doc.ActionCancel, "Wrong supplier GSTIN", false, nil},
		{"amend", order(doc.StateCancelled), doc.ActionAmend, "", false, nil},
		{"delete", order(doc.StateDraft), doc.ActionDelete, "", false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := tc.setup(t, h)
			in := doc.ActionInput{Version: d.Version, Reason: tc.reason}
			if tc.action == doc.ActionSave {
				in = h.In(d)
				in.Values["remarks"] = "Deliver to Pune site"
			}
			p := guardActor(h, tc.action)
			if d.DocType == fixture.Receipt {
				p = h.Suresh
			}
			h.Approvals.Calls, h.Auth.Calls = nil, nil
			if _, err := h.Do(t, t.Context(), p, d.DocType, d.ID, tc.action, in); err != nil {
				t.Fatalf("%s: %v", tc.action, err)
			}
			if !slices.Equal(h.Approvals.Calls, tc.want) {
				t.Errorf("Approvals.Calls = %q, want %q", h.Approvals.Calls, tc.want)
			}
			if tc.decision {
				fixturetest.Eq(t, h.Auth.Calls, []string{"AUTH.Can:read"})
			}
			if tc.action != doc.ActionSave {
				for _, c := range h.Auth.Calls {
					if strings.HasPrefix(c, "AUTH.CheckValues") || strings.HasPrefix(c, "AUTH.CheckLink") {
						t.Errorf("%s made %q; only create and save check restrictions (CD-70)", tc.action, c)
					}
				}
			}
		})
	}

	t.Run("deny approve on a draft", func(t *testing.T) {
		h, d := guardOrder(t, doc.StateDraft)
		deny := errors.New("not your approval")
		h.Approvals.Deny = func(auth.Principal) error { return deny }
		_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})
		if !errors.Is(err, deny) {
			t.Errorf("err = %v, want the Deny error (CheckApprover before the guard)", err)
		}
	})

	t.Run("withdraw without submit right", func(t *testing.T) {
		h, d := guardOrder(t, doc.StatePending)
		h.Auth.Deny = func(_ auth.Principal, _ string, r auth.Right) bool { return r == "submit" }
		h.Approvals.Calls = nil
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionWithdraw, doc.ActionInput{Version: d.Version})
		if !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("err = %v, want auth.ErrForbidden", err)
		}
		if len(h.Approvals.Calls) != 0 {
			t.Errorf("Approvals.Calls = %q, want none", h.Approvals.Calls)
		}
	})
}

// termTransition is one expected transition row.
type termTransition struct {
	Action, From, To, Reason string
}

// DOC-083
func TestAudit_Transitions(t *testing.T) {
	h := fixturetest.New(t)
	res := h.RefPO(t)
	steps := []struct {
		action doc.Action
		actor  auth.Principal
		reason string
	}{
		{doc.ActionSubmit, h.Priya, ""},
		{doc.ActionWithdraw, h.Priya, ""},
		{doc.ActionSubmit, h.Priya, ""},
		{doc.ActionReject, h.Ravi, "Rate too high"},
		{doc.ActionRevise, h.Priya, ""},
		{doc.ActionSubmit, h.Priya, ""},
		{doc.ActionApprove, h.Ravi, ""},
		{doc.ActionClose, h.Priya, "Balance not needed"},
	}
	var ats []time.Time
	version := res.Version
	for i, s := range steps {
		at := time.Date(2026, 10, 24, 6, i, 0, 0, time.UTC)
		h.Clock.SetTime(at)
		ats = append(ats, at)
		r, err := h.Do(t, t.Context(), s.actor, fixture.Order, res.ID, s.action, doc.ActionInput{Version: version, Reason: s.reason})
		if err != nil {
			t.Fatalf("step %d %s: %v", i+1, s.action, err)
		}
		version = r.Version
	}

	var rows []audit.Entry
	for _, e := range h.Audit(t, fixture.Order, res.ID) {
		if e.Action != "create" && e.Action != "save" {
			rows = append(rows, e)
		}
	}
	var got []termTransition
	for _, e := range rows {
		got = append(got, termTransition{e.Action, e.From, e.To, e.Reason})
	}
	fixturetest.Eq(t, got, []termTransition{
		{"submit", "draft", "pending_approval", ""},
		{"withdraw", "pending_approval", "draft", ""},
		{"submit", "draft", "pending_approval", ""},
		{"reject", "pending_approval", "rejected", "Rate too high"},
		{"revise", "rejected", "draft", ""},
		{"submit", "draft", "pending_approval", ""},
		{"approve", "pending_approval", "approved", ""},
		{"close", "approved", "closed", "Balance not needed"},
	})
	if len(rows) != len(steps) {
		t.Fatalf("%d transition rows, want %d", len(rows), len(steps))
	}
	for i, e := range rows {
		if e.By != steps[i].actor.ID || !e.At.Equal(ats[i]) {
			t.Errorf("row %d (%s) By %d At %v; want %d, %v", i+1, e.Action, e.By, e.At, steps[i].actor.ID, ats[i])
		}
		hasNumber := slices.ContainsFunc(e.Changes, func(c audit.Change) bool {
			return c.Field == "number" && strings.Contains(c.After, "PO/26-27/00001")
		})
		if wantNumber := e.Action == "approve"; hasNumber != wantNumber {
			t.Errorf("row %d (%s): number change PO/26-27/00001 present = %v, want %v (changes %+v)", i+1, e.Action, hasNumber, wantNumber, e.Changes)
		}
	}
}

// Review Focus 1
func TestDoc_Reason_TrimmedAndCountedInRunes(t *testing.T) {
	h, d := guardOrder(t, doc.StateApproved)
	count, rows := h.AuditCount(t), orderRows(t, h)
	_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionCancel, doc.ActionInput{Version: d.Version, Reason: "  ab  "})
	termMsg(t, err, termCancelShort)
	termUnchanged(t, h, d, count, rows)

	long := strings.Repeat("क", 500) // 1,500 bytes, 500 characters
	res := termDo(t, h, t.Context(), h.Ravi, d, doc.ActionCancel, "  "+long+"  ")
	if res.State != doc.StateCancelled {
		t.Errorf("Result.State = %q, want cancelled", res.State)
	}
	after := h.Load(t, fixture.Order, d.ID)
	if after.State != doc.StateCancelled {
		t.Errorf("State = %q, want cancelled", after.State)
	}
	if after.StateReason != long {
		t.Errorf("StateReason = %q (%d bytes), want the 500 characters without surrounding spaces", after.StateReason, len(after.StateReason))
	}
}
