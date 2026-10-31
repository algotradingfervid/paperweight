package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
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
// Callers filter it through LinkRead.
func LinkedOrders(ctx context.Context, tx *db.Tx, requestID int64) ([]LinkedOrder, error) {
	rows, err := dbq.New(tx.SQL()).RequestLinkedOrders(ctx, requestID)
	if err != nil {
		return nil, err
	}
	var out []LinkedOrder
	for _, r := range rows {
		out = append(out, LinkedOrder{ID: r.ID, Display: naming.Label(r.Number, r.ID), VendorName: r.SnapVendorName,
			State: doctype.State(r.State), ReceiptStatus: r.ReceiptStatus})
	}
	return out, nil
}
