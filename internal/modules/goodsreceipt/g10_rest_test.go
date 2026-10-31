package goodsreceipt_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G10 texts. g10MsgRest is purchase's comment (PO spec §27.2,
// CD-276; purchase/messages_rest.go), copied exactly; it names no GRN.
// g10MetaRest is the To close row's meta (purchase queues.go, UX §5.2)
// with the business date of the env clock.
const (
	g10MsgRest  = "Vendor says the rest won't come. Close this order if nothing more is due."
	g10MetaRest = "Vendor says the rest won't come · 24-10-2026"
)

// g10Ticked saves a GRN as p on po receiving received of PO line 1 with
// rest_not_coming ticked (as S1's delivery step posts it), and submits it
// unless draft; with no GRN rule submit approves. It returns the stored GRN.
func g10Ticked(t *testing.T, env *grntest.Env, p auth.Principal, po doc.Doc, received string, draft bool) doc.Doc {
	t.Helper()
	h := g2Header(t, env, po)
	h["rest_not_coming"] = "true"
	res := g2MustCreate(t, env, p, h, g2Line(po.Lines[0], received, "", ""))
	if !draft {
		if _, err := g2Submit(t, env, p, res.ID); err != nil {
			t.Fatalf("submit ticked GRN %d as %s: %v %+v", res.ID, p.Name, err, g1FieldErrors(err))
		}
		g5WantState(t, "the ticked GRN", env, res.ID, doctype.StateApproved)
	}
	g := env.Load(t, goodsreceipt.DocType, res.ID)
	if g.Header["rest_not_coming"] != true {
		t.Fatalf("GRN %d rest_not_coming = %v, want true", g.ID, g.Header["rest_not_coming"])
	}
	return g
}

// g10Comments are PO poID's comments as Priya reads them (modkit).
func g10Comments(t *testing.T, env *grntest.Env, poID int64) []string {
	t.Helper()
	cs, err := env.Comments.ListComments(t.Context(), env.Priya, purchase.DocType, poID)
	if err != nil {
		t.Fatalf("ListComments on PO %d as Priya: %v", poID, err)
	}
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, fmt.Sprintf("%q by %d", c.Body, c.CreatedBy))
	}
	return out
}

// g10Comment is one comment as g10Comments renders it.
func g10Comment(by auth.Principal) string { return fmt.Sprintf("%q by %d", g10MsgRest, by.ID) }

// g10Rows are PO poID's purchase_order_rest rows as "grn by user", by
// grn_id. Read only: the rows are written by purchase.SetRestNotComing.
func g10Rows(t *testing.T, env *grntest.Env, poID int64) []string {
	t.Helper()
	out := []string{}
	for _, r := range g2Query(t, env.DB, `SELECT grn_id, created_by FROM purchase_order_rest WHERE po_id = ? ORDER BY grn_id`, poID) {
		out = append(out, r[0]+" by "+r[1])
	}
	return out
}

// g10Row is one row as g10Rows renders it.
func g10Row(grnID int64, by auth.Principal) string { return fmt.Sprintf("%d by %d", grnID, by.ID) }

// g10ToClose is every row of Priya's To close queue as "PO id state meta".
func g10ToClose(t *testing.T, env *grntest.Env) []string {
	t.Helper()
	v, err := env.Env.Service.Queue(t.Context(), env.Priya, purchase.QueueToClose, 1)
	if err != nil {
		t.Fatalf("To close as Priya: %v", err)
	}
	out := []string{}
	for _, r := range v.Rows {
		out = append(out, fmt.Sprintf("%s %d %s %s", r.DocType, r.ID, r.State, strings.Join(r.Meta, " | ")))
	}
	if v.Total != len(out) {
		t.Errorf("To close Total = %d, want %d (one page)", v.Total, len(out))
	}
	return out
}

// g10Listed is PO poID's To close row: the vendor, then the rest meta (no
// GRN named).
func g10Listed(poID int64, vendor string) string {
	return fmt.Sprintf("%s %d %s %s | %s", purchase.DocType, poID, doctype.StateApproved, vendor, g10MetaRest)
}

// g10Want compares two rendered lists.
func g10Want(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got  %q\n want %q", what, got, want)
	}
}

