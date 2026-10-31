package expense_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// The DocType declarations of plan E1 Do 1 (spec §3, §4, CD-151, CD-241,
// CD-243, CD-255).
func TestExp_SchemaDocTypes(t *testing.T) {
	for _, tc := range []struct {
		name, label, plural, table, lines, series string
	}{
		{expense.ClaimDocType, "Expense Claim", "Expense Claims", "expense_claim", "expense_claim_line", "EX/{FY}/{#####}"},
		{expense.AdvanceDocType, "Employee Advance", "Employee Advances", "employee_advance", "employee_advance_line", "EA/{FY}/{#####}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dt := lookupDT(t, tc.name)
			if dt.Label != tc.label || dt.PluralLabel != tc.plural {
				t.Errorf("Label, PluralLabel = %q, %q; want %q, %q", dt.Label, dt.PluralLabel, tc.label, tc.plural)
			}
			if dt.Table != tc.table || dt.LinesTable != tc.lines {
				t.Errorf("Table, LinesTable = %q, %q; want %q, %q", dt.Table, dt.LinesTable, tc.table, tc.lines)
			}
			if dt.Series != tc.series {
				t.Errorf("Series = %q, want %q", dt.Series, tc.series)
			}
			if !dt.Submittable || !dt.Approvable || !dt.Printable {
				t.Errorf("Submittable, Approvable, Printable = %v, %v, %v; want all true", dt.Submittable, dt.Approvable, dt.Printable)
			}
			if dt.HasRoundedTotal {
				t.Error("HasRoundedTotal = true, want false (a reimbursement pays the exact amount)")
			}
			if dt.Attachable {
				t.Error("Attachable = true, want false (CD-241, CD-243)")
			}
			if dt.OwnerField != "created_by" || dt.EmployeeField != "employee_id" {
				t.Errorf("OwnerField, EmployeeField = %q, %q; want created_by, employee_id", dt.OwnerField, dt.EmployeeField)
			}
			if dt.CloseRight != string(auth.RightCancel) {
				t.Errorf("CloseRight = %q, want cancel", dt.CloseRight)
			}
			if len(dt.ProjectFields) != 0 || len(dt.SiteFields) != 0 {
				t.Errorf("ProjectFields, SiteFields = %q, %q; want none (claims are scoped by own and team)", dt.ProjectFields, dt.SiteFields)
			}
			if dt.Hooks == nil {
				t.Error("Hooks = nil, want the module's hooks")
			}

			emp, ok := dt.Field("employee_id")
			if !ok {
				t.Fatal("no employee_id field")
			}
			if emp.Type != doctype.Link || emp.OnLine || !emp.AllowBlocked {
				t.Errorf("employee_id: Type %v, OnLine %v, AllowBlocked %v; want a header Link with AllowBlocked", emp.Type, emp.OnLine, emp.AllowBlocked)
			}
		})
	}

	claim := lookupDT(t, expense.ClaimDocType)
	if want := []doctype.LinkSpec{{Field: "advance_id", Target: expense.AdvanceDocType}}; !slices.Equal(claim.Links, want) {
		t.Errorf("claim Links = %+v, want %+v", claim.Links, want)
	}
	if adv, ok := claim.Field("advance_id"); !ok || adv.Type != doctype.Link || adv.Link != expense.AdvanceDocType || adv.OnLine {
		t.Errorf("claim advance_id = %+v (found %v), want a header Link to %s", adv, ok, expense.AdvanceDocType)
	}

	advance := lookupDT(t, expense.AdvanceDocType)
	if len(advance.Links) != 0 {
		t.Errorf("advance Links = %+v, want none", advance.Links)
	}
	la, ok := advance.Field("line_amount")
	if !ok {
		t.Fatal("advance has no line_amount field (CD-151: the line amount clashes with the header amount)")
	}
	if la.Column != "amount" || !la.OnLine || la.Type != doctype.Money {
		t.Errorf("advance line_amount: Column %q, OnLine %v, Type %v; want Column amount, a line Money field", la.Column, la.OnLine, la.Type)
	}
	if amt, ok := advance.Field("amount"); !ok || amt.OnLine || amt.Type != doctype.Money || !amt.ReadOnly {
		t.Errorf("advance amount = %+v (found %v), want the read-only header Money field", amt, ok)
	}
}
