package goodsreceipt_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G2 review rows (AD-12 fix loop). Each row names the mutant of the
// G2 code it kills; they extend the GRN cases named in their comments.

// GRN-009 (CD-308, CD-311, CD-302): one line whose own amount is above the
// money maximum, while its accepted quantity is within twice the ordered
// (1,800 of 1,000 at ₹60,00,000.00 = ₹10,80,00,00,000.00), is refused on
// "lines" with the total text, and a step save that keeps it leaves
// accepted_value cleared. Kills: ignoring LineAmount's error when summing.
func TestGRN_Review_LineAmountOverMax(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "1000", "price": "6000000"}))
	totalErr := doctype.FieldError{Field: "lines", Message: g2MsgTotal}

	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "1800", "", ""))
	g2WantInvalid(t, "one line over the maximum", err, totalErr)

	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "1000", "", ""))
	if got := g2Column(t, env, d.ID, "accepted_value"); got != "600000000000" {
		t.Errorf("accepted_value of 1,000 at ₹60,00,000.00 = %q, want 600000000000", got)
	}
	res, err := g2Save(t, env, env.Suresh, d.ID, map[string]string{"remarks": "Recounted"},
		g2StoredLines(t, env, d.ID, map[int]map[string]string{1: {"received_qty": "1800"}}), "remarks")
	g2WantProblems(t, "step save of one line over the maximum", res, err, totalErr)
	if got := g2Column(t, env, d.ID, "accepted_value"); !g2Same(got, "") {
		t.Errorf("accepted_value with a line over the maximum = %q, want cleared", got)
	}
	g2CheckDerived(t, "line over the maximum", env, d.ID)
}

// GRN-009 (GRN spec §3.2: value at the PO's net rate): a PO line at ₹100.00
// with 10% discount has net rate ₹90.0000; the GRN line's snap_rate is the
// net rate and accepted_value is 10 × ₹90.00. Kills: snap_rate copied from
// the PO line's price.
func TestGRN_Review_NetRateNotPrice(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "100", "price": "100", "discount_pct": "10"}))
	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "10", "", ""))
	if got := g2LineColumn(t, env, d.ID, "snap_rate"); len(got) != 1 || got[0] != "900000" {
		t.Errorf("snap_rate %v, want [900000] (₹90.0000, the net rate, not the price)", got)
	}
	if got := g2Column(t, env, d.ID, "accepted_value"); got != "90000" {
		t.Errorf("accepted_value = %q, want 90000 (10 × ₹90.0000)", got)
	}
	g2CheckDerived(t, "discounted line", env, d.ID)
}

// GRN-008 (GRN spec §3.2: rejected not above received): rejecting all that
// was received saves, with accepted 0 and no error. Kills: `>` → `>=` in
// the rejected check.
func TestGRN_Review_RejectAll(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	res, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "100", "All bent"))
	if err != nil {
		t.Fatalf("rejected 100 of 100: %v %+v, want it saved", err, g1FieldErrors(err))
	}
	if len(res.Problems) != 0 {
		t.Errorf("rejected 100 of 100: problems %+v, want none", res.Problems)
	}
	if got := g2LineColumn(t, env, res.ID, "accepted_qty"); len(got) != 1 || got[0] != "0" {
		t.Errorf("accepted_qty %v, want [0]", got)
	}
	if got := g2Column(t, env, res.ID, "accepted_value"); got != "0" {
		t.Errorf("accepted_value = %q, want 0", got)
	}
}

