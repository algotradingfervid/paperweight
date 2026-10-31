package goodsreceipt_test

import (
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN G4: PO interplay, closed and cancelled POs (GRN spec §3, §5; PO
// spec §5, §6.4). Every PO, request and GRN is made and changed through
// Engine.Do with purchasetest's and grntest's builders: no raw writes of PO
// or GRN state (CD-125, research §2 pattern 2). Each refusal is asserted by
// its exact text as DOC renders it (CD-142) and by a snapshot of every PO
// and GRN state, version, quantity and row count (g3Snapshot) taken before
// it.

// DOC's texts (internal/kernel/doc/messages.go), as checkSource and
// checkBlockers render them for a purchase.Order source: the source's
// display (its number, or Draft {id}) and the lower-cased label.
const (
	g4MsgClosed      = "%s is closed. Remove the line or pick another purchase order."     // DOC-017
	g4MsgCancelled   = "%s is cancelled. Remove the line or pick another purchase order."  // DOC-038
	g4MsgNotApproved = "%s isn't approved yet. Pick an approved purchase order."           // msgSourceNotApproved
	g4MsgPending     = "%s is waiting for approval. Pick an approved purchase order."      // msgSourcePending
	g4MsgBlockedOne  = "Can't cancel %s because %s (%s) is linked to it. Cancel %s first." // DOC-052
	g4MsgLineMissing = "Line %d: Purchase Order line not found. Pick one from the list."   // msgSourceLineNotFound, CD-314
	g4MissingLineID  = "999999"                                                            // no purchase_order_line has this id
	g4CloseReason    = "Vendor short-closed the order"                                     // ≥ 3 characters
	g4CancelReason   = "Goods found bad after acceptance"                                  // ≥ 3 characters
	g4POCancelReason = "Order raised by mistake"                                           // ≥ 3 characters
	g4FirstGRN       = "GR/26-27/00001"                                                    // 14 characters
	g4FirstAmend     = "GR/26-27/00001-1"                                                  // amendment of the first GRN
	g4FirstPO        = "PO/26-27/00001"                                                    // the reference PO
)

// g4POHeader is the header error DOC's source-state check gives on
// purchase_order_id (the header LinkSpec is checked before the line link,
// and save, create and submit stop at the first source error).
func g4POHeader(format, display string) doctype.FieldError {
	return doctype.FieldError{Field: "purchase_order_id", Message: fmt.Sprintf(format, display)}
}

// g4WantUnchanged asserts nothing was written since before (g3Snapshot).
func g4WantUnchanged(t *testing.T, what string, env *grntest.Env, before string) {
	t.Helper()
	if after := g3Snapshot(t, env); after != before {
		t.Errorf("%s wrote:\nbefore %s\nafter  %s", what, before, after)
	}
}

// g4WantInvalid asserts err is DOC's KindInvalid (422) with exactly want,
// and that nothing was written since before.
func g4WantInvalid(t *testing.T, what string, env *grntest.Env, before string, err error, want ...doctype.FieldError) {
	t.Helper()
	g2WantInvalid(t, what, err, want...)
	var de *doc.Error
	if errors.As(err, &de) && de.Kind != doc.KindInvalid {
		t.Errorf("%s: kind %s, want %s", what, de.Kind, doc.KindInvalid)
	}
	g4WantUnchanged(t, what, env, before)
}

// g4WantBlocked asserts err is DOC's KindBlocked refusal (409) with text msg.
func g4WantBlocked(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var de *doc.Error
	if err == nil || !errors.As(err, &de) {
		t.Errorf("%s: err = %v (%T), want DOC's 409 %q", what, err, err, msg)
		return
	}
	if de.Kind != doc.KindBlocked || de.Status() != 409 || de.Message != msg {
		t.Errorf("%s: %s %d %q, want %s 409 %q", what, de.Kind, de.Status(), de.Message, doc.KindBlocked, msg)
	}
}

// g4CancelPO runs Ravi's (Purchase Manager) cancel of PO id at its stored
// version and returns the engine's error.
func g4CancelPO(t *testing.T, env *grntest.Env, id int64) error {
	t.Helper()
	_, err := env.Engine.Do(t.Context(), env.Ravi, purchase.DocType, id, doctype.ActionCancel,
		doc.ActionInput{Version: env.Load(t, purchase.DocType, id).Version, Reason: g4POCancelReason})
	return err
}

// g4Line1 is a one-line GRN on the PO's line 1 receiving received kg.
func g4Line1(po doc.Doc, received string) []grntest.Line {
	return []grntest.Line{{POLineID: po.Lines[0].ID, Received: received}}
}

// g4PendingRows are the pending report rows (P9, all pages) Priya reads for
// PO poID.
func g4PendingRows(t *testing.T, env *grntest.Env, poID int64) []purchase.PendingRow {
	t.Helper()
	v, err := env.Env.Service.PendingReport(t.Context(), env.Priya, purchase.PendingFilter{}, 0)
	if err != nil {
		t.Fatalf("pending report as Priya: %v", err)
	}
	var out []purchase.PendingRow
	for _, r := range v.Rows {
		if r.POID == poID {
			out = append(out, r)
		}
	}
	return out
}

// g4Row is a request row's stored ordered_qty (quantity units) and its
// request's order_status.
type g4Row struct {
	ordered int64
	status  string
}

// g4ReadRow reads request row id with a plain SELECT.
func g4ReadRow(t *testing.T, env *grntest.Env, id int64) g4Row {
	t.Helper()
	r := g2Query(t, env.DB, `SELECT l.ordered_qty, h.order_status FROM purchase_request_line l
		JOIN purchase_request h ON h.id = l.parent_id WHERE l.id = ?`, id)
	if len(r) != 1 {
		t.Fatalf("request row %d not found", id)
	}
	return g4Row{ordered: g2Int(t, r[0][0]), status: r[0][1]}
}

// g4WantRow asserts request row id reads ordered (a quantity literal) and
// status.
func g4WantRow(t *testing.T, what string, env *grntest.Env, id int64, ordered, status string) {
	t.Helper()
	if got := g4ReadRow(t, env, id); got.ordered != int64(g3Q(ordered)) || got.status != status {
		t.Errorf("%s: request row ordered_qty %d, order_status %q; want %d (%s) %q", what, got.ordered, got.status, int64(g3Q(ordered)), ordered, status)
	}
}

// GRN-019 (GRN spec §5, §16; D-DOC-09): on a part-received PO that Priya
// closed, Ravi's cancel of the GRN succeeds; the PO stays closed and its
// received_qty and receipt_status follow the cancel (do and undo, CD-272).
func TestGRN_CancelClosedPO(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateApproved)
	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	g3CheckPO(t, "after the close", env, po.ID, g3Want{
		received: []string{"400", "0"}, pending: []string{"800", "50"}, status: "part_received", state: doctype.StateClosed,
	})

	if got := env.Cancel(t, env.Ravi, grn.ID, g4CancelReason); got.State != doctype.StateCancelled {
		t.Errorf("GRN state after Ravi's cancel = %s, want cancelled", got.State)
	}
	g3CheckPO(t, "after the cancel", env, po.ID, g3Want{
		received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received", state: doctype.StateClosed,
	})
}

// GRN-020 (GRN spec §3, §16; DOC-017): a closed PO refuses a new GRN on
// create, and the submit of a draft made before the close, with DOC-017's
// text; nothing is written.
func TestGRN_ClosedPORefused(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	draft := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateDraft)
	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	want := g4POHeader(g4MsgClosed, g4FirstPO)

	before := g3Snapshot(t, env)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""))
	g4WantInvalid(t, "create on the closed PO", env, before, err, want)

	_, err = g2Submit(t, env, env.Suresh, draft.ID)
	g4WantInvalid(t, "submit of a draft on the closed PO", env, before, err, want)
}

