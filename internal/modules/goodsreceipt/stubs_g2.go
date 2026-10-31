package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// G2's stubs (Validate, snapshots, submit rules). G2 deletes this file and
// adds its own.

// validateReceipt: no field errors and nothing copied until G2.
func validateReceipt(context.Context, *db.Tx, Deps, int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// submitFields: no submit-only rule until G2.
func submitFields(context.Context, *db.Tx, Deps, int64) ([]doctype.FieldError, error) {
	return nil, nil
}
