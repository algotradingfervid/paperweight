package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p3HeaderCols are the order's user header columns amend copies (PO spec §5:
// "Copies everything").
var p3HeaderCols = []string{"vendor_id", "vendor_address_id", "site_id", "branch_id", "project_id", "order_date", "required_by",
	"payment_terms_id", "vendor_quote_ref", "terms", "reverse_charge", "remarks", "earlier_po_no", "earlier_po_date"}

// p3LineCols are the order's user line columns amend copies, already_received
// included (PO spec §5).
var p3LineCols = []string{"line_no", "item_id", "description", "uom_id", "qty", "price", "discount_pct", "rate", "tax_code_id",
	"hsn_sac", "amount", "cgst", "sgst", "igst", "already_received"}

// PO-024 (PO spec §5, §6.1, §10, §18): Ravi cancels approved PO/26-27/00001
// and Priya amends it (Purchase User holds amend). The amendment copies every
// field and line, already_received included, and is Priya's; submitted and
// approved by Ravi it is PO/26-27/00001-1.
func TestPO_AmendCopies(t *testing.T) {
	env := purchasetest.New(t)
	lines := p2RefLines(t, env)
	lines[0]["already_received"] = "200"
	lines[1]["description"] = "Binding wire, 18 gauge"
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(lines...),
		purchasetest.OrderHeader(map[string]string{"vendor_quote_ref": "SQ-2026-114", "remarks": "Deliver before 10 am",
			"terms": "Prices firm until delivery", "earlier_po_no": "P-17", "earlier_po_date": "02-04-2026"}))
	if po.Number != "PO/26-27/00001" || po.CreatedBy != env.Priya.ID {
		t.Fatalf("original PO %q by %d, want PO/26-27/00001 by Priya", po.Number, po.CreatedBy)
	}
	header, origLines := p2Snap(t, env, po.ID, p3HeaderCols...), p2LineSnap(t, env, po.ID, p3LineCols...)
	if header["earlier_po_no"] != "P-17" || len(origLines) != 2 || origLines[0]["already_received"] == "" || origLines[0]["already_received"] == "0" {
		t.Fatalf("original PO header %v lines %v, want the values given (already_received on line 1)", header, origLines)
	}

	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Wrong rate on line 2"))
	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	am := p2Load(t, env, res.ID)
	if am.ID == po.ID || am.State != doctype.StateDraft || am.CreatedBy != env.Priya.ID || am.AmendedFromID != po.ID {
		t.Fatalf("amendment = id %d state %q by %d from %d, want a new draft by Priya amended from %d", am.ID, am.State, am.CreatedBy, am.AmendedFromID, po.ID)
	}
	if got := p2Snap(t, env, am.ID, p3HeaderCols...); !mapsEqual(got, header) {
		t.Errorf("amendment header = %v, want the original's %v", got, header)
	}
	got := p2LineSnap(t, env, am.ID, p3LineCols...)
	if len(got) != len(origLines) {
		t.Fatalf("amendment has %d lines, want %d", len(got), len(origLines))
	}
	for i := range got {
		if !mapsEqual(got[i], origLines[i]) {
			t.Errorf("amendment line %d = %v, want the original's %v", i+1, got[i], origLines[i])
		}
	}

	p2Must(t, "Priya submits the amendment")(p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""))
	if st := p2Load(t, env, am.ID).State; st != doctype.StatePending {
		t.Fatalf("submitted amendment is %q, want pending (₹94,164.00 is under the Purchase Manager rule)", st)
	}
	p2Must(t, "Ravi approves the amendment")(p2Do(t, env, env.Ravi, am.ID, doc.ActionApprove, ""))
	if fin := p2Load(t, env, am.ID); fin.State != doctype.StateApproved || fin.Number != "PO/26-27/00001-1" {
		t.Errorf("approved amendment: state %q number %q, want approved PO/26-27/00001-1", fin.State, fin.Number)
	}
}

// p3SnapCols are the order's snapshot and total columns, which amend doesn't
// copy (System fields and module columns) and only Validate writes.
var p3SnapCols = []string{"snap_vendor_id", "snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin", "snap_vendor_category",
	"snap_vendor_address", "snap_vendor_state", "snap_site_name", "snap_branch_name", "snap_branch_gstin", "snap_place_state",
	"net_total", "cgst_total", "sgst_total", "igst_total", "grand_total", "rounded_total", "receipt_status"}

// p3LineSnapCols are the line snapshot columns only Validate writes.
var p3LineSnapCols = []string{"snap_item_code", "snap_item_name", "snap_uom_code", "snap_tax_code", "snap_gst_rate", "snap_factor", "snap_stock_qty"}

// PO-047 (PO spec §15 item 8, §18; M0-10): a cancelled PO's amendment has its
// snapshot columns filled at once, without a further save, because amend
// runs Validate.
func TestPO_AmendFillsSnapshots(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(p2RefLines(t, env)...))
	header, lines := p2Snap(t, env, po.ID, p3SnapCols...), p2LineSnap(t, env, po.ID, p3LineSnapCols...)
	for _, c := range p3SnapCols {
		if header[c] == "" {
			t.Fatalf("original PO's %s is empty; want every snapshot column filled (header %v)", c, header)
		}
	}
	p2Totals(t, "original PO", env, po.ID, p2Intra)

	p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, "Vendor changed the rate"))
	res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
	if res.State != doctype.StateDraft || res.Version != 1 {
		t.Fatalf("amend = %+v, want a new draft at version 1 (no further save)", res)
	}
	if got := p2Snap(t, env, res.ID, p3SnapCols...); !mapsEqual(got, header) {
		t.Errorf("amended draft snapshots = %v, want %v filled by amend's Validate", got, header)
	}
	p2Totals(t, "amended draft", env, res.ID, p2Intra)
	got := p2LineSnap(t, env, res.ID, p3LineSnapCols...)
	if len(got) != len(lines) {
		t.Fatalf("amended draft has %d lines, want %d", len(got), len(lines))
	}
	for i := range got {
		for _, c := range p3LineSnapCols {
			if got[i][c] == "" || got[i][c] != lines[i][c] {
				t.Errorf("amended draft line %d %s = %q, want %q (filled by amend's Validate)", i+1, c, got[i][c], lines[i][c])
			}
		}
	}
	if v := p2Version(t, env, res.ID); v != 1 {
		t.Errorf("amended draft version = %d, want 1 (no save after amend)", v)
	}
}
