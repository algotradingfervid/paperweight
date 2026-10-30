// Package taxcodeweb is the Tax Code master's screens (MST Task 22, CD-89):
// the list, new and record pages, disable, enable and delete under
// /m/tax-code, and the picker search at /pick/Tax Code. The screens themselves
// are mstweb's; this package gives them the tax code service, its copy and the
// four functions below.
package taxcodeweb

import (
	"errors"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/types"
)

// Routes mounts the tax code screens under /m/tax-code and the picker at
// /pick/Tax Code.
func Routes(svc *taxcode.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[taxcode.TaxCode, taxcode.Input]{
		DocType: taxcode.DocType,
		Base:    "/m/tax-code",
		Copy: mstweb.Copy{
			Plural: "Tax Codes", Noun: "tax codes", One: "tax code", New: "New tax code", Search: "Search tax codes", Empty: "No tax codes yet.",
			Saved: "Tax code saved.", Disabled: "Tax code disabled.", Enabled: "Tax code enabled.", Deleted: "Tax code deleted.",
		},
		Svc:      svc,
		Env:      env,
		ToInput:  toInput,
		Validate: taxcode.Validate,
		Values:   values,
		Head:     head,
		Ref:      ref,
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank rate is 0, because a code that isn't Taxable posts it empty; the
// service refuses it on a Taxable code. Any other rate NUM refuses is a parse
// error with NUM's message.
func toInput(v map[string]string) (taxcode.Input, []doctype.FieldError) {
	in := taxcode.Input{Code: v["code"], Name: v["name"], Treatment: taxcode.Treatment(v["treatment"])}
	rate, err := types.ParsePercent(v["rate"])
	if err != nil && !errors.Is(err, types.ErrEmpty) {
		return in, []doctype.FieldError{{Field: "rate", Message: err.Error()}}
	}
	in.Rate = rate
	return in, nil
}

// values is a stored tax code as the form's values: the rate plain, as
// docscreen takes it.
func values(c taxcode.TaxCode) map[string]string {
	return map[string]string{"code": c.Code, "name": c.Name, "treatment": string(c.Treatment), "rate": c.Rate.Plain()}
}

func head(c taxcode.TaxCode) mstweb.Head {
	return mstweb.Head{Code: c.Code, Name: c.Name, Disabled: c.Disabled, Version: c.Version}
}

// ref is the record AUTH checks, as taxcode.Service builds it.
func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: taxcode.DocType, ID: id} }