// GRN-017 (uom.CheckLineQty, MST-087): a line in KGS, a fractional unit that
// converts at 50 to the item's whole-number stock unit NOS, refuses 0.01
// KGS (0.5 NOS) with the stock-unit text on received_qty, and the same on
// rejected_qty; 0.02 KGS (1 NOS) saves. The item SPRING-50 is made through
// the item master service (no seeded item converts a fractional unit to a
// whole one). Kills: line and stock units swapped, and factor 0.
func TestGRN_Review_StockUnitWhole(t *testing.T) {
	env := grntest.New(t)
	kgs, _ := strconv.ParseInt(g1MasterID(t, env.DB, "uom", "KGS"), 10, 64)
	nos, _ := strconv.ParseInt(g1MasterID(t, env.DB, "uom", "NOS"), 10, 64)
	kit := mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
	saved, err := item.New(kit).Create(t.Context(), env.Admin, item.Input{
		Code: "SPRING-50", Name: "Spring washer 50", Type: item.TypeGoods, HSNSAC: "7318",
		StockUoMID: nos, PurchaseUoMID: kgs,
		Conversions: []item.Conversion{{UoMID: kgs, Factor: 50 * types.Factor(1_000_000_000)}},
	})
	if err != nil {
		t.Fatalf("create SPRING-50: %v", err)
	}
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{
		"item_id": strconv.FormatInt(saved.ID, 10), "uom_id": strconv.FormatInt(kgs, 10), "qty": "10", "price": "500", "hsn_sac": "7318",
	}))
	stock := "Line 1: 0.01 KGS = 0.5 NOS, but NOS must be a whole number. Change the quantity."
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "0.01", "", ""))
	g2WantInvalid(t, "received 0.01 KGS", err, doctype.FieldError{Field: "received_qty", Line: 1, Message: stock})
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "1", "0.01", "Cracked"))
	g2WantInvalid(t, "rejected 0.01 KGS", err, doctype.FieldError{Field: "rejected_qty", Line: 1, Message: stock})
	if _, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "0.02", "", "")); err != nil {
		t.Errorf("received 0.02 KGS (1 NOS): %v %+v, want it saved", err, g1FieldErrors(err))
	}
}

// GRN spec §3.2 (po_line_id required; a line of purchase_order_id) and
// CD-302: a draft line with a blank po_line_id gets exactly "isn't on" on
// create and on a draft save; a step save that keeps it clears the line's
// uom_id and snapshots. Kills: checkLine's early return removed, changedPO
// without its !Valid test, and uom_id kept when po_line_id doesn't resolve.
func TestGRN_Review_BlankPOLine(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	notOn := doctype.FieldError{Field: "po_line_id", Line: 1, Message: "Line 1: This line isn't on PO/26-27/00001."}
	blank := map[string]string{"po_line_id": "", "received_qty": "10"}

	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po), blank)
	g2WantInvalid(t, "create with a blank PO line", err, notOn)

	d := g2MustCreate(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "10", "", ""))
	_, err = g2Save(t, env, env.Suresh, d.ID, nil, g2StoredLines(t, env, d.ID, map[int]map[string]string{1: {"po_line_id": ""}}))
	g2WantInvalid(t, "draft save with a blank PO line", err, notOn)

	res, err := g2Save(t, env, env.Suresh, d.ID, map[string]string{"remarks": "Line unknown"},
		g2StoredLines(t, env, d.ID, map[int]map[string]string{1: {"po_line_id": ""}}), "remarks")
	g2WantProblems(t, "step save of a blank PO line", res, err, notOn)
	for _, col := range []string{"uom_id", "snap_item_code", "snap_item_name", "snap_hsn_sac", "snap_uom_code", "snap_rate", "snap_is_service"} {
		if got := g2LineColumn(t, env, d.ID, col); len(got) != 1 || !g2Same(got[0], "") {
			t.Errorf("%s %v after the PO line was blanked, want cleared (CD-302)", col, got)
		}
	}
	g2CheckDerived(t, "blank PO line", env, d.ID)

	// With the PO blanked too, no line is marked foreign, and the line whose
	// po_line_id doesn't resolve still gets no quantity check (no "more than
	// twice the 0 ordered"). Kills: `|| !l.PoID.Valid` removed from
	// checkLine's guard.
	res, err = g2Save(t, env, env.Suresh, d.ID, map[string]string{"purchase_order_id": "", "remarks": "Nothing known"},
		g2StoredLines(t, env, d.ID, nil), "remarks")
	if err != nil {
		t.Fatalf("step save of a blank PO and a blank PO line: %v %+v", err, g1FieldErrors(err))
	}
	if want := []doctype.FieldError{{Field: "purchase_order_id", Message: g2MsgChoosePO}}; !slices.Equal(res.Problems, want) {
		t.Errorf("step save of a blank PO and a blank PO line: problems %+v, want exactly %+v", res.Problems, want)
	}
}

// GRN-007, GRN-008 (submit rules): a challan no. and a reject reason of
// spaces only are blank at submit.
func TestGRN_Review_SpacesAreBlank(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	h := g2Header(t, env, po)
	h["challan_no"] = "   "
	d := g2MustCreate(t, env, env.Suresh, h, g2Line(po.Lines[0], "100", "5", "   "))
	g2WantSubmitRefused(t, "spaces for challan and reason", env, d.ID,
		doctype.FieldError{Field: "reject_reason", Line: 1, Message: "Line 1: Reason for rejecting is required."},
		doctype.FieldError{Field: "challan_no", Message: g2MsgChallanNo})
}

