package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// G7's stubs (bill details). G7 deletes only its own functions from this
// file and adds its own files.

// copyBillOnAmend: nil until G7 moves the bill details to an approved
// amendment (CD-272 Gap 1).
func copyBillOnAmend(context.Context, *db.Tx, Deps, int64) error { return nil }
