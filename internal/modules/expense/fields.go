package expense

import (
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
)

// states are the state convention column's options, for list filters
// (CD-144, CD-162).
var states = []string{
	string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved),
	string(doctype.StateClosed), string(doctype.StateRejected), string(doctype.StateCancelled),
}

// employeeField is the claimant on both DocTypes (spec §3.1). Blank defaults
// in Validate to the updater's employee; AllowBlocked keeps a leaver's
// documents moving (spec §3.1 Leavers, CD-237).
var employeeField = doctype.Field{Name: "employee_id", Label: "Employee", Type: doctype.Link, Link: employee.DocType, AllowBlocked: true}

// claimFields are spec §3.1 (header) and §3.2 (lines). Snapshot columns are
// not fields. number and state map the convention columns (CD-144).
func claimFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "Claim number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		employeeField,
		{Name: "claim_date", Label: "Claim date", Type: doctype.Date, Required: true},
		{Name: "purpose", Label: "Purpose", Type: doctype.Text, Required: true, MaxLen: 140},
		{Name: "advance_id", Label: "Advance", Type: doctype.Link, Link: AdvanceDocType},
		{Name: "less_advance", Label: "Less advance", Type: doctype.Money},
		{Name: "total", Label: "Total", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
		{Name: "net_payable", Label: "To pay", Type: doctype.Money, ReadOnly: true},
		{Name: "period_from", Label: "Period from", Type: doctype.Date, ReadOnly: true},
		{Name: "period_to", Label: "Period to", Type: doctype.Date, ReadOnly: true},
		{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 3, Options: states},
		{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},

		{Name: "expense_date", Label: "Date", Type: doctype.Date, Required: true, OnLine: true},
		{Name: "expense_type_id", Label: "Type", Type: doctype.Link, Link: expensetype.DocType, Required: true, OnLine: true},
		{Name: "description", Label: "Note", Type: doctype.Text, MaxLen: 200, OnLine: true},
		{Name: "amount", Label: "Amount", Type: doctype.Money, Required: true, Positive: true, OnLine: true},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, OnLine: true},
		{Name: "site_id", Label: "Site", Type: doctype.Link, Link: site.DocType, OnLine: true},
		{Name: "receipt_sha256", Label: "Receipt", Type: doctype.Text, MaxLen: 64, OnLine: true},
		{Name: "no_receipt_reason", Label: "Why no receipt", Type: doctype.Text, MaxLen: 200, OnLine: true},
		{Name: "company_bill", Label: "Bill in company GSTIN", Type: doctype.Check, OnLine: true},
		{Name: "supplier_gstin", Label: "Supplier GSTIN", Type: doctype.Text, OnLine: true},
		{Name: "invoice_no", Label: "Invoice no.", Type: doctype.Text, MaxLen: 40, OnLine: true},
		{Name: "billed_to_id", Label: "Billed to", Type: doctype.Link, Link: company.DocType, OnLine: true},
		{Name: "tax_code_id", Label: "Tax code", Type: doctype.Link, Link: taxcode.DocType, OnLine: true},
		{Name: "hsn_sac", Label: "HSN/SAC", Type: doctype.Text, OnLine: true},
		{Name: "gst_amount", Label: "GST on the bill", Type: doctype.Money, OnLine: true},
	}
}

// advanceFields are spec §4's header and lines. The line amount clashes with
// the header amount, so it is line_amount on column amount (CD-151).
// used_amount is written only by the claim effects and paid_on only by Mark
// as paid (CD-271 G1).
func advanceFields() []doctype.Field {
	return []doctype.Field{
		{Name: "number", Label: "Advance number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
		employeeField,
		{Name: "advance_date", Label: "Advance date", Type: doctype.Date, Required: true},
		{Name: "purpose", Label: "Purpose", Type: doctype.Text, Required: true, MaxLen: 140},
		{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
		{Name: "used_amount", Label: "Used", Type: doctype.Money, ReadOnly: true, System: true, EditableAfterSubmit: true},
		{Name: "paid_on", Label: "Paid on", Type: doctype.Date, ReadOnly: true, System: true, EditableAfterSubmit: true},
		{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 3, Options: states},

		{Name: "expense_type_id", Label: "Type", Type: doctype.Link, Link: expensetype.DocType, Required: true, OnLine: true},
		{Name: "description", Label: "Note", Type: doctype.Text, MaxLen: 200, OnLine: true},
		{Name: "line_amount", Label: "Amount", Column: "amount", Type: doctype.Money, Required: true, Positive: true, OnLine: true},
		{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType, OnLine: true},
		{Name: "site_id", Label: "Site", Type: doctype.Link, Link: site.DocType, OnLine: true},
	}
}
