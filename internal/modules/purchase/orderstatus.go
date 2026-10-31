package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// RefreshOrderStatus recomputes ordered_qty and order_status of the given
// requests from lineOrdered, with plain UPDATEs (PO spec §20.3, CD-280 D3).
// Stub until P6.
func RefreshOrderStatus(ctx context.Context, tx *db.Tx, requestIDs []int64) error {
	return errNotImplementedP6
}
