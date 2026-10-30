// Package uomweb is the UoM master's screens (MST Task 22, CD-89): the list,
// new and record pages, disable, enable and delete under /m/uom, and the
// picker search at /pick/UoM. The screens themselves are mstweb's;
// this package gives them the UoM service, its copy and the four functions
// below. It is the pattern the other single-form masters copy.
package uomweb

import (
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// Routes mounts the UoM screens under /m/uom and the picker at /pick/UoM.
func Routes(svc *uom.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[uom.UoM, uom.Input]{
		DocType: uom.DocType,
		Base:    "/m/uom",
		Copy: mstweb.Copy{
			Plural: "UoMs", Noun: "UoMs", One: "UoM", New: "New UoM", Search: "Search UoMs", Empty: "No UoMs yet.",
			Saved: "UoM saved.", Disabled: "UoM disabled.", Enabled: "UoM enabled.", Deleted: "UoM deleted.",
		},
		Svc:     svc,
		Env:     env,
		ToInput: toInput,
		Values:  values,
		Head:    head,
		Ref:     ref,
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input.
// A UoM has only text and a checkbox, so nothing can fail to parse.
func toInput(v map[string]string) (uom.Input, []doctype.FieldError) {
	return uom.Input{Code: v["code"], Name: v["name"], UQC: v["uqc"], WholeNumber: v["whole_number"] != ""}, nil
}

// values is a stored UoM as the form's values: Check fields "1" or "0".
func values(u uom.UoM) map[string]string {
	return map[string]string{"code": u.Code, "name": u.Name, "uqc": u.UQC, "whole_number": check(u.WholeNumber)}
}

func head(u uom.UoM) mstweb.Head {
	return mstweb.Head{Code: u.Code, Name: u.Name, Disabled: u.Disabled, Version: u.Version}
}

// ref is the record AUTH checks, as uom.Service builds it.
func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: uom.DocType, ID: id} }

func check(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