// GRN-021 (GRN spec §3, §16; DOC-052): Ravi's cancel of the reference PO is
// refused while GR/26-27/00001 is approved, naming it, and the PO stays
// approved; after the GRN is cancelled, the PO's cancel succeeds.
func TestGRN_BlocksPOCancel(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateApproved)
	if grn.Number != g4FirstGRN {
		t.Fatalf("GRN number %q, want %s", grn.Number, g4FirstGRN)
	}

	before := g3Snapshot(t, env)
	g4WantBlocked(t, "PO cancel with an approved GRN", g4CancelPO(t, env, po.ID),
		fmt.Sprintf(g4MsgBlockedOne, g4FirstPO, g4FirstGRN, "approved", g4FirstGRN))
	g4WantUnchanged(t, "the refused PO cancel", env, before)
	if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateApproved {
		t.Errorf("PO state after the refused cancel = %s, want approved", got.State)
	}

	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)
	if err := g4CancelPO(t, env, po.ID); err != nil {
		t.Fatalf("PO cancel after the GRN's cancel: %v", err)
	}
	if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateCancelled {
		t.Errorf("PO state = %s, want cancelled", got.State)
	}
}

// PO-025 (PO spec §5, §18; DOC-052; MOD-GRN exit): the PO's cancel is
// refused while a GRN against it is live, with DOC-052's text naming it:
// an approved GRN by its number, and a GRN waiting for approval (a GRN
// approval rule from ₹0 for the Administrator added here through APR) as
// Draft {id}. The PO stays
// approved and nothing is written. A closed GRN can't be reached: GRN's
// CloseRight is none (CD-279 (2)).
func TestPO_CancelBlockedByGRN(t *testing.T) {
	t.Run("approved GRN", func(t *testing.T) {
		env := grntest.New(t)
		po := g2RefPO(t, env)
		grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateApproved)
		before := g3Snapshot(t, env)
		g4WantBlocked(t, "PO cancel", g4CancelPO(t, env, po.ID),
			fmt.Sprintf(g4MsgBlockedOne, g4FirstPO, grn.Number, "approved", grn.Number))
		g4WantUnchanged(t, "the refused PO cancel", env, before)
		if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateApproved {
			t.Errorf("PO state = %s, want approved", got.State)
		}
	})

	t.Run("GRN waiting for approval", func(t *testing.T) {
		env := grntest.New(t)
		po := g2RefPO(t, env)
		env.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
		grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StatePending)
		display := "Draft " + strconv.FormatInt(grn.ID, 10)
		before := g3Snapshot(t, env)
		g4WantBlocked(t, "PO cancel", g4CancelPO(t, env, po.ID),
			fmt.Sprintf(g4MsgBlockedOne, g4FirstPO, display, "waiting for approval", display))
		g4WantUnchanged(t, "the refused PO cancel", env, before)
		if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateApproved {
			t.Errorf("PO state = %s, want approved", got.State)
		}
	})
}

