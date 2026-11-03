package purchase

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// UoM and Tax code resolve themselves from the item when blank, so they
// aren't Required (CD-387 (2)) and DOC no longer checks them. A value Validate
// couldn't resolve is refused here at Submit, on its own field, with DOC's
// text. A draft saves without them, so Validate can't refuse them. Project
// stays Required: DOC's restricted-user check (CD-178, PO-012) reads the flag.

// blankLine is the error of a blank link on line, or none, labelled as dt
// declares field.
func blankLine(dt *doctype.DocType, id, line int64, field string) []doctype.FieldError {
	if id != 0 {
		return nil
	}
	f, _ := dt.Field(field)
	return []doctype.FieldError{doc.RequiredError(f, int(line))}
}

// orderBlanks are what the PO's stored lines leave unresolved.
func orderBlanks(dt *doctype.DocType, rows []dbq.OrderLinesForValidateRow) []doctype.FieldError {
	var errs []doctype.FieldError
	for _, l := range rows {
		errs = append(errs, blankLine(dt, l.UomID, l.LineNo, "uom_id")...)
		errs = append(errs, blankLine(dt, l.TaxCodeID, l.LineNo, "tax_code_id")...)
	}
	return errs
}

// requestBlanks are what the request's stored lines leave unresolved.
func requestBlanks(dt *doctype.DocType, rows []dbq.RequestLinesForValidateRow) []doctype.FieldError {
	var errs []doctype.FieldError
	for _, l := range rows {
		errs = append(errs, blankLine(dt, l.UomID, l.LineNo, "uom_id")...)
	}
	return errs
}

// MissingOrder is what Submit will refuse on PO id as unresolved, and the
// vendor's refusals from checkVendor, the rule Validate runs on Submit (a
// supplier with no state, RR1c), for its check page. A read failure inside
// checkVendor panics, as in Validate.
func MissingOrder(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	h, err := q.OrderForValidate(ctx, id)
	if err != nil {
		return nil, err
	}
	rows, err := q.OrderLinesForValidate(ctx, id)
	if err != nil {
		return nil, err
	}
	_, _, errs := checkVendor(ctx, tx, h, &dbq.SetOrderVendorParams{})
	dt, _ := doc.Lookup(DocType)
	return append(errs, orderBlanks(dt, rows)...), nil
}

// MissingRequest is what Submit will refuse as unresolved on request id,
// for its check page.
func MissingRequest(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	rows, err := dbq.New(tx.SQL()).RequestLinesForValidate(ctx, id)
	if err != nil {
		return nil, err
	}
	dt, _ := doc.Lookup(RequestDocType)
	return requestBlanks(dt, rows), nil
}

// RejectedReason is the reason of the rejection PO id's latest submit ended
// in, "" when that submit wasn't rejected or there was none. Revise clears
// state_reason, so the audit trail holds it. The caller has checked the read
// right.
func RejectedReason(ctx context.Context, tx *db.Tx, id int64) (string, error) {
	entries, err := audit.ForDoc(ctx, tx, DocType, id)
	if err != nil {
		return "", err
	}
	for _, e := range slices.Backward(entries) {
		switch e.Action {
		case string(doctype.ActionReject):
			return e.Reason, nil
		case string(doctype.ActionSubmit):
			return "", nil
		}
	}
	return "", nil
}
