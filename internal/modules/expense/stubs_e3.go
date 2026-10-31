package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E3 (Receipts) replaces this file.

// checkReceipts refuses a line whose receipt isn't among the claim's
// attachments (Validate). The stub adds no rule.
func checkReceipts(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// submitReceipts refuses a line with neither a receipt nor a reason (submit).
// The stub adds no rule.
func submitReceipts(_ context.Context, _ *db.Tx, _ Deps, _ int64) ([]doctype.FieldError, error) {
	return nil, nil
}

// receiptLine is the 1-based line of claim parentID holding the receipt of
// attachmentID, or 0 when no line does (RemoveGuard). The stub finds none.
func receiptLine(_ context.Context, _ *db.Tx, _, _ int64) (int, error) {
	return 0, nil
}
