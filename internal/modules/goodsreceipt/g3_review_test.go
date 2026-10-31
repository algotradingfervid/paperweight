package goodsreceipt_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// g3GRNRule adds an approval rule for goodsreceipt.Receipt from ₹10,000.00
// (accepted_value, the Amount hook) with the Administrator role as approver:
// no fixture role but Administrator holds approve on a GRN (spec §10), so a
// GRN above it waits as pending until the Administrator approves it.
func g3GRNRule(t *testing.T, env *grntest.Env) {
	t.Helper()
	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: goodsreceipt.DocType, FromAmount: 10_000_00, ApproverType: approval.ApproverRole, Role: "Administrator",
	}); err != nil {
		t.Fatalf("add a Goods Receipt rule: %v", err)
	}
}

// g3Decide runs approve or reject (with a reason) on pending GRN id as the
// Administrator and returns the stored GRN.
func g3Decide(t *testing.T, env *grntest.Env, id int64, a doctype.Action, reason string) doc.Doc {
	t.Helper()
	if _, err := env.Engine.Do(t.Context(), env.Admin, goodsreceipt.DocType, id, a,
		doc.ActionInput{Version: env.Load(t, goodsreceipt.DocType, id).Version, Reason: reason}); err != nil {
		t.Fatalf("%s GRN %d as the Administrator: %v %+v", a, id, err, g1FieldErrors(err))
	}
	return env.Load(t, goodsreceipt.DocType, id)
}

// G3 review row (GRN spec §5; PO spec §6.3; plan G3 Do 1: accepted counts
// only on approved and closed GRNs), a walk table: a pending GRN accepting
// all 1,200 kg of line 1 counts for nothing, so when a GRN for line 2 is
// approved the PO is part_received from line 2 alone (counting the pending
// GRN would make it fully_received); a rejected GRN changes nothing; the
// Administrator's approve of the pending GRN, a second path into the
// effects (approve from pending, lessons §1 item 6), fills line 1; the
// cancels walk it back.
func TestGRN_Review_PendingNotCounted(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g3GRNRule(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	var waiting, line2 doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "GRN for 1,200 kg of line 1 (₹75,000) waits for approval", do: func(t *testing.T) {
			waiting = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}}, doctype.StatePending)
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
		{what: "GRN for 50 kg of line 2 (₹4,800) approved on submit", do: func(t *testing.T) {
			line2 = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"0", "50"}, pending: []string{"1200", "0"}, status: "part_received"}},
		{what: "a GRN for 400 kg of line 1 (₹25,000) rejected while pending", do: func(t *testing.T) {
			r := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "400"}}, doctype.StatePending)
			if got := g3Decide(t, env, r.ID, doctype.ActionReject, "Wrong truck"); got.State != doctype.StateRejected {
				t.Errorf("rejected GRN state = %s, want rejected", got.State)
			}
		}, want: g3Want{received: []string{"0", "50"}, pending: []string{"1200", "0"}, status: "part_received"}},
		{what: "the Administrator approves the waiting GRN", do: func(t *testing.T) {
			if got := g3Decide(t, env, waiting.ID, doctype.ActionApprove, ""); got.State != doctype.StateApproved {
				t.Errorf("approved GRN state = %s, want approved", got.State)
			}
		}, want: g3Want{received: []string{"1200", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "line 2's GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, line2.ID, "Wrong entry")
		}, want: g3Want{received: []string{"1200", "0"}, pending: []string{"0", "50"}, status: "part_received"}},
		{what: "the approved-from-pending GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, waiting.ID, "Wrong entry")
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
	})
}

// G3 review row (GRN spec §5; plan G3 Do 1), a walk table: a draft GRN for
// all 1,200 kg of line 1 counts for nothing, so approving line 2's GRN
// leaves the PO part_received (counting the draft would make it
// fully_received); once the draft is submitted it counts.
func TestGRN_Review_DraftNotCounted(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	var draft, line2 doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "draft GRN for 1,200 kg of line 1", do: func(t *testing.T) {
			draft = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}}, doctype.StateDraft)
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
		{what: "GRN for 50 kg of line 2 approved", do: func(t *testing.T) {
			line2 = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"0", "50"}, pending: []string{"1200", "0"}, status: "part_received"}},
		{what: "line 2's GRN cancelled with the draft still open", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, line2.ID, "Wrong entry")
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
		{what: "the draft submitted", do: func(t *testing.T) {
			if _, err := g2Submit(t, env, env.Suresh, draft.ID); err != nil {
				t.Fatalf("submit the draft: %v %+v", err, g1FieldErrors(err))
			}
		}, want: g3Want{received: []string{"1200", "0"}, pending: []string{"0", "50"}, status: "part_received"}},
	})
}