// PO-033 (PO spec §6.4, §18; DOC-017; D-DOC-09; MOD-GRN exit): a
// part-received PO is in P9's pending report until Priya closes it; then
// its rows are gone (a closed PO's pending reads 0), a new GRN is refused
// with DOC-017's text and nothing is written, and the GRN against it can
// still be cancelled, the PO staying closed.
func TestPO_CloseStopsReceipts(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateApproved)
	if rows := g4PendingRows(t, env, po.ID); len(rows) != 2 {
		t.Fatalf("pending report before the close: %d rows of the PO, want 2 (800 kg and 50 kg pending)", len(rows))
	}

	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	if rows := g4PendingRows(t, env, po.ID); len(rows) != 0 {
		t.Errorf("pending report after the close: %d rows of the closed PO %+v, want none", len(rows), rows)
	}

	before := g3Snapshot(t, env)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""))
	g4WantInvalid(t, "a new GRN on the closed PO", env, before, err, g4POHeader(g4MsgClosed, g4FirstPO))

	if got := env.Cancel(t, env.Ravi, grn.ID, g4CancelReason); got.State != doctype.StateCancelled {
		t.Errorf("GRN state after the cancel = %s, want cancelled", got.State)
	}
	g3CheckPO(t, "after the GRN's cancel", env, po.ID, g3Want{
		received: []string{"0", "0"}, pending: []string{"1200", "50"}, status: "not_received", state: doctype.StateClosed,
	})
	if rows := g4PendingRows(t, env, po.ID); len(rows) != 0 {
		t.Errorf("pending report after the GRN's cancel: %d rows of the closed PO, want none", len(rows))
	}
}

