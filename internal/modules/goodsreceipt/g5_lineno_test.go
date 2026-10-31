package goodsreceipt_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// g5MixPO is PO/26-27/00001 by Priya with line 1 MS-ANGLE-50 1,200 kg at
// ₹62.50 and line 2 BOLT-M12 100 NOS (a whole-number UoM) at ₹12.
func g5MixPO(t *testing.T, env *grntest.Env) doc.Doc {
	t.Helper()
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "1200", "price": "62.5"},
		map[string]string{"item_id": g1MasterID(t, env.DB, "item", "BOLT-M12"), "uom_id": g1MasterID(t, env.DB, "uom", "NOS"),
			"qty": "100", "price": "12", "hsn_sac": "7318"},
	))
	if po.Number != "PO/26-27/00001" || len(po.Lines) != 2 {
		t.Fatalf("PO = %q with %d lines, want PO/26-27/00001 with 2", po.Number, len(po.Lines))
	}
	return po
}

// Line numbers in module texts (AD-12 ruling from the approved UX §5.4
// decision P2, UX lines 395-398): the module's own line texts number the
// line by its PO line (purchase_order_line.line_no), never by the GRN's
// posted position, while FieldError.Line stays the posted position
// (docscreen places errors by it). Each GRN here receives only PO line 2,
// so posted position 1 is PO line 2: the twice stop, rejected above
// received, the whole-number text (uom.CheckLineQty's wording), the reason
// required at submit, {m} in the duplicate refusal and the over-delivery
// warnings all say "Line 2:" on Line 1. The foreign-line refusal and the
// duplicate refusal's leading "Line {n}:" keep the posted position (UX line
// 398).
func TestGRN_LineNoIsPOLine(t *testing.T) {
	env := grntest.New(t)
	po := g5MixPO(t, env)
	p1, p2 := po.Lines[0], po.Lines[1]
	other := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"qty": "40"}))
	header := func() map[string]string { return g2Header(t, env, po) }

	counts := env.RowCounts(t)
	_, err := g2Create(t, env, env.Suresh, header(), g2Line(p2, "201", "", ""))
	g2WantInvalid(t, "twice stop on PO line 2 at position 1", err, doctype.FieldError{Field: "received_qty", Line: 1,
		Message: "Line 2: Accepted 201 NOS is more than twice the 100 NOS ordered. Check the quantity."})
	_, err = g2Create(t, env, env.Suresh, header(), g2Line(p2, "10", "12", "Broken"))
	g2WantInvalid(t, "rejected above received on PO line 2 at position 1", err, doctype.FieldError{Field: "rejected_qty", Line: 1,
		Message: "Line 2: Rejected can't be more than received."})
	_, err = g2Create(t, env, env.Suresh, header(), g2Line(p2, "10.5", "", ""))
	g2WantInvalid(t, "fractional NOS on PO line 2 at position 1", err, doctype.FieldError{Field: "received_qty", Line: 1,
		Message: "Line 2: Quantity 10.5 can't be a fraction for UoM NOS. Enter a whole number."})
	_, err = g2Create(t, env, env.Suresh, header(), g2Line(p2, "10", "", ""), g2Line(p1, "5", "", ""), g2Line(p2, "3", "", ""))
	g2WantInvalid(t, "PO line 2 again at position 3", err, doctype.FieldError{Field: "po_line_id", Line: 3,
		Message: "Line 3: This PO line is already on line 2."})
	_, err = g2Create(t, env, env.Suresh, header(), g2Line(p2, "10", "", ""), g2Line(other.Lines[0], "10", "", ""))
	g2WantInvalid(t, "another PO's line 1 at position 2 (posted position kept)", err, doctype.FieldError{Field: "po_line_id", Line: 2,
		Message: "Line 2: This line isn't on PO/26-27/00001."})
	if got := env.RowCounts(t); got != counts {
		t.Errorf("row counts after the refusals %+v, want %+v", got, counts)
	}

	noReason := g2MustCreate(t, env, env.Suresh, header(), g2Line(p2, "10", "2", ""))
	g2WantSubmitRefused(t, "no reason on PO line 2 at position 1", env, noReason.ID, doctype.FieldError{Field: "reject_reason", Line: 1,
		Message: "Line 2: Reason for rejecting is required."})

	over := g2MustCreate(t, env, env.Suresh, header(), g2Line(p2, "150", "", ""))
	g5WantList(t, "Warnings: 150 of 100 NOS on PO line 2 at position 1", g5Warnings(t, env, over.ID),
		g5Over(2, "150 NOS", "100 NOS", "PO/26-27/00001"))
	g5WantMap(t, "LineWarnings: 150 typed on PO line 2 only", g5LineWarnings(t, env, po.ID, 0, map[int64]types.Qty{p2.ID: g3Q("150")}),
		map[int64]string{p2.ID: g5Over(2, "150 NOS", "100 NOS", "PO/26-27/00001")})
}
