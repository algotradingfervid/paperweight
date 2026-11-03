package purchase

import (
	"context"
	"fmt"
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

// msgLineRequired is DOC's required text on a line: msgLinePrefix and
// msgRequired in internal/kernel/doc/messages.go, which DOC doesn't export.
const msgLineRequired = "Line %d: %s is required."

// blankLine is the error of a blank link on line, or none, labelled as dt
// declares field.
func blankLine(dt *doctype.DocType, id, line int64, field string) []doctype.FieldError {
	if id != 0 {
		return nil
	}
	f, _ := dt.Field(field)
	return []doctype.FieldError{{Field: field, Line: int(line), Message: fmt.Sprintf(msgLineRequired, line, f.Label)}}
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

// MissingOrder is what Submit will refuse as unresolved on PO id, for its
// check page.
func MissingOrder(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	rows, err := dbq.New(tx.SQL()).OrderLinesForValidate(ctx, id)
	if err != nil {
		return nil, err
	}
	dt, _ := doc.Lookup(DocType)
	return orderBlanks(dt, rows), nil
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
