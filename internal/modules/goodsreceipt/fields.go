package goodsreceipt

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// stepDelivery is the Delivery details wizard step (CD-282 Q7).
const stepDelivery = "delivery"

// fields are GRN spec §3.1 and §3.2, with the UX doc's list priorities (P4).
// site_id, project_id and po_line_id are posted by the module's steps, so
// they are plain Int fields, not ReadOnly (checkReadOnly refuses them on
// create, CD-247 (1)); Validate resets them from the PO. vendor_id and
// uom_id are System and ReadOnly, copied from the PO by Validate.
// accepted_value and accepted_qty are computed by Validate. The snapshot
// columns are not Fields. The More fields are the rarely used delivery
// ones (CD-359 (7)); none is one submit may require (SubmitRequired).
func fields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "GRN number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		{Name: "purchase_order_id", Label: "Purchase order", Type: doctype.Link, Link: purchase.DocType, Required: true, ListPriority: 5},
		{Name: "vendor_id", Label: "Vendor", Type: doctype.Link, Link: party.DocType, System: true, ReadOnly: true, ListPriority: 4},
		{Name: "site_id", Label: "Site", Type: doctype.Int},
		{Name: "project_id", Label: "Project", Type: doctype.Int},
		{Name: "receipt_date", Label: "Receipt date", Type: doctype.Date, Required: true, ListPriority: 4},
		{Name: "challan_kind", Label: "Document with the goods", Type: doctype.Select, Step: stepDelivery,
			Options: []string{"Delivery challan", "Tax invoice"}},
		{Name: "challan_no", Label: "Challan or invoice no.", Type: doctype.Text, MaxLen: 40, Step: stepDelivery},
		{Name: "challan_date", Label: "Challan date", Type: doctype.Date, Step: stepDelivery},
		{Name: "vehicle_no", Label: "Vehicle no.", Type: doctype.Text, MaxLen: 20, Step: stepDelivery, More: true},
		{Name: "transporter", Label: "Transporter", Type: doctype.Text, MaxLen: 140, Step: stepDelivery, More: true},
		{Name: "eway_bill_no", Label: "E-way bill no.", Type: doctype.Text, MaxLen: 20, Step: stepDelivery, More: true},
		{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500, Step: stepDelivery, More: true},
		{Name: "rest_not_coming", Label: "Vendor says the rest won't come", Type: doctype.Check, Step: stepDelivery},
		{Name: "accepted_value", Label: "Accepted value", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
		{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 3,
			Options: []string{
				string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
				string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
			}},

		{Name: "po_line_id", Label: "PO line", Type: doctype.Int, Required: true, OnLine: true},
		{Name: "uom_id", Label: "UoM", Type: doctype.Link, Link: uom.DocType, System: true, ReadOnly: true, OnLine: true},
		{Name: "received_qty", Label: "Received", Type: doctype.Qty, Required: true, Positive: true, OnLine: true},
		{Name: "rejected_qty", Label: "Rejected", Type: doctype.Qty, NonNegative: true, Default: "0", OnLine: true},
		{Name: "accepted_qty", Label: "Accepted", Type: doctype.Qty, ReadOnly: true, OnLine: true},
		{Name: "reject_reason", Label: "Reason for rejecting", Type: doctype.Text, MaxLen: 200, OnLine: true},
	}
}
