package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// G3: effects and receipt status (GRN spec §5). The PO's received_qty is the
// kernel roll-up's, after ApplyEffects; GRN only refreshes receipt_status.

// applyReceipt refreshes the PO's receipt status on approve.
func applyReceipt(ctx context.Context, tx *db.Tx, _ Deps, id int64) error {
	return refreshPO(ctx, tx, id)
}

// reverseReceipt refreshes the PO's receipt status on cancel.
func reverseReceipt(ctx context.Context, tx *db.Tx, _ Deps, id int64) error {
	return refreshPO(ctx, tx, id)
}

// refreshPO hands the accepted totals of the GRN's PO to
// purchase.RefreshReceiptStatus. The kernel has already set this GRN's
// state, so the totals are right on approve and on cancel.
func refreshPO(ctx context.Context, tx *db.Tx, id int64) error {
	po, err := dbq.New(tx.SQL()).G3ReceiptPO(ctx, id)
	if err != nil {
		return err
	}
	accepted, err := acceptedByLine(ctx, tx, po.Int64)
	if err != nil {
		return err
	}
	return purchase.RefreshReceiptStatus(ctx, tx, po.Int64, accepted)
}

// acceptedByLine is, for every line of the PO, the accepted quantity summed
// over GRN lines whose GRN is approved or closed (0 when none).
func acceptedByLine(ctx context.Context, tx *db.Tx, poID int64) (map[int64]types.Qty, error) {
	rows, err := dbq.New(tx.SQL()).G3AcceptedByLine(ctx, poID)
	if err != nil {
		return nil, err
	}
	accepted := make(map[int64]types.Qty, len(rows))
	for _, r := range rows {
		accepted[r.ID] = types.Qty(r.Accepted)
	}
	return accepted, nil
}
