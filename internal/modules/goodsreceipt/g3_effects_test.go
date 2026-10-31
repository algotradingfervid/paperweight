package goodsreceipt_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN G3 (GRN spec §5; PO spec §3.2, §6.3; CD-276 frozen
// RefreshReceiptStatus): approving a GRN applies its effects and cancelling
// it reverses them. The kernel roll-up writes the PO's received_qty; GRN's
// ApplyEffects and ReverseEffects refresh receipt_status from the accepted
// totals on approved and closed GRNs. Every effect is asserted after the
// action and after its reversal (research §2 pattern 4, CD-272), through
// the real engine (CD-125).

// g3Row is one walk-table row: an action and what the PO shows after it.
type g3Row struct {
	what string
	do   func(t *testing.T)
	want g3Want
}

// g3Walk runs rows in order on PO poID, checking the PO after each.
func g3Walk(t *testing.T, env *grntest.Env, poID int64, rows []g3Row) {
	t.Helper()
	for _, r := range rows {
		if r.do != nil {
			r.do(t)
		}
		g3CheckPO(t, r.what, env, poID, r.want)
	}
}

// GRN-010 (GRN spec §3, §5, §6): with no approval rule, Suresh's submit
// approves the GRN at once and numbers it GR/26-27/00001; submit's
// ApplyEffects (spec §5 effects table) has refreshed the PO's receipt_status
// by the time it returns. The approval and number are the kernel's (G1
// wiring); the receipt_status row is G3's.
func TestGRN_SubmitApproves(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	draft := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "400"}}, doctype.StateDraft)

	res, err := g2Submit(t, env, env.Suresh, draft.ID)
	if err != nil {
		t.Fatalf("submit as Suresh: %v %+v", err, g1FieldErrors(err))
	}
	if res.State != doctype.StateApproved || res.Number != "GR/26-27/00001" {
		t.Errorf("submit result: state %s number %q, want approved GR/26-27/00001", res.State, res.Number)
	}
	got := env.Load(t, goodsreceipt.DocType, draft.ID)
	if got.State != doctype.StateApproved || got.Number != "GR/26-27/00001" {
		t.Errorf("stored GRN: state %s number %q, want approved GR/26-27/00001", got.State, got.Number)
	}
	g3CheckPO(t, "after submit", env, po.ID, g3Want{
		received: []string{"400", "0"}, pending: []string{"800", "50"}, status: "part_received",
	})
}

// GRN-011 (GRN spec §5; PO spec §3.2, §6.3), a walk table: two partial
// receipts take the reference PO from not_received through part_received to
// fully_received, and it stays approved; cancelling each in turn walks it
// back (do and undo, CD-272 pattern 4).
func TestGRN_PartialReceipts(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	var first, second doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "reference PO", want: g3Want{
			received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
		{what: "first GRN accepts 400 kg of line 1", do: func(t *testing.T) {
			first = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "400"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"400", "0"}, pending: []string{"800", "50"}, status: "part_received"}},
		{what: "second GRN accepts 800 kg of line 1 and 50 kg of line 2", do: func(t *testing.T) {
			second = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "800"}, {POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1200", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "second GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, second.ID, "Counted twice")
		}, want: g3Want{received: []string{"400", "0"}, pending: []string{"800", "50"}, status: "part_received"}},
		{what: "first GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, first.ID, "Counted twice")
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
	})
}

// GRN-012 (GRN spec §3.2, §4; CD-234): accepted, not received, drives the
// roll-up, so 200 kg rejected of 1,200 received stays pending; the cancel
// gives it all back.
func TestGRN_RejectedStaysPending(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	var grn doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "GRN receives 1,200 kg of line 1 with 200 rejected", do: func(t *testing.T) {
			grn = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "1200", Rejected: "200", Reason: "Bent angles"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1000", "0"}, pending: []string{"200", "50"}, status: "part_received"}},
		{what: "that GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, grn.ID, "Wrong entry")
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
	})
	if got := g2LineColumn(t, env, grn.ID, "accepted_qty"); !slices.Equal(got, []string{"1000000"}) {
		t.Errorf("GRN line accepted_qty = %v, want [1000000] (1,000.000 kg)", got)
	}
}

