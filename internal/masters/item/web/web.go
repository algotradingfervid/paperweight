// Package itemweb is the Item master's screens (MST Task 22, CD-89): the list,
// new and record pages, disable, enable and delete under /m/item, and the
// picker search at /pick/Item. The screens themselves are mstweb's; this
// package gives them the item service, its copy and the functions below,
// with the conversion table as the form's rows.
package itemweb

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// factor6 is the validator kind of Item's factor: 6 decimals (MST-085), where
// the shared factor kind allows 9 (CD-203).
const factor6 components.NumberKind = "factor6"

// Routes mounts the item screens under /m/item and the picker at /pick/Item.
func Routes(svc *item.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[record, item.Input]{
		DocType: item.DocType,
		Base:    "/m/item",
		Copy: mstweb.Copy{
			Plural: "Items", Noun: "items", One: "item", New: "New item", Search: "Search items", Empty: "No items yet.",
			Saved: "Item saved.", Disabled: "Item disabled.", Enabled: "Item enabled.", Deleted: "Item deleted.",
			Rows: "Unit conversions", AddRow: "Add conversion",
		},
		Svc:        service{svc},
		Env:        env,
		ToInput:    toInput,
		Validate:   item.Validate,
		Values:     values,
		Head:       head,
		Ref:        ref,
		Codes:      map[string]components.CodeKind{"hsn_sac": components.HSN},
		Numbers:    map[string]components.NumberKind{"factor": factor6},
		ZeroIsNone: map[string]bool{"standard_rate": true},
		WideLines:  map[string]bool{"factor": true},
	}.Routes()
}

// Parsers returns the factor6 parser for components.ValidateHandler; WP3
// merges it with NUMParsers and gstinweb.Parsers (CD-212). The message past 6
// decimals is uom's.
func Parsers() map[string]components.ParseFunc {
	return map[string]components.ParseFunc{
		string(factor6): func(in string) (string, string, error) {
			f, err := uom.ParseFactor(in)
			if err != nil {
				return "", "", err
			}
			return types.FormatFactor(f), "", nil
		},
	}
}

// record is an item with its conversion table.
type record struct {
	item.Item
	Conversions []item.Conversion
}

// service is the item service with Get reading the conversion table, which
// item.Service.Get leaves out, in the same tx. Save replaces the whole table,
// so a form without its rows would wipe it.
type service struct{ *item.Service }

func (s service) Get(ctx context.Context, tx *db.Tx, id int64) (record, error) {
	it, err := s.Service.Get(ctx, tx, id)
	if err != nil {
		return record{}, err
	}
	rows, err := s.Conversions(ctx, tx, id)
	if err != nil {
		return record{}, err
	}
	return record{Item: it, Conversions: rows}, nil
}

// toInput is the form's values (keyed by Field.Name) as the service input,
// every posted row included. A blank link or rate is none, and a blank factor
// is 0, which the service refuses on its row. A link that can't be parsed gets
// the service's refusal of one that doesn't exist; a number that can't, NUM's
// message.
func toInput(v map[string]string) (item.Input, []doctype.FieldError) {
	in := item.Input{
		Code: v["code"], Name: v["name"], Type: item.ItemType(v["item_type"]), HSNSAC: v["hsn_sac"],
		IsStock: v["is_stock"] != "",
	}
	var errs []doctype.FieldError
	link := func(field, msg string) int64 {
		id, ok := linkID(v[field])
		if !ok {
			errs = append(errs, doctype.FieldError{Field: field, Message: msg})
		}
		return id
	}
	in.StockUoMID = link("stock_uom_id", item.MsgStockNotFound)
	in.PurchaseUoMID = link("purchase_uom_id", item.MsgPurchaseNotFound)
	in.DefaultTaxCodeID = link("default_tax_code_id", item.MsgTaxNotFound)
	rate, err := types.ParseRate(v["standard_rate"])
	if err != nil && !errors.Is(err, types.ErrEmpty) {
		errs = append(errs, doctype.FieldError{Field: "standard_rate", Message: err.Error()})
	}
	in.StandardRate = rate
	for n := 1; ; n++ {
		uomText, ok := v[rowKey(n, "uom_id")]
		if !ok {
			return in, errs
		}
		id, ok := linkID(uomText)
		if !ok {
			errs = append(errs, doctype.FieldError{Field: "uom_id", Line: n, Message: fmt.Sprintf(item.MsgRowNotFound, n)})
		}
		factor, err := uom.ParseFactor(v[rowKey(n, "factor")])
		if err != nil && !errors.Is(err, types.ErrEmpty) {
			errs = append(errs, doctype.FieldError{Field: "factor", Line: n, Message: fmt.Sprintf(item.MsgRowPrefix, n) + err.Error()})
		}
		in.Conversions = append(in.Conversions, item.Conversion{UoMID: id, Factor: factor})
	}
}

// linkID is a Link field's posted id: blank is 0, and false means it can't be
// parsed.
func linkID(s string) (int64, bool) {
	if s == "" {
		return 0, true
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

func rowKey(n int, field string) string {
	return doctype.FieldError{Field: field, Line: n}.Path()
}

// values is a stored item as the form's values: links the id in decimal, a
// zero rate empty because it means none (CD-37), numbers plain as docscreen
// takes them.
func values(r record) map[string]string {
	rate := ""
	if r.StandardRate != 0 {
		rate = r.StandardRate.Plain()
	}
	out := map[string]string{
		"code": r.Code, "name": r.Name, "item_type": string(r.Type), "hsn_sac": r.HSNSAC,
		"stock_uom_id": id(r.StockUoMID), "purchase_uom_id": id(r.PurchaseUoMID),
		"default_tax_code_id": id(r.DefaultTaxCodeID), "is_stock": check(r.IsStock), "standard_rate": rate,
	}
	for i, c := range r.Conversions {
		out[rowKey(i+1, "uom_id")] = id(c.UoMID)
		out[rowKey(i+1, "factor")] = c.Factor.Plain()
	}
	return out
}

func head(r record) mstweb.Head {
	return mstweb.Head{Code: r.Code, Name: r.Name, Disabled: r.Disabled, Version: r.Version}
}

// ref is the record AUTH checks, as item.Service builds it.
func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: item.DocType, ID: id} }

func id(v int64) string { return strconv.FormatInt(v, 10) }

func check(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
