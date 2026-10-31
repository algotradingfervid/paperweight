package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// G5's stubs (warnings, notes, period lock). G5 deletes this file and adds
// its own.

// checkLockFields is the lock at submit as a receipt_date field error
// (CD-282 Q9); none until G5.
func checkLockFields(context.Context, *db.Tx, Deps, int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// checkLock is the lock's refusal of a cancel (DOC-077); nil until G5.
func checkLock(context.Context, *db.Tx, Deps, int64) error { return nil }

// Warnings are GRN id's over-delivery and receipt-date warnings (GRN spec
// §4); none until G5.
func Warnings(context.Context, *db.Tx, int64) ([]string, error) { return nil, nil }

// Notes are GRN id's information lines for the check and document pages
// (CD-272 Gaps 4 and 13); none until G5.
func Notes(context.Context, *db.Tx, int64) ([]string, error) { return nil, nil }

// LineWarnings are the over-delivery warnings for typed accepted quantities
// on PO poID's lines, keyed by PO line id, with draft grnID's own accepted
// not counted (S2); none until G5.
func LineWarnings(context.Context, *db.Tx, int64, int64, map[int64]types.Qty) (map[int64]string, error) {
	return nil, nil
}
