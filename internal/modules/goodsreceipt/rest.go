package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// restNotComing passes a ticked GRN's rest_not_coming to its PO (GRN spec
// §3.1, CD-276): on true from ApplyEffects, false from ReverseEffects, as the
// GRN's updated_by. A GRN without the tick calls nothing. Purchase owns the
// comment, the row and the To close queue.
func restNotComing(ctx context.Context, tx *db.Tx, _ Deps, id int64, on bool) error {
	r, err := dbq.New(tx.SQL()).G10Rest(ctx, id)
	if err != nil {
		return err
	}
	if r.RestNotComing == 0 {
		return nil
	}
	return purchase.SetRestNotComing(ctx, tx, r.PurchaseOrderID, id, types.UserID(r.UpdatedBy), on)
}
