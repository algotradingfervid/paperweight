package goodsreceipt_test

import (
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/types"
)

// G5 review row (GRN spec §4; plan G5 Do 1: pending counts accepted on
// other approved or closed GRNs only; CD-279 (7)): with a GRN approval rule
// (Administrator, from ₹10,000.00; g3GRNRule) a GRN accepting all 1,200 kg
// of line 1 waits as pending_approval and lowers neither Warnings' nor
// LineWarnings' pending: a draft of 1,200 doesn't warn, and 1,250 warns
// against the full 1,200.
func TestGRN_Review_WarningsIgnorePending(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g3GRNRule(t, env)
	l1 := po.Lines[0].ID
	const no = "PO/26-27/00001"
	waiting := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}}, doctype.StatePending)
	if waiting.State != doctype.StatePending {
		t.Fatalf("the GRN of 1,200 kg is %s, want pending_approval", waiting.State)
	}

	exact := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}}, doctype.StateDraft)
	g5WantList(t, "Warnings: draft of 1,200 beside a pending GRN of 1,200", g5Warnings(t, env, exact.ID))
	over := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1250"}}, doctype.StateDraft)
	g5WantList(t, "Warnings: draft of 1,250 beside a pending GRN of 1,200", g5Warnings(t, env, over.ID),
		g5Over(1, "1,250 KGS", "1,200 KGS", no))

	g5WantMap(t, "LineWarnings: 1,200 typed beside a pending GRN of 1,200",
		g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: g3Q("1200")}), nil)
	g5WantMap(t, "LineWarnings: 1,250 typed beside a pending GRN of 1,200",
		g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: g3Q("1250")}),
		map[int64]string{l1: g5Over(1, "1,250 KGS", "1,200 KGS", no)})
}

// G5 review rows (AD-12 ruling on CD-272 Gaps 4 and 13): a GRN with no
// lines gets neither "All lines received as ordered." nor the e-way reject
// hint, even when its PO has nothing pending or an e-way bill no. is set;
// a draft with no PO (blanked by a step save, as GRN-004 does) gets no
// notes either.
func TestGRN_Review_NoLinesNoNotes(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	eway := map[string]string{"eway_bill_no": "331000123456"}
	empty := func() int64 {
		t.Helper()
		h := g2Header(t, env, po)
		maps.Copy(h, eway)
		return g2MustCreate(t, env, env.Suresh, h).ID
	}

	g5WantList(t, "no lines, PO with pending, e-way no. set", g5Notes(t, env, empty()))

	noPO := empty()
	res, err := g2Save(t, env, env.Suresh, noPO, map[string]string{"purchase_order_id": "", "remarks": "PO not known"}, nil, "remarks")
	g2WantProblems(t, "step save of a blank PO", res, err, doctype.FieldError{Field: "purchase_order_id", Message: g2MsgChoosePO})
	if got := g2Column(t, env, noPO, "purchase_order_id"); got != "" {
		t.Fatalf("purchase_order_id = %q after the step save, want blank", got)
	}
	g5WantList(t, "no PO, no lines, e-way no. set", g5Notes(t, env, noPO))

	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "1200"}, {POLineID: po.Lines[1].ID, Received: "50"}}, doctype.StateApproved)
	g5WantList(t, "no lines, fully received PO, e-way no. set", g5Notes(t, env, empty()))
}
