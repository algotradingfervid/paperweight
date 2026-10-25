package doc_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// guardPO is the number ForceState gives approved, closed and cancelled orders.
const guardPO = "PO/26-27/00001"

// guardReason is a valid reason for actions that take one, so a refusal can't
// come from a missing reason.
const guardReason = "Supplier revised the rate"

// guardStates is every document state, in table order.
var guardStates = []doc.State{
	doc.StateDraft, doc.StatePending, doc.StateApproved,
	doc.StateRejected, doc.StateClosed, doc.StateCancelled,
}

// guardStateLabel is the StateLabel users see for each state.
var guardStateLabel = map[doc.State]string{
	doc.StateDraft:     "Draft",
	doc.StatePending:   "Waiting for approval",
	doc.StateApproved:  "Approved",
	doc.StateClosed:    "Closed",
	doc.StateRejected:  "Rejected",
	doc.StateCancelled: "Cancelled",
}

// guardNumber is the number ForceState sets for s: approved, closed and
// cancelled orders are numbered, the others stay "Draft <id>".
func guardNumber(s doc.State) string {
	switch s {
	case doc.StateApproved, doc.StateClosed, doc.StateCancelled:
		return guardPO
	}
	return ""
}

// guardOrder returns a fresh harness with the reference PO forced into s.
func guardOrder(t *testing.T, s doc.State) (*fixturetest.H, doc.Doc) {
	t.Helper()
	h := fixturetest.New(t)
	res := h.RefPO(t)
	h.ForceState(t, fixture.Order, res.ID, s, guardNumber(s))
	return h, h.Load(t, fixture.Order, res.ID)
}

// guardX is the display name of d.
func guardX(d doc.Doc) string {
	if d.Number != "" {
		return d.Number
	}
	return fmt.Sprintf("Draft %d", d.ID)
}

// guardActor is who sends action a: Ravi decides, Priya does the rest.
func guardActor(h *fixturetest.H, a doc.Action) auth.Principal {
	if a == doc.ActionApprove || a == doc.ActionReject {
		return h.Ravi
	}
	return h.Priya
}

// guardInput is a current-version request for a on d; a save also changes remarks.
func guardInput(h *fixturetest.H, d doc.Doc, a doc.Action) doc.ActionInput {
	if a == doc.ActionSave {
		in := h.In(d)
		in.Values["remarks"] = "Deliver to Pune site"
		return in
	}
	return doc.ActionInput{Version: d.Version, Reason: guardReason}
}

// guardRefused runs a on d as p and asserts a KindRefused error with want
// (any of wants when more than one), and that the document (state, version,
// header, lines), the fixture_order row count and AuditCount are unchanged.
func guardRefused(t *testing.T, h *fixturetest.H, p auth.Principal, d doc.Doc, a doc.Action, in doc.ActionInput, wants ...string) {
	t.Helper()
	before := h.Load(t, d.DocType, d.ID)
	count := h.AuditCount(t)
	rows := orderRows(t, h)
	_, err := h.Do(t, t.Context(), p, d.DocType, d.ID, a, in)
	if len(wants) == 1 {
		fixturetest.ErrIs(t, err, doc.KindRefused, wants[0])
	} else {
		fixturetest.ErrIs(t, err, doc.KindRefused, "")
		var de *doc.Error
		if errors.As(err, &de) && !slices.Contains(wants, de.Message) {
			t.Errorf("Message = %q, want one of %q", de.Message, wants)
		}
	}
	after := h.Load(t, d.DocType, d.ID)
	fixturetest.Eq(t, after, before)
	if got := h.AuditCount(t); got != count {
		t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
	}
	if got := orderRows(t, h); got != rows {
		t.Errorf("fixture_order rows = %d, want %d (unchanged)", got, rows)
	}
}

