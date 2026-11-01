package purchase

import (
	"cmp"
	"slices"

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
// and never copied by amend (doc/terminal.go:162); they (but copy_notes,
// which Copy sets) and the RollUp are ReadOnly, so no form draws an input
// for them (P34). request_line_id,
// copied_from_no and copied_from_id are set by module routes on create, so
// they are not ReadOnly (checkReadOnly refuses them); Module.Hide hides them
// and Validate owns their rules (CD-247 (1)).
func orderFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "PO number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		{Name: "vendor_id", Label: "Vendor", Type: doctype.Link, Link: party.DocType, LinkRole: "vendor", Required: true, Filterable: true, ListPriority: 4,
			Hint: "Pick the party you are buying from; only vendors are listed.", Placeholder: "Name, code or GSTIN"},
		{Name: "vendor_address_id", Label: "Vendor address", Type: doctype.Link, Link: address.DocType, Step: stepTerms,
			Hint: "Pick the address to print on the order; the vendor's primary billing address is chosen for you."},
		{Name: "site_id", Label: "Deliver to", Type: doctype.Link, Link: site.DocType, Required: true, Filterable: true,
			Hint: "Pick the site the goods are to be delivered to.", Placeholder: "Name or code"},
		{Name: "branch_id", Label: "Billed to", Type: doctype.Link, Link: company.DocType, ReadOnly: true},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, Required: true, Filterable: true, ListPriority: 6,
			Hint: "Left blank, the site's project is used.", Placeholder: "Name or code"},
		{Name: "order_date", Label: "Order date", Type: doctype.Date, Required: true, Filterable: true, Sortable: true, ListPriority: 5,
			Hint: "Today or an earlier date; it sets the financial year in the PO number."},
		{Name: "required_by", Label: "Required by", Type: doctype.Date,
			Hint: "The date you need the goods at the site, on or after the order date."},
		{Name: "payment_terms_id", Label: "Payment terms", Type: doctype.Link, Link: paymentterms.DocType, Step: stepTerms,
			Hint: "Left blank, the vendor's usual terms are used, for example 30 days from the bill date."},
		{Name: "vendor_quote_ref", Label: "Vendor quote ref", Type: doctype.Text, MaxLen: 140, Step: stepTerms, More: true,
			Hint: "The vendor's quotation number, printed on the order, for example QT-2026-118.", Placeholder: "e.g. QT-2026-118"},
		{Name: "terms", Label: "Terms and conditions", Type: doctype.Text, MaxLen: 2000, Step: stepTerms,
			Hint: "Left blank, the company's standard terms are used; type a dash to print none."},
		{Name: "reverse_charge", Label: "Reverse charge applies", Type: doctype.Check, Step: stepTerms,
			Hint: "Tick only if your CA has said reverse charge applies, so GST is shown as payable by us and left out of the total."},
		{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500, Step: stepTerms,
			Hint: "For people in the company only, and never printed.", Placeholder: "e.g. Rate agreed with the vendor on phone"},
		{Name: "earlier_po_no", Label: "Earlier PO no.", Type: doctype.Text, MaxLen: 40, Step: stepTerms, More: true,
			Hint: "Only for orders first raised on paper; printed on the order so the vendor recognises it.", Placeholder: "e.g. SI/PO/0412"},
		{Name: "earlier_po_date", Label: "Earlier PO date", Type: doctype.Date, Step: stepTerms, More: true,
			Hint: "The date of the paper order, given together with Earlier PO no."},
		{Name: "copied_from_no", Label: "Copied from", Type: doctype.Text, MaxLen: 40},
		{Name: "copied_from_id", Label: "Copied from id", Type: doctype.Int},
		{Name: "copy_notes", Label: "Copy notes", Type: doctype.Text, System: true},
		{Name: "net_total", Label: "Taxable value", Type: doctype.Money, System: true, ReadOnly: true},
		{Name: "cgst_total", Label: "CGST", Type: doctype.Money, System: true, ReadOnly: true},
		{Name: "sgst_total", Label: "SGST", Type: doctype.Money, System: true, ReadOnly: true},
		{Name: "igst_total", Label: "IGST", Type: doctype.Money, System: true, ReadOnly: true},
		{Name: "grand_total", Label: "Grand total", Type: doctype.Money, System: true, ReadOnly: true},
		{Name: "rounded_total", Label: "Rounded total", Type: doctype.Money, System: true, ReadOnly: true, Sortable: true, ListPriority: 2},
		{Name: "receipt_status", Label: "Receipt", Type: doctype.Select, System: true, ReadOnly: true, Filterable: true,
			Options:      []string{"not_received", "part_received", "fully_received"},
			OptionLabels: map[string]string{"not_received": "Not received", "part_received": "Part received", "fully_received": "Fully received"}}, // UX §5.2, CD-325
		stateField(),

		{Name: "item_id", Label: "Item", Type: doctype.Link, Link: item.DocType, Required: true, OnLine: true,
			Hint: "Pick the item to buy.", Placeholder: "Name or code"},
		{Name: "description", Label: "Description", Type: doctype.Text, MaxLen: 500, OnLine: true,
			Hint: "Left blank, the item's name is used; it is printed on the order."},
		{Name: "uom_id", Label: "UoM", Type: doctype.Link, Link: uom.DocType, Required: true, OnLine: true,
			Hint: "Left blank, the item's purchase unit is used, or else its stock unit; another unit needs a conversion on the item."},
		{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true,
			Hint: "How many to order in the chosen UoM, more than 0."},
		{Name: "price", Label: "Rate", Type: doctype.Rate, NonNegative: true, OnLine: true,
			Hint: "Left blank, the last price paid to this vendor on this project is used, or else the item's standard rate."},
		{Name: "discount_pct", Label: "Discount", Type: doctype.Percent, NonNegative: true, Max: i64(maxDiscount), Default: "0", OnLine: true,
			Hint: "The vendor's discount on the rate, from 0 to 100, for example 5 for 5%."},
		{Name: "rate", Label: "Net rate", Type: doctype.Rate, ReadOnly: true, OnLine: true},
		{Name: "tax_code_id", Label: "Tax code", Type: doctype.Link, Link: taxcode.DocType, Required: true, OnLine: true,
			Hint: "Left blank, the item's tax code is used."},
		{Name: "hsn_sac", Label: "HSN/SAC", Type: doctype.Text, OnLine: true,
			Hint: "Left blank, the item's HSN or SAC code is used."},
		{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "cgst", Label: "CGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "sgst", Label: "SGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "igst", Label: "IGST", Type: doctype.Money, ReadOnly: true, OnLine: true},
		{Name: "already_received", Label: "Already received", Type: doctype.Qty, NonNegative: true, Default: "0", OnLine: true,
			Hint: "What arrived before this order was entered in the app."},
		{Name: "received_qty", Label: "Received", Type: doctype.Qty, RollUp: true, ReadOnly: true, OnLine: true},
		{Name: "request_line_id", Label: "Request row", Type: doctype.Int, OnLine: true},
	}
}

