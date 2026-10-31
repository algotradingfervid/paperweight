package purchase

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// stepTerms is the Order's Terms wizard step (PO spec §3).
const stepTerms = "terms"

// maxDiscount is 100% in thousandths of a percent (NUM-015).
const maxDiscount int64 = 100_000

func i64(v int64) *int64 { return &v }

// stateField is the state convention column, for the list and its filter
// (CD-144).
func stateField() doctype.Field {
	return doctype.Field{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 3,
		Options: []string{
			string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
			string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
		}}
}

// orderFields are PO spec §3.1 and §3.2. System fields are kept by the module
// and never copied by amend (doc/terminal.go:162). request_line_id,
// copied_from_no and copied_from_id are set by module routes on create, so
// they are not ReadOnly (checkReadOnly refuses them); Module.Hide hides them
// and Validate owns their rules (CD-247 (1)).
func orderFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "PO number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		{Name: "vendor_id", Label: "Vendor", Type: doctype.Link, Link: party.DocType, LinkRole: "vendor", Required: true, Filterable: true, ListPriority: 4},
		{Name: "vendor_address_id", Label: "Vendor address", Type: doctype.Link, Link: address.DocType, Step: stepTerms},
		{Name: "site_id", Label: "Deliver to", Type: doctype.Link, Link: site.DocType, Required: true, Filterable: true},
		{Name: "branch_id", Label: "Billed to", Type: doctype.Link, Link: company.DocType, ReadOnly: true},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, Required: true, Filterable: true, ListPriority: 6},
		{Name: "order_date", Label: "Order date", Type: doctype.Date, Required: true, Filterable: true, Sortable: true, ListPriority: 5},
		{Name: "required_by", Label: "Required by", Type: doctype.Date},
		{Name: "payment_terms_id", Label: "Payment terms", Type: doctype.Link, Link: paymentterms.DocType, Step: stepTerms},
		{Name: "vendor_quote_ref", Label: "Vendor quote ref", Type: doctype.Text, MaxLen: 140, Step: stepTerms},
		{Name: "terms", Label: "Terms and conditions", Type: doctype.Text, MaxLen: 2000, Step: stepTerms},
		{Name: "reverse_charge", Label: "Reverse charge applies", Type: doctype.Check, Step: stepTerms},
		{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500, Step: stepTerms},
		{Name: "earlier_po_no", Label: "Earlier PO no.", Type: doctype.Text, MaxLen: 40, Step: stepTerms},
		{Name: "earlier_po_date", Label: "Earlier PO date", Type: doctype.Date, Step: stepTerms},
		{Name: "copied_from_no", Label: "Copied from", Type: doctype.Text, MaxLen: 40},
		{Name: "copied_from_id", Label: "Copied from id", Type: doctype.Int},
		{Name: "copy_notes", Label: "Copy notes", Type: doctype.Text, System: true},
		{Name: "net_total", Label: "Taxable value", Type: doctype.Money, System: true},
		{Name: "cgst_total", Label: "CGST", Type: doctype.Money, System: true},
		{Name: "sgst_total", Label: "SGST", Type: doctype.Money, System: true},
		{Name: "igst_total", Label: "IGST", Type: doctype.Money, System: true},
		{Name: "grand_total", Label: "Grand total", Type: doctype.Money, System: true},
		{Name: "rounded_total", Label: "Rounded total", Type: doctype.Money, System: true, Sortable: true, ListPriority: 2},
		{Name: "receipt_status", Label: "Receipt", Type: doctype.Select, System: true, Filterable: true,
			Options: []string{"not_received", "part_received", "fully_received"}},
		stateField(),

		{Name: "item_id", Label: "Item", Type: doctype.Link, Link: item.DocType, Required: true, OnLine: true},
		{Name: "description", Label: "Description", Type: doctype.Text, MaxLen: 500, OnLine: true},
		{Name: "uom_id", Label: "UoM", Type: doctype.Link, Link: uom.DocType, Required: true, OnLine: true},
		{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true},
		{Name: "price", Label: "Rate", Type: doctype.Rate, NonNegative: true, OnLine: true},
		{Name: "discount_pct", Label: "Discount", Type: doctype.Percent, NonNegative: true, Max: i64(maxDiscount), Default: "0", OnLine: true},
		{Name: "rate", Label: "Net rate", Type: doctype.Rate, ReadOnly: true, OnLine: true},
		{Name: "tax_code_id", Label: "Tax code", Type: doctype.Link, Link: taxcode.DocType, Required: true, OnLine: true},
		{Name: "hsn_sac", Label: "HSN/SAC", Type: doctype.Text, OnLine: true},
		{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "cgst", Label: "CGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "sgst", Label: "SGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "igst", Label: "IGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "already_received", Label: "Already received", Type: doctype.Qty, NonNegative: true, Default: "0", OnLine: true},
		{Name: "received_qty", Label: "Received", Type: doctype.Qty, RollUp: true, OnLine: true},
		{Name: "request_line_id", Label: "Request row", Type: doctype.Int, OnLine: true},
	}
}

// requestFields are PO spec §20.2. order_status and ordered_qty are System
// fields kept by RefreshOrderStatus (§20.3), not a RollUp (CD-247 (4)).
func requestFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "Request number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		{Name: "site_id", Label: "Needed at", Type: doctype.Link, Link: site.DocType, Required: true, Filterable: true},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, Required: true, Filterable: true},
		{Name: "request_date", Label: "Request date", Type: doctype.Date, Required: true, Sortable: true},
		{Name: "required_by", Label: "Needed by", Type: doctype.Date},
		{Name: "purpose", Label: "What is it for?", Type: doctype.Text, MaxLen: 500},
		{Name: "order_status", Label: "Ordered", Type: doctype.Select, System: true, Filterable: true, ListPriority: 2,
			Options: []string{"not_ordered", "part_ordered", "ordered"}},
		stateField(),

		{Name: "item_id", Label: "Item", Type: doctype.Link, Link: item.DocType, Required: true, OnLine: true},
		{Name: "description", Label: "Description", Type: doctype.Text, MaxLen: 500, OnLine: true},
		{Name: "uom_id", Label: "Unit", Type: doctype.Link, Link: uom.DocType, Required: true, OnLine: true},
		{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true},
		{Name: "note", Label: "Note", Type: doctype.Text, MaxLen: 200, OnLine: true},
		{Name: "ordered_qty", Label: "Ordered", Type: doctype.Qty, System: true, OnLine: true},
	}
}