// GRN-037 (research S33, ERPNext test_purchase_order.py:87
// test_make_purchase_receipt; GRN spec §3, §16; CD-142): a draft PO and a
// PO waiting for approval (the Purchase Manager rule from ₹50,000 added
// here) are each refused on step 1's path (DraftFromPO, then the engine's
// create) and on a direct engine create, with DOC's source-state text for
// the unnumbered PO, and nothing is written.
func TestGRN_UnapprovedPORefused(t *testing.T) {
	env := grntest.New(t)
	env.AddRule(t, purchase.DocType, 50_000_00, "Purchase Manager")
	lines := purchasetest.OrderLines(
		map[string]string{"qty": "1200", "price": "62.5"},
		map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
	)
	draft := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtDraft(), lines)
	pending := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtPending(), lines)

	for _, c := range []struct {
		name   string
		po     doc.Doc
		format string
	}{
		{"draft PO", draft, g4MsgNotApproved},
		{"pending PO", pending, g4MsgPending},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := g4POHeader(c.format, "Draft "+strconv.FormatInt(c.po.ID, 10))
			before := g3Snapshot(t, env)

			values, poLines, err := env.Service.DraftFromPO(t.Context(), env.Suresh, c.po.ID)
			if err != nil {
				t.Fatalf("DraftFromPO: %v, want the values (it checks no state)", err)
			}
			_, err = g2Create(t, env, env.Suresh, values, poLines...)
			g4WantInvalid(t, "step 1 create", env, before, err, want)

			_, err = g2Create(t, env, env.Suresh, g2Header(t, env, c.po), g2Line(c.po.Lines[0], "100", "", ""))
			g4WantInvalid(t, "engine create", env, before, err, want)
		})
	}
}

// GRN-039 (research S58, Odoo purchase_order_process.py:105; DOC-017,
// DOC-038): a draft GRN whose PO is then closed, or cancelled (a draft
// doesn't block the PO's cancel), is refused on its next save (a changed
// quantity) and on submit; nothing is written.
func TestGRN_DraftAfterPOClosed(t *testing.T) {
	for _, c := range []struct {
		name   string
		end    func(t *testing.T, env *grntest.Env, po doc.Doc)
		format string
	}{
		{"PO closed", func(t *testing.T, env *grntest.Env, po doc.Doc) {
			env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
		}, g4MsgClosed},
		{"PO cancelled", func(t *testing.T, env *grntest.Env, po doc.Doc) {
			if err := g4CancelPO(t, env, po.ID); err != nil {
				t.Fatalf("Ravi cancels the PO with only a draft GRN on it: %v, want cancelled (a draft doesn't block)", err)
			}
		}, g4MsgCancelled},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := grntest.New(t)
			po := g2RefPO(t, env)
			d := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "400"), doctype.StateDraft)
			c.end(t, env, po)
			want := g4POHeader(c.format, g4FirstPO)

			before := g3Snapshot(t, env)
			_, err := g2Save(t, env, env.Suresh, d.ID, g2Header(t, env, po),
				g2StoredLines(t, env, d.ID, map[int]map[string]string{1: {"received_qty": "390"}}))
			g4WantInvalid(t, "save of the draft", env, before, err, want)

			_, err = g2Submit(t, env, env.Suresh, d.ID)
			g4WantInvalid(t, "submit of the draft", env, before, err, want)
		})
	}
}

// GRN-040 (research S59; PO spec §20.2 lineOrdered on a closed PO, §20.3;
// CD-276 (1)): PR-A's row of 40 Bag is pulled into a PO; a GRN accepting 25
// is approved and the PO closed, so the row reads 25 ordered. Ravi's cancel
// of the GRN drops it to 0 and not_ordered through P6's
// RefreshReceiptStatus, the PO staying closed; the replacement amendment
// accepting 25 brings it back (do and undo).
func TestGRN_CancelClosedPOUpdatesRequest(t *testing.T) {
	env := grntest.New(t)
	cement, bag := g1MasterID(t, env.DB, "item", "CEMENT-OPC"), g1MasterID(t, env.DB, "uom", "BAG")
	req := purchasetest.NewRequest(t, env.Env, purchasetest.RequestLines(map[string]string{"item_id": cement, "uom_id": bag, "qty": "40"}))
	row := req.Lines[0].ID
	po := purchasetest.NewOrder(t, env.Env, purchasetest.OrderLines(map[string]string{
		"item_id": cement, "uom_id": bag, "qty": "40", "price": "1500", "hsn_sac": "2523",
		"request_line_id": strconv.FormatInt(row, 10),
	}))
	g4WantRow(t, "PO approved", env, row, "40", "ordered")

	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "25"), doctype.StateApproved)
	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	g4WantRow(t, "GRN of 25 approved, PO closed", env, row, "25", "part_ordered")

	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)
	g4WantRow(t, "GRN cancelled", env, row, "0", "not_ordered")
	if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateClosed {
		t.Errorf("PO state after the GRN's cancel = %s, want closed", got.State)
	}

	am := env.Amend(t, env.Suresh, grn.ID)
	g2WantApproved(t, "submit of the amendment", env, am.ID)
	g4WantRow(t, "amendment accepting 25 approved", env, row, "25", "part_ordered")
	if got := env.Load(t, purchase.DocType, po.ID); got.State != doctype.StateClosed {
		t.Errorf("PO state after the amendment = %s, want closed", got.State)
	}
}