// GRN-005 (proposed decision 15; plan G2 Do 1). A draft PO is made first,
// so the reference PO's id (2) differs from its vendor SHREE's id (1).
// A line that is both foreign and a repeat gets only "isn't on"; a foreign
// line gets no quantity error measured against the other PO's line (250 of
// that line's 100 would be over twice). Kills: the "isn't on" branch
// without its continue, and the PO number read through a join on
// vendor_id.
func TestGRN_Review_ForeignLineOnly(t *testing.T) {
	env := grntest.New(t)
	purchasetest.NewOrder(t, env.Env, purchasetest.StopAtDraft())
	po := g2RefPO(t, env)
	shree := g1MasterID(t, env.DB, "party", "SHREE")
	if strconv.FormatInt(po.ID, 10) == shree {
		t.Fatalf("reference PO id %d equals SHREE's id; the row needs them different", po.ID)
	}
	po2 := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved()) // 100 KGS

	_, err := g2Create(t, env, env.Suresh, g2Header(t, env, po),
		g2Line(po.Lines[0], "100", "", ""), g2Line(po2.Lines[0], "10", "", ""), g2Line(po2.Lines[0], "10", "", ""))
	g2WantInvalid(t, "a foreign line twice", err,
		doctype.FieldError{Field: "po_line_id", Line: 2, Message: "Line 2: This line isn't on PO/26-27/00001."},
		doctype.FieldError{Field: "po_line_id", Line: 3, Message: "Line 3: This line isn't on PO/26-27/00001."})

	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po2.Lines[0], "250", "", ""))
	g2WantInvalid(t, "a foreign line over twice its own PO line", err,
		doctype.FieldError{Field: "po_line_id", Line: 2, Message: "Line 2: This line isn't on PO/26-27/00001."})

	// Rejected above received reads only the line itself, so a foreign line
	// still gets it. Kills: `!foreign &&` added to the rejected check.
	_, err = g2Create(t, env, env.Suresh, g2Header(t, env, po), g2Line(po.Lines[0], "100", "", ""), g2Line(po2.Lines[0], "10", "20", "Wet"))
	g2WantInvalid(t, "a foreign line rejecting more than received", err,
		doctype.FieldError{Field: "po_line_id", Line: 2, Message: "Line 2: This line isn't on PO/26-27/00001."},
		doctype.FieldError{Field: "rejected_qty", Line: 2, Message: "Line 2: Rejected can't be more than received."})
}

// GRN-022 (CD-302): amending writes only the amendment. The cancelled GRN's
// columns, made wrong as in TestGRN_AmendRestrictedReal, are still as they
// were after the amend. Kills: G2SnapReceipt, G2SnapLines or
// G2SetAcceptedValue without its WHERE scope.
func TestGRN_Review_AmendLeavesCancelled(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}, {POLineID: po.Lines[1].ID, Received: "10"}}, doctype.StateApproved)
	env.Cancel(t, env.Ravi, grn.ID, "Wrong quantity entered")
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET snap_vendor_name = 'Stale Traders',
			snap_po_number = 'PO/26-27/09999', accepted_value = 1 WHERE id = ?`, grn.ID); err != nil {
			return err
		}
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE goods_receipt_line SET snap_rate = 1, snap_item_name = 'Stale item' WHERE parent_id = ?`, grn.ID)
		return err
	}); err != nil {
		t.Fatalf("make the cancelled GRN's copies wrong: %v", err)
	}

	amended := env.Amend(t, env.Suresh, grn.ID)
	g2CheckDerived(t, "amendment", env, amended.ID)
	for col, want := range map[string]string{"snap_vendor_name": "Stale Traders", "snap_po_number": "PO/26-27/09999", "accepted_value": "1"} {
		if got := g2Column(t, env, grn.ID, col); got != want {
			t.Errorf("cancelled GRN %s = %q after the amend, want %q unchanged", col, got, want)
		}
	}
	for col, want := range map[string]string{"snap_rate": "1", "snap_item_name": "Stale item"} {
		if got := g2LineColumn(t, env, grn.ID, col); len(got) != 2 || got[0] != want || got[1] != want {
			t.Errorf("cancelled GRN line %s %v after the amend, want %q unchanged", col, got, want)
		}
	}
	if c := env.Load(t, goodsreceipt.DocType, grn.ID); c.State != doctype.StateCancelled {
		t.Errorf("cancelled GRN state %s, want cancelled", c.State)
	}
}
