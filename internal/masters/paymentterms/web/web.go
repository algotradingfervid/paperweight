// Package paymenttermsweb is the Payment Terms master's screens (MST Task 22,
// CD-89): the list, new and record pages, disable, enable and delete under
// /m/payment-terms, and the picker search at /pick/Payment Terms. The screens
// themselves are mstweb's; this package gives them the service, its copy and
// the four functions below. The rows post as lines[N].portion and
// lines[N].days (CD-201).
package paymenttermsweb

import (
	"errors"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
)

// Routes mounts the screens under /m/payment-terms and the picker at
// /pick/Payment Terms.
func Routes(svc *paymentterms.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[paymentterms.Terms, paymentterms.Input]{
		DocType: paymentterms.DocType,
		Base:    "/m/payment-terms",
		Copy: mstweb.Copy{
			Plural: "Payment Terms", Noun: "payment terms", One: "payment term", New: "New payment term", Search: "Search payment terms", Empty: "No payment terms yet.",
			Saved: "Payment term saved.", Disabled: "Payment term disabled.", Enabled: "Payment term enabled.", Deleted: "Payment term deleted.",
			Rows: "Payment portions", AddRow: "Add portion",
		},
		Svc:      svc,
		Env:      env,
		ToInput:  toInput,
		Validate: paymentterms.Validate,
		Values:   values,
		Head:     head,
		Ref:      ref,
		// Terms need a row to total 100%, so the new form starts with one.
		StartRows: 1,
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// row that doesn't parse is ParseRows' error on its own row and field; the
// service, which replaces the rows, is then not called.
func toInput(v map[string]string) (paymentterms.Input, []doctype.FieldError) {
	in := paymentterms.Input{Code: v["code"], Name: v["name"]}
	var raw []paymentterms.RawRow
	for n := 1; ; n++ {
		portion, ok := v[rowKey(n, "portion")]
		if !ok {
			break
		}
		raw = append(raw, paymentterms.RawRow{Portion: portion, Days: v[rowKey(n, "days")]})
	}
	rows, err := paymentterms.ParseRows(raw)
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) {
		return in, ve.Fields
	}
	in.Rows = rows
	return in, nil
}

// values is stored terms as the form's values: each portion plain, as
// docscreen takes it, so saving the form untouched keeps the rows.
func values(t paymentterms.Terms) map[string]string {
	vals := map[string]string{"code": t.Code, "name": t.Name}
	for i, r := range t.Rows {
		vals[rowKey(i+1, "portion")] = r.Portion.Plain()
		vals[rowKey(i+1, "days")] = strconv.Itoa(r.Days)
	}
	return vals
}

func head(t paymentterms.Terms) mstweb.Head {
	return mstweb.Head{Code: t.Code, Name: t.Name, Disabled: t.Disabled, Version: t.Version}
}

// ref is the record AUTH checks, as paymentterms.Service builds it.
func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: paymentterms.DocType, ID: id} }

func rowKey(n int, field string) string {
	return doctype.FieldError{Field: field, Line: n}.Path()
}
