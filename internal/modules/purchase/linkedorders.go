package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// LinkedOrder is one PO linking a request (MOD-PO P6 Do 7).
type LinkedOrder struct {
	ID            int64
	Display       string
	VendorName    string
	State         doctype.State
	ReceiptStatus string
}

// LinkedOrders is every non-cancelled PO linking the request, oldest first.
// Callers filter it through LinkRead. Stub until P6.
func LinkedOrders(ctx context.Context, tx *db.Tx, requestID int64) ([]LinkedOrder, error) {
	return nil, errNotImplementedP6
}
