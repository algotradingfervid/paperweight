package fixture_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// lifeReason is the reason every transition in the lifecycle table carries.
const lifeReason = "Supplier sent the wrong grade"

// lifeLine is the one order line of the lifecycle table's orders.
var lifeLine = fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10", Rate: "62.5000"}

// lifeDo runs action a on the stored document d with d's version and lifeReason.
func lifeDo(t *testing.T, h *fixturetest.H, d doc.Doc, a doc.Action) doc.Result {
	t.Helper()
	in := doc.ActionInput{Version: d.Version, Reason: lifeReason}
	if a == doc.ActionSave {
		in = h.In(d)
		in.Values["remarks"] = "Deliver to Pune site"
	}
	p := h.Priya
	if a == doc.ActionApprove || a == doc.ActionReject || a == doc.ActionCancel || a == doc.ActionAmend {
		p = h.Ravi
	}
	return h.MustDo(t, t.Context(), p, d.DocType, d.ID, a, in)
}

// DOC-004
func TestDoc_Transitions_Allowed(t *testing.T) {
	draft := func(t *testing.T, h *fixturetest.H) doc.Doc {
		res := h.NewOrder(t, h.Priya, lifeLine)
		return h.Load(t, fixture.Order, res.ID)
	}
	pending := func(t *testing.T, h *fixturetest.H) doc.Doc { return h.PendingOrder(t) }
	approved := func(t *testing.T, h *fixturetest.H) doc.Doc { return h.ApprovedOrder(t, lifeLine) }
	rejected := func(t *testing.T, h *fixturetest.H) doc.Doc {
		d := h.PendingOrder(t)
		lifeDo(t, h, d, doc.ActionReject)
		return h.Load(t, fixture.Order, d.ID)
	}
	cancelled := func(t *testing.T, h *fixturetest.H) doc.Doc {
		d := h.ApprovedOrder(t, lifeLine)
		lifeDo(t, h, d, doc.ActionCancel)
		return h.Load(t, fixture.Order, d.ID)
	}

	tests := []struct {
		name   string
		start  func(*testing.T, *fixturetest.H) doc.Doc
		from   doc.State
		action doc.Action
		want   doc.State
	}{
		{"draft+save", draft, doc.StateDraft, doc.ActionSave, doc.StateDraft},
		{"draft+submit no rule", draft, doc.StateDraft, doc.ActionSubmit, doc.StateApproved},
		{"draft+submit rule", func(t *testing.T, h *fixturetest.H) doc.Doc {
			return h.Load(t, fixture.Order, h.RefPO(t).ID) // ₹94,164.00 needs approval
		}, doc.StateDraft, doc.ActionSubmit, doc.StatePending},
		{"pending+approve", pending, doc.StatePending, doc.ActionApprove, doc.StateApproved},
		{"pending+reject", pending, doc.StatePending, doc.ActionReject, doc.StateRejected},
		{"pending+withdraw", pending, doc.StatePending, doc.ActionWithdraw, doc.StateDraft},
		{"rejected+revise", rejected, doc.StateRejected, doc.ActionRevise, doc.StateDraft},
		{"approved+cancel", approved, doc.StateApproved, doc.ActionCancel, doc.StateCancelled},
		{"approved+close", approved, doc.StateApproved, doc.ActionClose, doc.StateClosed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := tc.start(t, h)
			if d.State != tc.from {
				t.Fatalf("starting state = %q, want %q", d.State, tc.from)
			}
			count := h.AuditCount(t)
			res := lifeDo(t, h, d, tc.action)
			if res.ID != d.ID || res.State != tc.want {
				t.Errorf("Result ID, State = %d, %q; want %d, %q", res.ID, res.State, d.ID, tc.want)
			}
			after := h.Load(t, fixture.Order, d.ID)
			if after.State != tc.want {
				t.Errorf("stored state = %q, want %q", after.State, tc.want)
			}
			if after.Version != d.Version+1 || res.Version != after.Version {
				t.Errorf("Version stored %d, Result %d; want %d", after.Version, res.Version, d.Version+1)
			}
			if got := h.AuditCount(t); got != count+1 {
				t.Errorf("AuditCount = %d, want %d (one row for the action)", got, count+1)
			}
		})
	}

	t.Run("cancelled+amend", func(t *testing.T) {
		h := fixturetest.New(t)
		orig := cancelled(t, h)
		if orig.State != doc.StateCancelled {
			t.Fatalf("starting state = %q, want cancelled", orig.State)
		}
		res := lifeDo(t, h, orig, doc.ActionAmend)
		if res.ID == 0 || res.ID == orig.ID || res.State != doc.StateDraft {
			t.Fatalf("Result = %+v, want a new document in draft", res)
		}
		if got := h.Load(t, fixture.Order, orig.ID).State; got != doc.StateCancelled {
			t.Errorf("original state = %q, want cancelled", got)
		}
		amended := h.Load(t, fixture.Order, res.ID)
		if amended.State != doc.StateDraft || amended.AmendedFromID != orig.ID {
			t.Errorf("new document State, AmendedFromID = %q, %d; want draft, %d", amended.State, amended.AmendedFromID, orig.ID)
		}
	})

	t.Run("draft+delete", func(t *testing.T) {
		h := fixturetest.New(t)
		d := draft(t, h)
		lifeDo(t, h, d, doc.ActionDelete)
		err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
			_, err := h.Engine.Load(t.Context(), tx, fixture.Order, d.ID)
			return err
		})
		fixturetest.ErrIs(t, err, doc.KindNotFound, "")
	})
}
