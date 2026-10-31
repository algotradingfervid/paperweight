package goodsreceipt_test

import (
	"testing"
)

// MOD-GRN G1 Do 1 (GRN spec §3.1, §3.2; UX doc §5.1 P4): every declared
// field, header then lines, with spec §3's label and list priority: number 1,
// accepted value 2, state 3 (spec); vendor 4, receipt date 4, purchase
// order 5 (P4); no other field is a list column. No field is missing or extra.
func TestGRN_Fields_LabelsAndListPriorities(t *testing.T) {
	dt := g1DocType(t)
	want := []struct {
		name, label string
		priority    int
	}{
		{"number", "GRN number", 1},
		{"purchase_order_id", "Purchase order", 5},
		{"vendor_id", "Vendor", 4},
		{"site_id", "Site", 0},
		{"project_id", "Project", 0},
		{"receipt_date", "Receipt date", 4},
		{"challan_kind", "Document with the goods", 0},
		{"challan_no", "Challan or invoice no.", 0},
		{"challan_date", "Challan date", 0},
		{"vehicle_no", "Vehicle no.", 0},
		{"transporter", "Transporter", 0},
		{"eway_bill_no", "E-way bill no.", 0},
		{"remarks", "Remarks", 0},
		{"rest_not_coming", "Vendor says the rest won't come", 0},
		{"accepted_value", "Accepted value", 2},
		{"state", "State", 3},
		{"po_line_id", "PO line", 0},
		{"uom_id", "UoM", 0},
		{"received_qty", "Received", 0},
		{"rejected_qty", "Rejected", 0},
		{"accepted_qty", "Accepted", 0},
		{"reject_reason", "Reason for rejecting", 0},
	}
	if len(dt.Fields) != len(want) {
		t.Errorf("%s declares %d fields, want %d", dt.Name, len(dt.Fields), len(want))
	}
	for _, w := range want {
		f := g1Field(t, dt, w.name)
		if f.Label != w.label || f.ListPriority != w.priority {
			t.Errorf("%s: Label %q ListPriority %d, want %q %d", w.name, f.Label, f.ListPriority, w.label, w.priority)
		}
	}
}