// guardAppend writes one transition row for d through audit.Store.
func guardAppend(t *testing.T, h *fixturetest.H, d doc.Doc, action string, from, to doc.State, by types.UserID) {
	t.Helper()
	e := audit.Entry{
		DocType: d.DocType, DocID: d.ID, Action: action, From: string(from), To: string(to),
		By: by, At: time.Date(2026, 10, 24, 5, 30, 0, 0, time.UTC),
	}
	err := h.DB.Write(t.Context(), func(tx *db.Tx) error { return audit.Store{}.Append(t.Context(), tx, e) })
	if err != nil {
		t.Fatalf("audit.Store.Append(%s): %v", action, err)
	}
}

// guardRefusals is the plan's Task 8 table: every refused action per state,
// with its message (%[1]s is the display name, %[2]s the state label).
var guardRefusals = map[doc.State]map[doc.Action]string{
	doc.StateDraft: {
		doc.ActionWithdraw: "%[1]s hasn't been submitted, so there is nothing to withdraw.",
		doc.ActionApprove:  "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionReject:   "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionRevise:   "Only rejected documents can be revised.",
		doc.ActionClose:    "Only approved documents can be closed.",
		doc.ActionCancel:   "Only approved documents can be cancelled. Delete the draft instead.",
		doc.ActionAmend:    "Only cancelled documents can be amended.",
	},
	doc.StatePending: {
		doc.ActionSave:   "%[1]s is waiting for approval and can't be changed. Withdraw it to make changes.",
		doc.ActionSubmit: "%[1]s is waiting for approval and can't be changed. Withdraw it to make changes.",
		doc.ActionRevise: "%[1]s is waiting for approval and can't be changed. Withdraw it to make changes.",
		doc.ActionClose:  "Only approved documents can be closed.",
		doc.ActionCancel: "%[1]s is waiting for approval. Withdraw it first if it needs changes.",
		doc.ActionAmend:  "Only cancelled documents can be amended.",
		doc.ActionDelete: "%[1]s is waiting for approval and can't be deleted. Withdraw it first, then delete it.",
	},
	doc.StateApproved: {
		doc.ActionSave:     "%[1]s is approved and can't be edited. Cancel and amend it to make changes.",
		doc.ActionSubmit:   "%[1]s is approved and can't be edited. Cancel and amend it to make changes.",
		doc.ActionRevise:   "%[1]s is approved and can't be edited. Cancel and amend it to make changes.",
		doc.ActionWithdraw: "%[1]s has already been approved and can't be withdrawn. Cancel and amend it to make changes.",
		doc.ActionApprove:  "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionReject:   "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionAmend:    "Only cancelled documents can be amended. Cancel %[1]s first.",
		doc.ActionDelete:   "Approved documents can't be deleted. Cancel it instead.",
	},
	doc.StateRejected: {
		doc.ActionSave:     "%[1]s was rejected. Revise it to move it back to draft.",
		doc.ActionSubmit:   "%[1]s was rejected. Revise it to move it back to draft.",
		doc.ActionCancel:   "%[1]s was rejected. Revise it to move it back to draft.",
		doc.ActionWithdraw: "%[1]s was rejected. Revise it to move it back to draft.",
		doc.ActionApprove:  "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionReject:   "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionClose:    "Only approved documents can be closed.",
		doc.ActionAmend:    "Only cancelled documents can be amended.",
		doc.ActionDelete:   "Rejected documents can't be deleted. Revise it to move it back to draft, then delete it.",
	},
	doc.StateClosed: {
		doc.ActionSave:     "%[1]s is closed and can't be changed.",
		doc.ActionSubmit:   "%[1]s is closed and can't be changed.",
		doc.ActionWithdraw: "%[1]s is closed and can't be changed.",
		doc.ActionRevise:   "%[1]s is closed and can't be changed.",
		doc.ActionClose:    "%[1]s is closed and can't be changed.",
		doc.ActionCancel:   "%[1]s is closed and can't be changed.",
		doc.ActionAmend:    "%[1]s is closed and can't be changed.",
		doc.ActionApprove:  "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionReject:   "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionDelete:   "Approved documents can't be deleted. Cancel it instead.",
	},
	doc.StateCancelled: {
		doc.ActionSave:     "%[1]s is cancelled and can't be changed. Amend it to make a corrected copy.",
		doc.ActionSubmit:   "%[1]s is cancelled and can't be changed. Amend it to make a corrected copy.",
		doc.ActionWithdraw: "%[1]s is cancelled and can't be changed. Amend it to make a corrected copy.",
		doc.ActionRevise:   "%[1]s is cancelled and can't be changed. Amend it to make a corrected copy.",
		doc.ActionCancel:   "%[1]s is cancelled and can't be changed. Amend it to make a corrected copy.",
		doc.ActionClose:    "Only approved documents can be closed.",
		doc.ActionApprove:  "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionReject:   "%[1]s is not waiting for approval. It is %[2]s.",
		doc.ActionDelete:   "Cancelled documents are kept for the record and can't be deleted.",
	},
}

