package purchase

import (
	"context"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// UoM and Tax code resolve themselves from the item when blank, so they
// aren't Required (CD-387 (2)) and DOC no longer checks them. A value Validate
// couldn't resolve is refused here at Submit, on its own field, with DOC's
// text. A draft saves without them, so Validate can't refuse them. Project
// stays Required: DOC's restricted-user check (CD-178, PO-012) reads the flag.

// Labels in the refusals, as the fields declare them.
const (
	orderUoMLabel   = "UoM"
	requestUoMLabel = "Unit"
	taxCodeLabel    = "Tax code"
	msgLineRequired = "Line %d: %s is required."
)

// blankLine is the error of a blank link on line, or none.
func blankLine(id, line int64, field, label string) []doctype.FieldError {
	if id != 0 {
		return nil
	}
	return []doctype.FieldError{{Field: field, Line: int(line), Message: fmt.Sprintf(msgLineRequired, line, label)}}
}

// orderBlanks are what the PO's stored lines leave unresolved.
func orderBlanks(rows []dbq.OrderLinesForValidateRow) []doctype.FieldError {
	var errs []doctype.FieldError
	for _, l := range rows {
		errs = append(errs, blankLine(l.UomID, l.LineNo, "uom_id", orderUoMLabel)...)
		errs = append(errs, blankLine(l.TaxCodeID, l.LineNo, "tax_code_id", taxCodeLabel)...)
	}
	return errs
}

// requestBlanks are what the request's stored lines leave unresolved.
func requestBlanks(rows []dbq.RequestLinesForValidateRow) []doctype.FieldError {
	var errs []doctype.FieldError
	for _, l := range rows {
		errs = append(errs, blankLine(l.UomID, l.LineNo, "uom_id", requestUoMLabel)...)
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
	return orderBlanks(rows), nil
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
