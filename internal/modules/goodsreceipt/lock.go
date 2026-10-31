package goodsreceipt

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// checkLock refuses a GRN dated on or before the period lock by its receipt
// date (PO spec §14.2, CD-234). In ReverseEffects the refusal rolls the
// cancel back (DOC-077).
func checkLock(ctx context.Context, tx *db.Tx, _ Deps, id int64) error {
	h, err := dbq.New(tx.SQL()).G5Header(ctx, id)
	if err != nil {
		return err
	}
	date, err := types.DateFromISO(h.ReceiptDate.String)
	if err != nil {
		return err
	}
	return modkit.CheckNotLocked(ctx, tx, "Goods Receipt", naming.Label(h.Number.String, id), date)
}

// checkLockFields is the lock at submit as a receipt_date field error with
// the same text, so the check page links it to step 1's date (CD-282 Q9).
func checkLockFields(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	err := checkLock(ctx, tx, d, id)
	var refused *doc.Error
	if errors.As(err, &refused) {
		return []doctype.FieldError{{Field: "receipt_date", Message: refused.Message}}, nil
	}
	return nil, err
}
