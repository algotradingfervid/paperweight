package goodsreceipt_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G5 Do 1 (GRN spec §4; CD-234, CD-238, CD-279 (7)), no test ID:
// Warnings compares each line's accepted with the PO line's pending, where
// pending is purchase.Pending(qty, already_received, accepted on OTHER
// approved GRNs). Accepted over pending warns; received over pending with
// the excess rejected doesn't (CD-238); exactly pending doesn't; drafts and
// cancelled GRNs don't count; this GRN's own accepted never counts, as a
// draft or approved (CD-279 (7)). Each GRN receives only PO line 2, so the
// text says "Line 2:" at posted position 1 (AD-12 PO line number ruling).
func TestGRN_Warnings_OverPending(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l2 := po.Lines[1].ID // WIRE-18G, 50 kg ordered: each GRN receives only PO line 2, at position 1
	const no = "PO/26-27/00001"
	line := func(received string) []grntest.Line { return []grntest.Line{{POLineID: l2, Received: received}} }

	over := env.GRN(t, env.Suresh, po.ID, line("60"), doctype.StateDraft)
	g5WantList(t, "draft accepting 60 of 50 pending", g5Warnings(t, env, over.ID),
		g5Over(2, "60 KGS", "50 KGS", no))

	rejected := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "60", Rejected: "10", Reason: "Bent"}}, doctype.StateDraft)
	g5WantList(t, "60 received with 10 rejected (CD-238)", g5Warnings(t, env, rejected.ID))

	exact := env.GRN(t, env.Suresh, po.ID, line("50"), doctype.StateDraft)
	g5WantList(t, "accepting exactly the 50 pending, other drafts not counted", g5Warnings(t, env, exact.ID))

	first := env.GRN(t, env.Suresh, po.ID, line("20"), doctype.StateApproved)
	g5WantList(t, "approved GRN of 20: its own accepted isn't counted", g5Warnings(t, env, first.ID))
	g5WantList(t, "draft of 60 after 20 approved elsewhere", g5Warnings(t, env, over.ID),
		g5Over(2, "60 KGS", "30 KGS", no))
	g5WantList(t, "draft of 50 after 20 approved elsewhere", g5Warnings(t, env, exact.ID),
		g5Over(2, "50 KGS", "30 KGS", no))

	second := env.GRN(t, env.Suresh, po.ID, line("40"), doctype.StateApproved)
	g5WantList(t, "approved GRN of 40 with 20 approved elsewhere: only the other counts", g5Warnings(t, env, second.ID),
		g5Over(2, "40 KGS", "30 KGS", no))

	env.Cancel(t, env.Ravi, first.ID, "Counted twice")
	g5WantList(t, "approved GRN of 40 after the other is cancelled", g5Warnings(t, env, second.ID))
}

// MOD-GRN G5 Do 1 (GRN spec §4; CD-238), no test ID: on a fully received
// PO any accepted quantity warns against 0 pending, each line in line
// order; a fully rejected line doesn't.
func TestGRN_Warnings_FullyReceived(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	const no = "PO/26-27/00001"
	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}, {POLineID: l2, Received: "50"}}, doctype.StateApproved)

	both := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "10"}, {POLineID: l2, Received: "0.5"}}, doctype.StateDraft)
	g5WantList(t, "10 kg and 0.5 kg on a fully received PO", g5Warnings(t, env, both.ID),
		g5Over(1, "10 KGS", "0 KGS", no), g5Over(2, "0.5 KGS", "0 KGS", no))

	only2 := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "0.5"}}, doctype.StateDraft)
	g5WantList(t, "0.5 kg on PO line 2 alone (position 1)", g5Warnings(t, env, only2.ID),
		g5Over(2, "0.5 KGS", "0 KGS", no))

	gone := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "10", Rejected: "10", Reason: "Rusted"}}, doctype.StateDraft)
	g5WantList(t, "10 kg received and all rejected on a fully received PO", g5Warnings(t, env, gone.ID))
}

// MOD-GRN G5 Do 3 (GRN spec §4, §11; CD-238, CD-279 (7)), no test ID:
// LineWarnings warns on typed accepted quantities, keyed by PO line id, with
// {n} the PO line number (the step's cards are PO lines; UX §5.3). Accepted
// above pending warns; at or below doesn't, so an excess the user rejects
// (1,250 received, 50 rejected: 1,200 accepted) doesn't; GRN grnID's own
// accepted never counts, even approved; a fully received PO warns on any
// accepted quantity above 0.
func TestGRN_LineWarnings_OverPending(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	const no = "PO/26-27/00001"
	q := g3Q

	g5WantMap(t, "1,250 typed on line 1", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: q("1250"), l2: q("50")}),
		map[int64]string{l1: g5Over(1, "1,250 KGS", "1,200 KGS", no)})
	g5WantMap(t, "51 typed on line 2", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l2: q("51")}),
		map[int64]string{l2: g5Over(2, "51 KGS", "50 KGS", no)})
	g5WantMap(t, "1,200 accepted (1,250 with 50 rejected) and 50", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: q("1200"), l2: q("50")}), nil)

	draft := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "900"}}, doctype.StateDraft)
	g5WantMap(t, "1,200 typed on the draft of 900", g5LineWarnings(t, env, po.ID, draft.ID, map[int64]types.Qty{l1: q("1200")}), nil)

	first := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "400"}}, doctype.StateApproved)
	g5WantMap(t, "801 typed with 400 approved on another GRN", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: q("801")}),
		map[int64]string{l1: g5Over(1, "801 KGS", "800 KGS", no)})
	g5WantMap(t, "801 typed for the approved GRN of 400 itself", g5LineWarnings(t, env, po.ID, first.ID, map[int64]types.Qty{l1: q("801")}), nil)
	g5WantMap(t, "800 typed with 400 approved on another GRN", g5LineWarnings(t, env, po.ID, draft.ID, map[int64]types.Qty{l1: q("800")}), nil)

	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "800"}, {POLineID: l2, Received: "50"}}, doctype.StateApproved)
	g5WantMap(t, "fully received: 0.001 and 0 typed", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: q("0.001"), l2: 0}),
		map[int64]string{l1: g5Over(1, "0.001 KGS", "0 KGS", no)})
}