// guardAllowed is the plan's allowed list; with guardRefusals it covers every
// (state, action) pair.
var guardAllowed = map[doc.State][]doc.Action{
	doc.StateDraft:     {doc.ActionSave, doc.ActionSubmit, doc.ActionDelete},
	doc.StatePending:   {doc.ActionWithdraw, doc.ActionApprove, doc.ActionReject},
	doc.StateApproved:  {doc.ActionClose, doc.ActionCancel},
	doc.StateRejected:  {doc.ActionRevise},
	doc.StateClosed:    {},
	doc.StateCancelled: {doc.ActionAmend},
}

// DOC-005
func TestDoc_Transitions_Refused(t *testing.T) {
	for _, s := range guardStates {
		for _, a := range doctype.Actions() {
			_, refused := guardRefusals[s][a]
			if refused == slices.Contains(guardAllowed[s], a) {
				t.Fatalf("table error: (%s, %s) must be either refused or allowed", s, a)
			}
		}
	}
	for _, s := range guardStates {
		t.Run(string(s), func(t *testing.T) {
			h, d := guardOrder(t, s)
			x := guardX(d)
			for _, a := range doctype.Actions() {
				tmpl, ok := guardRefusals[s][a]
				if !ok {
					continue
				}
				t.Run(string(a), func(t *testing.T) {
					want := fmt.Sprintf(tmpl, x, guardStateLabel[s])
					guardRefused(t, h, guardActor(h, a), d, a, guardInput(h, d, a), want)
				})
			}
		})
	}

	// Decision variants (CD-26): one transition row appended through audit.Store.
	t.Run("approved by Ravi, Meena approves or rejects", func(t *testing.T) {
		h, d := guardOrder(t, doc.StateApproved)
		guardAppend(t, h, d, "approve", doc.StatePending, doc.StateApproved, 3)
		want := "PO/26-27/00001 is no longer waiting for approval. It was approved by Ravi Kulkarni."
		for _, a := range []doc.Action{doc.ActionApprove, doc.ActionReject} {
			guardRefused(t, h, h.Meena, d, a, guardInput(h, d, a), want)
		}
	})
	t.Run("rejected by Ravi, Meena rejects", func(t *testing.T) {
		h, d := guardOrder(t, doc.StateRejected)
		guardAppend(t, h, d, "reject", doc.StatePending, doc.StateRejected, 3)
		want := fmt.Sprintf("Draft %d is no longer waiting for approval. It was rejected by Ravi Kulkarni.", d.ID)
		guardRefused(t, h, h.Meena, d, doc.ActionReject, guardInput(h, d, doc.ActionReject), want)
	})
	t.Run("withdrawn by Priya, Ravi approves", func(t *testing.T) {
		h, d := guardOrder(t, doc.StateDraft)
		guardAppend(t, h, d, "withdraw", doc.StatePending, doc.StateDraft, 4)
		want := fmt.Sprintf("Draft %d is no longer waiting for approval. It was withdrawn by Priya Shah.", d.ID)
		guardRefused(t, h, h.Ravi, d, doc.ActionApprove, guardInput(h, d, doc.ActionApprove), want)
	})
	t.Run("auto-approved receipt", func(t *testing.T) {
		h := fixturetest.New(t)
		o := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10.000", Rate: "62.5000"})
		h.ForceState(t, fixture.Order, o.ID, doc.StateApproved, guardPO)
		line := h.Load(t, fixture.Order, o.ID).Lines[0]
		r := h.NewReceipt(t, h.Suresh, fixturetest.RL{OrderLineID: line.ID, Qty: "5.000"})
		h.ForceState(t, fixture.Receipt, r.ID, doc.StateApproved, "GR/26-27/00001")
		d := h.Load(t, fixture.Receipt, r.ID)
		guardAppend(t, h, d, "submit", doc.StateDraft, doc.StateApproved, 5)
		guardRefused(t, h, h.Ravi, d, doc.ActionApprove, guardInput(h, d, doc.ActionApprove),
			"GR/26-27/00001 is not waiting for approval. It is Approved.")
	})
	t.Run("approved by Ravi, then closed", func(t *testing.T) {
		// The latest transition is a close, not a person's approval decision,
		// and the approve row's To no longer equals the state.
		h, d := guardOrder(t, doc.StateClosed)
		guardAppend(t, h, d, "approve", doc.StatePending, doc.StateApproved, 3)
		guardAppend(t, h, d, "close", doc.StateApproved, doc.StateClosed, 4)
		guardRefused(t, h, h.Meena, d, doc.ActionApprove, guardInput(h, d, doc.ActionApprove),
			"PO/26-27/00001 is not waiting for approval. It is Closed.")
	})
}

