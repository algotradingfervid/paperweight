// Package expensetypeweb is the Expense Type master's screens (MST Task 22,
// CD-89): the list, new and record pages, disable, enable and delete under
// /m/expense-type, and the picker search at /pick/Expense Type. The screens
// themselves are mstweb's; this package gives them the expense type service,
// its copy and the functions below.
package expensetypeweb

import (
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Routes mounts the expense type screens under /m/expense-type and the picker
// at /pick/Expense Type.
func Routes(svc *expensetype.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[expensetype.ExpenseType, expensetype.Input]{
		DocType: expensetype.DocType,
		Base:    "/m/expense-type",
		Copy: mstweb.Copy{
			Plural: "Expense Types", Noun: "expense types", One: "expense type", New: "New expense type",
			Search: "Search expense types", Empty: "No expense types yet.",
			Saved: "Expense type saved.", Disabled: "Expense type disabled.", Enabled: "Expense type enabled.", Deleted: "Expense type deleted.",
		},
		Svc:      svc,
		Env:      env,
		ToInput:  toInput,
		Validate: expensetype.Validate,
		Values:   values,
		Head:     head,
		Ref:      ref,
		Codes:    map[string]components.CodeKind{"default_hsn_sac": components.HSN},
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank tax code is none; an id that can't be parsed never reaches the
// service, so it gets the service's refusal of one that doesn't exist.
func toInput(v map[string]string) (expensetype.Input, []doctype.FieldError) {
	in := expensetype.Input{Code: v["code"], Name: v["name"], DefaultHSNSAC: v["default_hsn_sac"]}
	if s := v["default_tax_code_id"]; s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return in, []doctype.FieldError{{Field: "default_tax_code_id", Message: expensetype.MsgTaxNotFound}}
		}
		in.DefaultTaxCodeID = id
	}
	return in, nil
}

// values is a stored expense type as the form's values: the tax code the id
// in decimal.
func values(e expensetype.ExpenseType) map[string]string {
	return map[string]string{
		"code": e.Code, "name": e.Name, "default_hsn_sac": e.DefaultHSNSAC,
		"default_tax_code_id": strconv.FormatInt(e.DefaultTaxCodeID, 10),
	}
}

func head(e expensetype.ExpenseType) mstweb.Head {
	return mstweb.Head{Code: e.Code, Name: e.Name, Disabled: e.Disabled, Version: e.Version}
}

// ref is the record AUTH checks, as expensetype.Service builds it.
func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: expensetype.DocType, ID: id} }