// requestFields are PO spec §20.2. order_status and ordered_qty are System
// fields kept by RefreshOrderStatus (§20.3), not a RollUp (CD-247 (4)), and
// ReadOnly, so no form draws an input for them (P34).
func requestFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "Request number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		{Name: "site_id", Label: "Needed at", Type: doctype.Link, Link: site.DocType, Required: true, Filterable: true,
			Hint: "Pick the site the items are needed at.", Placeholder: "Name or code"},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, Required: true, Filterable: true,
			Hint: "Left blank, the site's project is used.", Placeholder: "Name or code"},
		{Name: "request_date", Label: "Request date", Type: doctype.Date, Required: true, Sortable: true},
		{Name: "required_by", Label: "Needed by", Type: doctype.Date,
			Hint: "The date you need the items, on or after the request date."},
		{Name: "purpose", Label: "What is it for?", Type: doctype.Text, MaxLen: 500,
			Hint: "For example, \"Slab casting on level 3\".", Placeholder: "e.g. Slab casting on level 3, Tower B"},
		{Name: "order_status", Label: "Ordered", Type: doctype.Select, System: true, ReadOnly: true, Filterable: true, ListPriority: 2,
			Options:      []string{"not_ordered", "part_ordered", "ordered"},
			OptionLabels: map[string]string{"not_ordered": "Not ordered", "part_ordered": "Part ordered", "ordered": "Ordered"}}, // UX §5.2, §5.14, CD-325
		stateField(),

		{Name: "item_id", Label: "Item", Type: doctype.Link, Link: item.DocType, Required: true, OnLine: true,
			Hint: "Pick the item you need.", Placeholder: "Name or code"},
		{Name: "description", Label: "Description", Type: doctype.Text, MaxLen: 500, OnLine: true,
			Hint: "Left blank, the item's name is used."},
		{Name: "uom_id", Label: "Unit", Type: doctype.Link, Link: uom.DocType, Required: true, OnLine: true,
			Hint: "Left blank, the item's purchase unit is used, or else its stock unit."},
		{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true,
			Hint: "How many you need in the chosen unit, more than 0."},
		{Name: "note", Label: "Note", Type: doctype.Text, MaxLen: 200, OnLine: true, More: true,
			Hint: "For example, a brand or size.", Placeholder: "e.g. UltraTech, 53 grade"},
		{Name: "ordered_qty", Label: "Ordered", Type: doctype.Qty, System: true, ReadOnly: true, OnLine: true},
	}
}

// ReceiptLabel is an order's receipt_status as users see it: its
// OptionLabels label, else the value (CD-325).
func ReceiptLabel(v string) string { return optionLabel(orderFields(), "receipt_status", v) }

// orderStatusLabel is a request's order_status as users see it (CD-325).
func orderStatusLabel(v string) string { return optionLabel(requestFields(), "order_status", v) }

// optionLabel is value v of the Select field name in fields, through its
// OptionLabels.
func optionLabel(fields []doctype.Field, name, v string) string {
	i := slices.IndexFunc(fields, func(f doctype.Field) bool { return f.Name == name })
	return cmp.Or(fields[i].OptionLabels[v], v)
}