// GRN-014 (GRN spec §4; PO spec §3.2; CD-238), a walk table of the
// receipt_status, pending and received rows only (the PO page's "Over by"
// note is P15b's, CD-277 (2); the warning row is G5's): 1,250 kg on line 1
// puts received above qty with pending 0, never negative; once line 2 is in
// the PO is fully_received; a further GRN on the fully received PO is
// approved and the status stays; each cancel walks it back.
func TestGRN_OverDeliveryPO(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	var over, rest, extra doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "GRN accepts 1,250 kg of line 1", do: func(t *testing.T) {
			over = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1250"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1250", "0"}, pending: []string{"0", "50"}, status: "part_received"}},
		{what: "GRN accepts 50 kg of line 2", do: func(t *testing.T) {
			rest = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1250", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "another GRN on the fully received PO", do: func(t *testing.T) {
			extra = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "10"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1260", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "that GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, extra.ID, "Not ours")
		}, want: g3Want{received: []string{"1250", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "line 2's GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, rest.ID, "Not ours")
		}, want: g3Want{received: []string{"1250", "0"}, pending: []string{"0", "50"}, status: "part_received"}},
		{what: "the over-delivery cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, over.ID, "Not ours")
		}, want: g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}},
	})
	if extra.State != doctype.StateApproved || extra.Number != "GR/26-27/00003" {
		t.Errorf("GRN on the fully received PO: state %s number %q, want approved GR/26-27/00003", extra.State, extra.Number)
	}
}

// GRN-018 (GRN spec §5, §10): on the PO of GRN-011 after its first GRN,
// Ravi (Purchase Manager) can't cancel without a reason (the kernel's
// text), Suresh (Stores) can't cancel at all (AUTH's 403), and neither
// refusal writes anything; Ravi's cancel with a reason gives line 1's 400 kg
// back and returns the status to not_received. Each user holds only his
// fixture roles.
func TestGRN_CancelRestores(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "400"}}, doctype.StateApproved)
	partWant := g3Want{received: []string{"400", "0"}, pending: []string{"800", "50"}, status: "part_received"}
	g3CheckPO(t, "after the first GRN", env, po.ID, partWant)
	before := g3Snapshot(t, env)

	cancel := func(p auth.Principal, reason string) error {
		_, err := env.Engine.Do(t.Context(), p, goodsreceipt.DocType, grn.ID, doctype.ActionCancel,
			doc.ActionInput{Version: env.Load(t, goodsreceipt.DocType, grn.ID).Version, Reason: reason})
		return err
	}

	const reasonText = "Enter a reason for cancelling (at least 3 characters)."
	err := cancel(env.Ravi, "")
	var he doctype.HTTPError
	switch {
	case err == nil || !errors.As(err, &he):
		t.Errorf("Ravi's cancel without a reason: err = %v, want the kernel's 422", err)
	default:
		if he.Status() != 422 || err.Error() != reasonText ||
			!slices.Equal(he.FieldErrors(), []doctype.FieldError{{Field: "reason", Message: reasonText}}) {
			t.Errorf("Ravi's cancel without a reason: %d %q %+v, want 422 %q on reason", he.Status(), err.Error(), he.FieldErrors(), reasonText)
		}
	}
	if after := g3Snapshot(t, env); after != before {
		t.Errorf("the refused cancel without a reason wrote:\nbefore %s\nafter  %s", before, after)
	}

	err = cancel(env.Suresh, "Wrong quantity")
	var de *auth.DeniedError
	if !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Suresh's cancel: err = %v (%T), want AUTH's 403", err, err)
	} else if want := (&auth.DeniedError{Right: auth.RightCancel, Label: "Goods Receipt", Plural: "Goods Receipts"}).Error(); err.Error() != want {
		t.Errorf("Suresh's cancel: %q, want %q", err.Error(), want)
	}
	if after := g3Snapshot(t, env); after != before {
		t.Errorf("Suresh's refused cancel wrote:\nbefore %s\nafter  %s", before, after)
	}
	g3CheckPO(t, "after both refusals", env, po.ID, partWant)

	if err := cancel(env.Ravi, "Wrong quantity"); err != nil {
		t.Fatalf("Ravi's cancel with a reason: %v", err)
	}
	if got := env.Load(t, goodsreceipt.DocType, grn.ID); got.State != doctype.StateCancelled {
		t.Errorf("GRN state = %s, want cancelled", got.State)
	}
	g3CheckPO(t, "after Ravi's cancel", env, po.ID, g3Want{
		received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received",
	})
}