// DOC-016
func TestDoc_Closed_IsTerminal(t *testing.T) {
	h, d := guardOrder(t, doc.StateClosed)
	closed := "PO/26-27/00001 is closed and can't be changed."
	for _, a := range []doc.Action{doc.ActionCancel, doc.ActionSave, doc.ActionWithdraw, doc.ActionAmend} {
		t.Run(string(a), func(t *testing.T) {
			guardRefused(t, h, h.Priya, d, a, guardInput(h, d, a), closed)
		})
	}
	t.Run("approve", func(t *testing.T) {
		guardRefused(t, h, h.Ravi, d, doc.ActionApprove, guardInput(h, d, doc.ActionApprove),
			"PO/26-27/00001 is not waiting for approval. It is Closed.")
	})
	t.Run("reopen", func(t *testing.T) {
		// DOC-016 gives the closed text; the plan's guard gives unknown actions
		// "{X} can't be changed that way." Either is a refusal of reopen.
		guardRefused(t, h, h.Priya, d, doc.Action("reopen"), doc.ActionInput{Version: d.Version},
			closed, "PO/26-27/00001 can't be changed that way.")
	})
	if slices.Contains(doctype.Actions(), doc.Action("reopen")) {
		t.Errorf("Actions() = %v, must not contain reopen (R6)", doctype.Actions())
	}
}

// DOC-048
func TestDoc_Lines_FrozenAfterDraft(t *testing.T) {
	for _, s := range []doc.State{doc.StatePending, doc.StateApproved, doc.StateRejected, doc.StateClosed, doc.StateCancelled} {
		t.Run(string(s), func(t *testing.T) {
			h, d := guardOrder(t, s)
			want := fmt.Sprintf(guardRefusals[s][doc.ActionSave], guardX(d), guardStateLabel[s])
			edits := map[string]func(in *doc.ActionInput){
				"edit": func(in *doc.ActionInput) { in.Lines[0]["qty"] = "1300.000" },
				"add": func(in *doc.ActionInput) {
					in.Lines = append(in.Lines, map[string]string{
						"item_id": fmt.Sprint(h.M.Item["BOLT-M12"]), "qty": "5.000", "rate": "12.0000",
					})
				},
				"remove": func(in *doc.ActionInput) { in.Lines = in.Lines[:1] },
			}
			for _, name := range []string{"edit", "add", "remove"} {
				t.Run(name, func(t *testing.T) {
					in := h.In(d)
					edits[name](&in)
					guardRefused(t, h, h.Priya, d, doc.ActionSave, in, want)
					fixturetest.Eq(t, h.Load(t, fixture.Order, d.ID).Lines, d.Lines)
				})
			}
		})
	}
}