// GRN-016 (GRN spec §4; PO spec §3.2, §12; CD-276): a cutover PO's
// already_received counts toward pending and so toward the warning: line 1
// has 1,000 of 1,200 already received, so accepting 300 warns against 200
// pending, on the stored GRN (draft and approved) and on the step. After
// approval pending is 0, received_qty 300 and already_received still 1,000;
// the cancel gives the 300 back (do and undo, CD-272 pattern 4).
func TestGRN_AlreadyReceivedCounts(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD/145"}),
		purchasetest.OrderLines(
			map[string]string{"qty": "1200", "price": "62.5", "already_received": "1000"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
		))
	if po.Number != "PO/26-27/00001" {
		t.Fatalf("cutover PO = %q, want PO/26-27/00001", po.Number)
	}
	l1 := po.Lines[0].ID
	warn := g5Over(1, "300 KGS", "200 KGS", "PO/26-27/00001")

	g5WantMap(t, "300 typed on the step", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: g3Q("300")}), map[int64]string{l1: warn})
	g5WantMap(t, "200 typed on the step", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{l1: g3Q("200")}), nil)

	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "300"}}, doctype.StateDraft)
	g5WantList(t, "draft accepting 300", g5Warnings(t, env, grn.ID), warn)

	if _, err := g2Submit(t, env, env.Suresh, grn.ID); err != nil {
		t.Fatalf("submit: %v %+v", err, g1FieldErrors(err))
	}
	g5WantList(t, "the approved GRN", g5Warnings(t, env, grn.ID), warn)
	g3CheckPO(t, "after approval", env, po.ID, g3Want{received: []string{"300", "0"}, pending: []string{"0", "50"}, status: "part_received"})
	g3Already(t, "after approval", env, po.ID, "1000", "0")

	env.Cancel(t, env.Ravi, grn.ID, "Counted twice")
	g3CheckPO(t, "after the cancel", env, po.ID, g3Want{received: []string{"0", "0"}, pending: []string{"200", "50"}, status: "part_received"})
	g3Already(t, "after the cancel", env, po.ID, "1000", "0")
}

// GRN-015 (GRN spec §4; CD-94, CD-213): two non-admin Stores users, Suresh
// and a second Stores user restricted to METRO-SITE, submit drafts of 700
// and 600 kg on line 1 at the same time, on two connections, through the
// real engine and real AUTH. Both are approved, line 1's received_qty is
// their sum, 1,300, and receipt_status matches it. Each GRN then warns
// against the pending the other left (CD-279 (7): its own never counts).
// The nestcheck gate in make test proves no second connection was opened
// inside a transaction.
func TestGRN_ConcurrentReceipts(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1 := po.Lines[0].ID
	gate2 := env.SecondStores(t)
	a := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "700"}}, doctype.StateDraft)
	b := env.GRN(t, gate2, po.ID, []grntest.Line{{POLineID: l1, Received: "600"}}, doctype.StateDraft)

	type submit struct {
		as      auth.Principal
		id      int64
		version int
		res     doc.Result
		err     error
	}
	subs := []*submit{{as: env.Suresh, id: a.ID, version: a.Version}, {as: gate2, id: b.ID, version: b.Version}}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, s := range subs {
		wg.Go(func() {
			<-start
			s.res, s.err = env.Engine.Do(t.Context(), s.as, goodsreceipt.DocType, s.id, doctype.ActionSubmit, doc.ActionInput{Version: s.version})
		})
	}
	close(start)
	wg.Wait()

	var numbers []string
	for _, s := range subs {
		if s.err != nil {
			t.Fatalf("submit GRN %d as %s: %v %+v", s.id, s.as.Name, s.err, g1FieldErrors(s.err))
		}
		got := env.Load(t, goodsreceipt.DocType, s.id)
		if got.State != doctype.StateApproved {
			t.Errorf("GRN %d by %s is %s, want approved", s.id, s.as.Name, got.State)
		}
		numbers = append(numbers, got.Number)
	}
	slices.Sort(numbers)
	if !slices.Equal(numbers, []string{"GR/26-27/00001", "GR/26-27/00002"}) {
		t.Errorf("numbers %v, want GR/26-27/00001 and GR/26-27/00002", numbers)
	}
	po2 := g3ReadPO(t, env, po.ID)
	g3CheckPO(t, "after both submits", env, po.ID, g3Want{received: []string{"1300", "0"}, pending: []string{"0", "50"}, status: g3StatusOf(po2.Lines)})
	if po2.Status != "part_received" {
		t.Errorf("receipt_status = %q, want part_received (line 2 still pending)", po2.Status)
	}

	g5WantList(t, "Suresh's GRN of 700", g5Warnings(t, env, a.ID), g5Over(1, "700 KGS", "600 KGS", "PO/26-27/00001"))
	g5WantList(t, "the second user's GRN of 600", g5Warnings(t, env, b.ID), g5Over(1, "600 KGS", "500 KGS", "PO/26-27/00001"))
}