// GRN-041 (research S60, Gap 2, ERPNext test_purchase_order.py:1327; GRN
// spec §5; CD-272; CD-279 (10) G0c): a GRN accepting 600 kg on the
// reference PO, then the PO closed by Priya; Ravi cancels the GRN, Suresh
// amends it to 580 and submits: approved as GR/26-27/00001-1, line 1's
// received_qty 580, the PO still closed. A fresh GRN on the closed PO is
// refused with DOC-017's text and nothing is written. (The cancel page's
// warning text is S3's page test.)
func TestGRN_AmendOnClosedPO(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, g4Line1(po, "600"), doctype.StateApproved)
	env.ClosePO(t, env.Priya, po.ID, g4CloseReason)
	env.Cancel(t, env.Ravi, grn.ID, g4CancelReason)

	am := env.Amend(t, env.Suresh, grn.ID)
	if _, err := g2Save(t, env, env.Suresh, am.ID, g2Header(t, env, po),
		g2StoredLines(t, env, am.ID, map[int]map[string]string{1: {"received_qty": "580"}})); err != nil {
		t.Fatalf("Suresh saves the amendment at 580: %v %+v, want saved (G0c keeps the closed PO link)", err, g1FieldErrors(err))
	}
	res, err := g2Submit(t, env, env.Suresh, am.ID)
	if err != nil {
		t.Fatalf("submit of the amendment: %v %+v, want approved", err, g1FieldErrors(err))
	}
	if res.State != doctype.StateApproved || res.Number != g4FirstAmend {
		t.Errorf("amendment %s %q, want approved %s", res.State, res.Number, g4FirstAmend)
	}
	g3CheckPO(t, "after the amendment", env, po.ID, g3Want{
		received: []string{"580", "0"}, pending: []string{"620", "50"}, status: "part_received", state: doctype.StateClosed,
	})

	before := g3Snapshot(t, env)
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "20", "", ""))
	g4WantInvalid(t, "a fresh GRN on the closed PO", env, before, err, g4POHeader(g4MsgClosed, g4FirstPO))
}

// CD-314 (head request, M0-42): a posted po_line_id that names no PO line
// is refused before any write with checkSource's own text on that line, 422
// (KindInvalid): on create, on a draft save and on a step save (Only
// "lines"). No foreign key error surfaces and nothing is written.
func TestGRN_MissingPOLineRefused(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	missing := map[string]string{"po_line_id": g4MissingLineID, "received_qty": "5"}

	before := g3Snapshot(t, env)
	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), missing)
	g4WantInvalid(t, "create", env, before, err,
		doctype.FieldError{Field: "po_line_id", Line: 1, Message: fmt.Sprintf(g4MsgLineMissing, 1)})

	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "400", "", ""))
	want := doctype.FieldError{Field: "po_line_id", Line: 2, Message: fmt.Sprintf(g4MsgLineMissing, 2)}
	before = g3Snapshot(t, env)
	_, err = g2Save(t, env, env.Suresh, d.ID, g2Header(t, env, po), append(g2StoredLines(t, env, d.ID, nil), missing))
	g4WantInvalid(t, "draft save", env, before, err, want)

	_, err = g2Save(t, env, env.Suresh, d.ID, g2Header(t, env, po), append(g2StoredLines(t, env, d.ID, nil), missing), "lines")
	g4WantInvalid(t, "step save", env, before, err, want)
}