// DOC-049
func TestDoc_Submitted_HeaderImmutable(t *testing.T) {
	h, d := guardOrder(t, doc.StateApproved)
	want := "PO/26-27/00001 is approved and can't be edited. Cancel and amend it to make changes."
	for field, v := range map[string]string{
		"supplier_id": fmt.Sprint(h.M.Party["BHARAT"]),
		"order_date":  "2026-10-25",
		"remarks":     "Deliver to Pune site",
	} {
		t.Run(field, func(t *testing.T) {
			in := h.In(d)
			in.Values[field] = v
			guardRefused(t, h, h.Priya, d, doc.ActionSave, in, want)
			fixturetest.Eq(t, h.Load(t, fixture.Order, d.ID).Header, d.Header)
		})
	}
}

// DOC-062
func TestDoc_Delete_OnlyDraft(t *testing.T) {
	for _, s := range []doc.State{doc.StatePending, doc.StateRejected, doc.StateApproved, doc.StateClosed, doc.StateCancelled} {
		t.Run(string(s), func(t *testing.T) {
			h, d := guardOrder(t, s)
			want := map[doc.State]string{
				doc.StatePending:   fmt.Sprintf("Draft %d is waiting for approval and can't be deleted. Withdraw it first, then delete it.", d.ID),
				doc.StateRejected:  "Rejected documents can't be deleted. Revise it to move it back to draft, then delete it.",
				doc.StateApproved:  "Approved documents can't be deleted. Cancel it instead.",
				doc.StateClosed:    "Approved documents can't be deleted. Cancel it instead.",
				doc.StateCancelled: "Cancelled documents are kept for the record and can't be deleted.",
			}[s]
			guardRefused(t, h, h.Priya, d, doc.ActionDelete, guardInput(h, d, doc.ActionDelete), want)
			var lines int
			if err := h.DB.SQL().QueryRowContext(t.Context(),
				`SELECT COUNT(*) FROM fixture_order_line WHERE parent_id = ?`, d.ID).Scan(&lines); err != nil {
				t.Fatalf("count lines: %v", err)
			}
			if lines != len(d.Lines) {
				t.Errorf("fixture_order_line rows = %d, want %d", lines, len(d.Lines))
			}
		})
	}
}

// DOC-070
func TestDoc_Concurrency_StaleTransition(t *testing.T) {
	for _, c := range []struct {
		a doc.Action
		s doc.State
	}{
		{doc.ActionSubmit, doc.StateDraft},
		{doc.ActionWithdraw, doc.StatePending},
		{doc.ActionApprove, doc.StatePending},
		{doc.ActionReject, doc.StatePending},
		{doc.ActionRevise, doc.StateRejected},
		{doc.ActionCancel, doc.StateApproved},
		{doc.ActionClose, doc.StateApproved},
		{doc.ActionAmend, doc.StateCancelled},
		{doc.ActionDelete, doc.StateDraft},
	} {
		t.Run(string(c.a), func(t *testing.T) {
			h, d := guardOrder(t, c.s)
			h.Exec(t, `UPDATE fixture_order SET version = 6 WHERE id = ?`, d.ID)
			before := h.Load(t, fixture.Order, d.ID)
			count := h.AuditCount(t)
			rows := orderRows(t, h)
			_, err := h.Do(t, t.Context(), guardActor(h, c.a), fixture.Order, d.ID, c.a,
				doc.ActionInput{Version: 5, Reason: guardReason})
			want := fmt.Sprintf("Someone else changed %s after you opened it. Reload it and make your changes again.", guardX(d))
			de := wantDocErr(t, err, doc.KindStale, want)
			if de.Status() != 409 {
				t.Errorf("Status = %d, want 409", de.Status())
			}
			fixturetest.Eq(t, h.Load(t, fixture.Order, d.ID), before)
			if before.Version != 6 || before.State != c.s {
				t.Errorf("setup: version %d state %s, want 6 %s", before.Version, before.State, c.s)
			}
			if got := h.AuditCount(t); got != count {
				t.Errorf("AuditCount = %d, want %d (unchanged)", got, count)
			}
			if got := orderRows(t, h); got != rows {
				t.Errorf("fixture_order rows = %d, want %d (unchanged)", got, rows)
			}
		})
	}
}