// GRN-038 (research S35, Gap 10, ERPNext test_purchase_receipt.py:77 and
// :3276 test_return_of_a_fully_rejected_receipt; GRN spec §4; CD-234): a
// GRN receiving 10 kg with all 10 rejected is approved with accepted 0 and
// accepted value ₹0.00, and neither it nor its cancel moves the PO. The last
// rows repeat the do and undo with the rest of the PO received in between,
// so the cancel of the fully rejected GRN must leave fully_received as it is.
func TestGRN_FullyRejected(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	nothing := g3Want{received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received"}
	rejectAll := []grntest.Line{{POLineID: l1, Received: "10", Rejected: "10", Reason: "Rusted through"}}
	var rejected, again doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "fully rejected GRN approved", do: func(t *testing.T) {
			rejected = env.GRN(t, env.Suresh, po.ID, rejectAll, doctype.StateApproved)
		}, want: nothing},
		{what: "fully rejected GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, rejected.ID, "Entered by mistake")
		}, want: nothing},
		{what: "a second fully rejected GRN approved", do: func(t *testing.T) {
			again = env.GRN(t, env.Suresh, po.ID, rejectAll, doctype.StateApproved)
		}, want: nothing},
		{what: "the whole PO received on another GRN", do: func(t *testing.T) {
			env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "1200"}, {POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1200", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "the second fully rejected GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, again.ID, "Entered by mistake")
		}, want: g3Want{received: []string{"1200", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
	})
	if rejected.State != doctype.StateApproved || rejected.Number != "GR/26-27/00001" {
		t.Errorf("fully rejected GRN on submit: state %s number %q, want approved GR/26-27/00001", rejected.State, rejected.Number)
	}
	if got := g2LineColumn(t, env, rejected.ID, "accepted_qty"); !slices.Equal(got, []string{"0"}) {
		t.Errorf("fully rejected GRN accepted_qty = %v, want [0]", got)
	}
	if got := g2Column(t, env, rejected.ID, "accepted_value"); got != "0" {
		t.Errorf("fully rejected GRN accepted_value = %q, want 0 (₹0.00)", got)
	}
}

// PO-031 (PO spec §3.2, §12, §18; CD-276): a PO line with 200 kg already
// received before the system keeps it while real GRNs roll received_qty up,
// and pending and receipt_status count both: 1,200 − 200 − 500 leaves 500
// pending; 1,000 accepted in all fills the line; the cancel walks it back,
// and already_received never moves.
func TestPO_OpeningSurvivesRollUp(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-77"}),
		purchasetest.OrderLines(
			map[string]string{"qty": "1200", "price": "62.5", "already_received": "200"},
			map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
		))
	l1, l2 := po.Lines[0].ID, po.Lines[1].ID
	var second doc.Doc
	g3Walk(t, env, po.ID, []g3Row{
		{what: "approved with 200 kg already received", want: g3Want{
			received: []string{"0", "0"}, pending: []string{"1000", "50"}, status: "part_received"}},
		{what: "GRN accepts 500 kg of line 1", do: func(t *testing.T) {
			env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "500"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"500", "0"}, pending: []string{"500", "50"}, status: "part_received"}},
		{what: "GRN accepts the last 500 kg of line 1 and line 2", do: func(t *testing.T) {
			second = env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: l1, Received: "500"}, {POLineID: l2, Received: "50"}}, doctype.StateApproved)
		}, want: g3Want{received: []string{"1000", "50"}, pending: []string{"0", "0"}, status: "fully_received"}},
		{what: "that GRN cancelled", do: func(t *testing.T) {
			env.Cancel(t, env.Ravi, second.ID, "Wrong entry")
		}, want: g3Want{received: []string{"500", "0"}, pending: []string{"500", "50"}, status: "part_received"}},
	})
	g3Already(t, "after the GRNs and the cancel", env, po.ID, "200", "0")
}
