package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// G5's stubs (period lock). G5 deletes this file and adds its own.

// checkLockFields is the lock at submit as a receipt_date field error
// (CD-282 Q9); none until G5.
func checkLockFields(context.Context, *db.Tx, Deps, int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// checkLock is the lock's refusal of a cancel (DOC-077); nil until G5.
func checkLock(context.Context, *db.Tx, Deps, int64) error { return nil }
