package goodsreceipt_test

import (
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// GRN-044 (research Gap 4, S39 Oracle blind receiving; GRN spec §4, §11;
// CD-272): Notes says "All lines received as ordered." only when every PO
// line with pending above 0 has a GRN line whose accepted equals that
// pending, and the GRN has no other line. Drafts built with grntest.GRN at
// each line's pending (what step 1 prefills) get it; clearing line 2,
// changing line 1 to 1,100 or rejecting 20 on line 1 removes it. Pending,
// not ordered, is compared: after 400 kg is approved elsewhere, 800 is "as
// ordered" and 1,200 isn't; once line 2 is fully received it needs no GRN
// line, and a GRN line on it is "another line".
func TestGRN_ReceivedAsOrderedNote(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	notes := func(lines ...grntest.Line) []string {
		t.Helper()
		return g5Notes(t, env, env.GRN(t, env.Suresh, po.ID, lines, doctype.StateDraft).ID)
	}

	g5WantList(t, "1,200 and 50, each line's pending", notes(grntest.Line{POLineID: l1, Received: "1200"}, grntest.Line{POLineID: l2, Received: "50"}), g5NoteAll)
	g5WantList(t, "line 2 cleared", notes(grntest.Line{POLineID: l1, Received: "1200"}))
	g5WantList(t, "line 1 changed to 1,100", notes(grntest.Line{POLineID: l1, Received: "1100"}, grntest.Line{POLineID: l2, Received: "50"}))
	g5WantList(t, "20 rejected on line 1", notes(grntest.Line{POLineID: l1, Received: "1200", Rejected: "20", Reason: "Bent"}, grntest.Line{POLineID: l2, Received: "50"}))

	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "400"}}, doctype.StateApproved)
	g5WantList(t, "800 and 50 after 400 approved", notes(grntest.Line{POLineID: l1, Received: "800"}, grntest.Line{POLineID: l2, Received: "50"}), g5NoteAll)
	g5WantList(t, "1,200 and 50 after 400 approved", notes(grntest.Line{POLineID: l1, Received: "1200"}, grntest.Line{POLineID: l2, Received: "50"}))

	env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "50"}}, doctype.StateApproved)
	g5WantList(t, "800 on line 1, line 2 fully received", notes(grntest.Line{POLineID: l1, Received: "800"}), g5NoteAll)
	g5WantList(t, "800 on line 1 and 5 more on the fully received line 2", notes(grntest.Line{POLineID: l1, Received: "800"}, grntest.Line{POLineID: l2, Received: "5"}))
}

// GRN-048 (research Gap 13, S68, S69: CGST Rule 138(1)(iii), 138(11)-(12);
// GRN spec §7; CD-272): the e-way bill hints are notes, in Notes' order
// after "All lines received as ordered.", and refuse nothing. An
// unregistered vendor (LOCAL, no GSTIN) with accepted_value above
// ₹50,000.00 gets the first: ₹50,000.01 does, exactly ₹50,000.00 doesn't,
// and SHREE (registered) never does. A GRN whose every line's accepted is 0
// with an e-way bill no. gets the second; without the number, or with any
// line accepted, it doesn't. Both GRNs submit and approve.
func TestGRN_EWayHints(t *testing.T) {
	env := grntest.New(t)
	shree := g2RefPO(t, env)
	local := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "LOCAL")}),
		purchasetest.OrderLines(
			map[string]string{"qty": "10", "price": "5000"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "1", "price": "0.01", "hsn_sac": "7217"},
		))
	m1, m2 := local.Lines[0], local.Lines[1]
	create := func(po doc.Doc, header map[string]string, lines ...map[string]string) int64 {
		t.Helper()
		h := g2Header(t, env, po)
		maps.Copy(h, header)
		return g2MustCreate(t, env, env.Suresh, h, lines...).ID
	}

	// The unregistered-vendor hint.
	if got := g2Column(t, env, create(local, nil, g2Line(m1, "1", "", "")), "snap_vendor_gstin"); got != "" {
		t.Fatalf("snap_vendor_gstin %q, want blank (LOCAL is unregistered)", got)
	}
	g5WantList(t, "LOCAL, ₹50,000.00 accepted", g5Notes(t, env, create(local, nil, g2Line(m1, "10", "", ""))))
	g5WantList(t, "LOCAL, ₹50,005.00 accepted", g5Notes(t, env, create(local, nil, g2Line(m1, "10.001", "", ""))), g5NoteUnreg)
	g5WantList(t, "LOCAL, ₹25,005.01 accepted (5 of 10.001 kg rejected)", g5Notes(t, env,
		create(local, nil, g2Line(m1, "10.001", "5", "Bent"), g2Line(m2, "1", "", ""))))
	above := create(local, nil, g2Line(m1, "10", "", ""), g2Line(m2, "1", "", ""))
	if got := g2Column(t, env, above, "accepted_value"); got != "5000001" {
		t.Fatalf("accepted_value %q, want 5000001 paise (₹50,000.01)", got)
	}
	g5WantList(t, "LOCAL, ₹50,000.01 accepted at each line's pending", g5Notes(t, env, above), g5NoteAll, g5NoteUnreg)
	g5WantList(t, "SHREE (registered), ₹62,500.00 accepted", g5Notes(t, env, create(shree, nil, g2Line(shree.Lines[0], "1000", "", ""))))

	// The fully-rejected e-way hint.
	eway := map[string]string{"eway_bill_no": "331000123456"}
	rejected := create(shree, eway, g2Line(shree.Lines[0], "10", "10", "Wet"), g2Line(shree.Lines[1], "5", "5", "Wet"))
	g5WantList(t, "every line fully rejected, e-way bill no. set", g5Notes(t, env, rejected), g5NoteReject)
	g5WantList(t, "every line fully rejected, no e-way bill no.", g5Notes(t, env,
		create(shree, nil, g2Line(shree.Lines[0], "10", "10", "Wet"), g2Line(shree.Lines[1], "5", "5", "Wet"))))
	g5WantList(t, "line 2 accepts 1 kg, e-way bill no. set", g5Notes(t, env,
		create(shree, eway, g2Line(shree.Lines[0], "10", "10", "Wet"), g2Line(shree.Lines[1], "5", "4", "Wet"))))

	// Neither hint refuses anything.
	for _, id := range []int64{above, rejected} {
		if _, err := g2Submit(t, env, env.Suresh, id); err != nil {
			t.Errorf("submit GRN %d with a hint: %v %+v, want it approved", id, err, g1FieldErrors(err))
		} else if got := env.Load(t, goodsreceipt.DocType, id).State; got != doctype.StateApproved {
			t.Errorf("GRN %d with a hint is %s, want approved", id, got)
		}
	}
	g5WantList(t, "the approved fully rejected GRN", g5Notes(t, env, rejected), g5NoteReject)
}