// GRN-047 (research Gap 8, S43: Odoo test_purchase_order.py:877; GRN spec
// §3.1, §5; CD-272 Gap 8, CD-276; plan G10): on the real services. A GRN
// without the tick posts nothing and queues nothing. A ticked draft does
// nothing until approved. Approving Suresh's ticked GRN (600 of line 1)
// posts purchase's comment once by Suresh (the GRN's updated_by), keys the
// row by that GRN, lists the PO in Priya's To close, and leaves the PO
// approved, part_received and at the version an unticked receipt leaves.
// Calling SetRestNotComing again for the same pair adds nothing. A second
// ticked GRN (Suresh's draft submitted by the second Stores user) adds its
// own row and comment by the submitter, and the PO is listed once.
// Cancelling one GRN removes only its own row; the PO leaves To close when
// the last one is cancelled, and the comments stay. The amendment of a
// ticked GRN keeps the tick and puts the PO back through its own row on
// approval. Priya closing the PO takes it out of To close.
func TestGRN_RestNotComing(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	vendor := g2Query(t, env.DB, `SELECT snap_vendor_name FROM purchase_order WHERE id = ?`, po.ID)[0][0]
	if vendor == "" {
		t.Fatalf("PO %d has no snap_vendor_name", po.ID)
	}
	listed := []string{g10Listed(po.ID, vendor)}

	// No tick: no comment, no row, not queued, on approve or cancel.
	other := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	otherBefore := g3ReadPO(t, env, other.ID).Version
	plain := env.GRN(t, env.Suresh, other.ID, g4Line1(other, "40"), doctype.StateApproved)
	// The receipt itself moves the PO's version (applyReceipt); the tick
	// must add nothing to that.
	receiptBump := g3ReadPO(t, env, other.ID).Version - otherBefore
	if plain.Header["rest_not_coming"] == true {
		t.Fatalf("grntest.GRN saved rest_not_coming ticked")
	}
	g10Want(t, "comments after an unticked GRN", g10Comments(t, env, other.ID), []string{})
	g10Want(t, "rest rows after an unticked GRN", g10Rows(t, env, other.ID), []string{})
	g10Want(t, "To close after an unticked GRN", g10ToClose(t, env), []string{})
	env.Cancel(t, env.Ravi, plain.ID, g4CancelReason)
	g10Want(t, "comments after cancelling an unticked GRN", g10Comments(t, env, other.ID), []string{})
	g10Want(t, "To close after cancelling an unticked GRN", g10ToClose(t, env), []string{})

	// A ticked draft does nothing; approval does.
	before := g3ReadPO(t, env, po.ID)
	a := g10Ticked(t, env, env.Suresh, po, "600", true)
	g10Want(t, "comments on a ticked draft", g10Comments(t, env, po.ID), []string{})
	g10Want(t, "To close on a ticked draft", g10ToClose(t, env), []string{})
	if _, err := g2Submit(t, env, env.Suresh, a.ID); err != nil {
		t.Fatalf("submit ticked GRN %d: %v %+v", a.ID, err, g1FieldErrors(err))
	}
	g5WantState(t, "GRN A", env, a.ID, doctype.StateApproved)
	g10Want(t, "comments after approving ticked GRN A", g10Comments(t, env, po.ID), []string{g10Comment(env.Suresh)})
	g10Want(t, "rest rows after approving GRN A", g10Rows(t, env, po.ID), []string{g10Row(a.ID, env.Suresh)})
	g10Want(t, "To close after approving GRN A", g10ToClose(t, env), listed)
	g3CheckPO(t, "after ticked GRN A", env, po.ID, g3Want{received: []string{"600", "0"}, pending: []string{"600", "50"}, status: "part_received"})
	if after := g3ReadPO(t, env, po.ID); after.Version != before.Version+receiptBump {
		t.Errorf("PO version after ticked GRN A = %d, want %d (%d + the receipt's own %d; the tick never touches the PO)",
			after.Version, before.Version+receiptBump, before.Version, receiptBump)
	}

	// Purchase's idempotence covers the same (PO, GRN) pair: a repeated call
	// for GRN A adds nothing, which also shows the hook keyed the row by A.
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return purchase.SetRestNotComing(t.Context(), tx, po.ID, a.ID, env.Suresh.ID, true)
	}); err != nil {
		t.Fatalf("SetRestNotComing again for GRN A: %v", err)
	}
	g10Want(t, "comments after a repeated call for GRN A", g10Comments(t, env, po.ID), []string{g10Comment(env.Suresh)})
	g10Want(t, "rest rows after a repeated call for GRN A", g10Rows(t, env, po.ID), []string{g10Row(a.ID, env.Suresh)})

	// A second ticked GRN, saved by Suresh and submitted by the second
	// Stores user: its own row and comment, by its updated_by (the
	// submitter, not the creator); the PO is listed once.
	gate2 := env.SecondStores(t)
	b := g10Ticked(t, env, env.Suresh, po, "100", true)
	if _, err := g2Submit(t, env, gate2, b.ID); err != nil {
		t.Fatalf("submit ticked GRN %d as %s: %v %+v", b.ID, gate2.Name, err, g1FieldErrors(err))
	}
	g5WantState(t, "GRN B", env, b.ID, doctype.StateApproved)
	if by := g2Column(t, env, b.ID, "updated_by"); by != fmt.Sprint(gate2.ID) {
		t.Fatalf("GRN B updated_by = %s, want the submitter %d", by, gate2.ID)
	}
	g10Want(t, "comments after ticked GRN B", g10Comments(t, env, po.ID), []string{g10Comment(env.Suresh), g10Comment(gate2)})
	g10Want(t, "rest rows after GRN B", g10Rows(t, env, po.ID), []string{g10Row(a.ID, env.Suresh), g10Row(b.ID, gate2)})
	g10Want(t, "To close after GRN B", g10ToClose(t, env), listed)

	// Cancel removes only the cancelled GRN's row; the comments stay.
	env.Cancel(t, env.Ravi, a.ID, g4CancelReason)
	g10Want(t, "rest rows after cancelling GRN A", g10Rows(t, env, po.ID), []string{g10Row(b.ID, gate2)})
	g10Want(t, "To close after cancelling GRN A (B still ticked)", g10ToClose(t, env), listed)
	env.Cancel(t, env.Ravi, b.ID, g4CancelReason)
	g10Want(t, "rest rows after cancelling GRN B", g10Rows(t, env, po.ID), []string{})
	g10Want(t, "To close after cancelling both", g10ToClose(t, env), []string{})
	g10Want(t, "comments after cancelling both", g10Comments(t, env, po.ID), []string{g10Comment(env.Suresh), g10Comment(gate2)})
	g3CheckPO(t, "after both cancels", env, po.ID, g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"})

	// The amendment keeps the tick; its approval lists the PO through its
	// own row.
	am := env.Amend(t, env.Suresh, a.ID)
	if am.Header["rest_not_coming"] != true {
		t.Errorf("amendment rest_not_coming = %v, want true (kept from GRN A)", am.Header["rest_not_coming"])
	}
	g10Want(t, "To close with the amendment in draft", g10ToClose(t, env), []string{})
	g7Approve(t, env, po.ID, am.ID, "")
	g10Want(t, "rest rows after approving the amendment", g10Rows(t, env, po.ID), []string{g10Row(am.ID, env.Suresh)})
	g10Want(t, "To close after approving the amendment", g10ToClose(t, env), listed)
	g10Want(t, "comments after approving the amendment", g10Comments(t, env, po.ID),
		[]string{g10Comment(env.Suresh), g10Comment(gate2), g10Comment(env.Suresh)})
	g3CheckPO(t, "after the amendment", env, po.ID, g3Want{received: []string{"600", "0"}, pending: []string{"600", "50"}, status: "part_received"})

	// Priya closes the listed PO: it leaves To close.
	env.ClosePO(t, env.Priya, po.ID, "Vendor stopped supplying")
	g10Want(t, "To close after Priya closes the PO", g10ToClose(t, env), []string{})
	if st := g3ReadPO(t, env, po.ID).State; st != string(doctype.StateClosed) {
		t.Errorf("PO state after Priya's close = %s, want closed", st)
	}
}
