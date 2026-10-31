package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// G11's stub (duplicate challan warning, CD-337 (6)). G11 deletes this file
// and adds DuplicateChallan to warnings.go.

// DuplicateChallan: no warning until G11 builds it.
func DuplicateChallan(context.Context, *db.Tx, auth.Principal, int64) (string, error) {
	return "", nil
}
