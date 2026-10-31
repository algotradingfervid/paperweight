package purchase_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-034 (PO spec §18; CD-94): every engine write on a PO posted with the
// version of a page opened before someone else changed it is DOC's version
// conflict (409, stale text naming the PO), and nothing changes: state,
// version and every row count. P10, P11 and P18 own their own stale cases.
func TestPO_StaleVersion(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	id := po.ID

	stale := func(what string, p auth.Principal, a doc.Action, old int, values map[string]string, reason string) {
		t.Helper()
		d := p2Load(t, env, id)
		before := p2CountAll(t, env)
		_, err := env.Engine.Do(ctx, p, purchase.DocType, id, a, doc.ActionInput{Version: old, Values: values, Reason: reason})
		want := fmt.Sprintf("Someone else changed %s after you opened it. Reload it and make your changes again.", naming.Label(d.Number, d.ID))
		var de *doc.Error
		if !errors.As(err, &de) || de.Kind != doc.KindStale || de.Status() != 409 || de.Message != want {
			t.Errorf("%s at old version %d (stored %d): err = %v; want 409 %q", what, old, d.Version, err, want)
		}
		p2SameCounts(t, what, before, env)
		if after := p2Load(t, env, id); after.State != d.State || after.Version != d.Version || after.Number != d.Number {
			t.Errorf("%s: state %q version %d number %q, want %q %d %q unchanged", what, after.State, after.Version, after.Number, d.State, d.Version, d.Number)
		}
	}
	step := func(what string, p auth.Principal, a doc.Action, reason string, values map[string]string, want doctype.State) {
		t.Helper()
		var lines []map[string]string
		if a == doc.ActionSave {
			lines = p2KeepLines(t, env, id)
		}
		res := p2Must(t, what)(env.Engine.Do(ctx, p, purchase.DocType, id, a,
			doc.ActionInput{Version: p2Version(t, env, id), Values: values, Reason: reason, Lines: lines}))
		if res.State != want {
			t.Fatalf("%s: state %q, want %q", what, res.State, want)
		}
	}

	// Draft: another tab saved.
	old := po.Version
	step("another save", env.Priya, doc.ActionSave, "", map[string]string{"remarks": "Second tab"}, doctype.StateDraft)
	stale("save", env.Priya, doc.ActionSave, old, map[string]string{"remarks": "First tab"}, "")
	stale("submit", env.Priya, doc.ActionSubmit, old, nil, "")
	stale("delete", env.Priya, doc.ActionDelete, old, nil, "")

	// Pending (₹94,164.00 needs the Purchase Manager): a page opened on the
	// draft before the submit.
	old = p2Version(t, env, id)
	step("submit", env.Priya, doc.ActionSubmit, "", nil, doctype.StatePending)
	stale("withdraw", env.Priya, doc.ActionWithdraw, old, nil, "")
	stale("approve", env.Ravi, doc.ActionApprove, old, nil, "")
	stale("reject", env.Ravi, doc.ActionReject, old, nil, "Rate too high")

	// Rejected.
	old = p2Version(t, env, id)
	step("reject", env.Ravi, doc.ActionReject, "Rate too high", nil, doctype.StateRejected)
	stale("revise", env.Priya, doc.ActionRevise, old, nil, "")

	// Approved.
	step("revise", env.Priya, doc.ActionRevise, "", nil, doctype.StateDraft)
	step("submit again", env.Priya, doc.ActionSubmit, "", nil, doctype.StatePending)
	old = p2Version(t, env, id)
	step("approve", env.Ravi, doc.ActionApprove, "", nil, doctype.StateApproved)
	stale("close", env.Priya, doc.ActionClose, old, nil, "Balance not needed")
	stale("cancel", env.Ravi, doc.ActionCancel, old, nil, "Wrong vendor")

	// Cancelled.
	old = p2Version(t, env, id)
	step("cancel", env.Ravi, doc.ActionCancel, "Wrong vendor", nil, doctype.StateCancelled)
	stale("amend", env.Priya, doc.ActionAmend, old, nil, "")
}
