package expense

import "github.com/vardhan/internaltools/internal/kernel/doctype"

// The DocType names (spec §3, §4).
const (
	ClaimDocType   = "expense.Claim"
	AdvanceDocType = "expense.Advance"
)

func init() {
	doctype.Register(doctype.DocType{
		Name: ClaimDocType, Label: "Expense Claim", PluralLabel: "Expense Claims",
		Table: "expense_claim", LinesTable: "expense_claim_line", Series: "EX/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true,
		OwnerField: "created_by", EmployeeField: "employee_id",
		CloseRight: "cancel", // Director and Administrator close (spec §6, CD-255)
		// An advance can't be cancelled while a live claim names it (DOC-052).
		Links:         []doctype.LinkSpec{{Field: "advance_id", Target: AdvanceDocType}},
		SearchFields:  []string{"number", "purpose"}, // CD-317
		DefaultRights: defaultRights(),
		Fields:        claimFields(),
		Hooks:         claimHooks{},
	})
	doctype.Register(doctype.DocType{
		Name: AdvanceDocType, Label: "Employee Advance", PluralLabel: "Employee Advances",
		Table: "employee_advance", LinesTable: "employee_advance_line", Series: "EA/{FY}/{#####}",
		Submittable: true, Approvable: true, Printable: true,
		OwnerField: "created_by", EmployeeField: "employee_id",
		CloseRight:    "cancel",
		SearchFields:  []string{"number", "purpose"}, // CD-317
		DefaultRights: defaultRights(),
		Fields:        advanceFields(),
		Hooks:         advanceHooks{},
	})
}
