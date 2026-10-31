package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// RefreshOrderStatus recomputes ordered_qty and order_status of the given
// requests from lineOrdered over their PO lines as stored, with plain
// UPDATEs: no version, no updated_*, no audit (PO spec §20.3, CD-280 D3).
func RefreshOrderStatus(ctx context.Context, tx *db.Tx, requestIDs []int64) error {
	return refreshRequests(ctx, tx, requestIDs, 0, nil)
}

// applyOrder, reverseOrder and closeOrder refresh the requests the PO links
// on approve, cancel and close; the PO is already in its new state.
func applyOrder(ctx context.Context, tx *db.Tx, id int64) error   { return refreshLinked(ctx, tx, id) }
func reverseOrder(ctx context.Context, tx *db.Tx, id int64) error { return refreshLinked(ctx, tx, id) }
func closeOrder(ctx context.Context, tx *db.Tx, id int64) error   { return refreshLinked(ctx, tx, id) }

func refreshLinked(ctx context.Context, tx *db.Tx, poID int64) error {
	ids, err := dbq.New(tx.SQL()).OrderRequestIDs(ctx, poID)
	if err != nil {
		return err
	}
	return RefreshOrderStatus(ctx, tx, ids)
}

// refreshClosedRequests refreshes the requests the closed PO poID links,
// counting its lines as min(qty, already_received + accepted) from the map
// RefreshReceiptStatus was given, never the received_qty column: GRN's hooks
// run before the roll-up rewrites it (spec §20.3, CD-276).
func refreshClosedRequests(ctx context.Context, tx *db.Tx, poID int64, accepted map[int64]types.Qty) error {
	ids, err := dbq.New(tx.SQL()).OrderRequestIDs(ctx, poID)
	if err != nil {
		return err
	}
	return refreshRequests(ctx, tx, ids, poID, accepted)
}

// refreshRequests writes each request's ordered_qty per row and its
// order_status: not_ordered when nothing is ordered, ordered when every row
// reaches its qty, part_ordered otherwise. PO poID's lines count from
// accepted (linkedOrdered).
func refreshRequests(ctx context.Context, tx *db.Tx, requestIDs []int64, poID int64, accepted map[int64]types.Qty) error {
	q := dbq.New(tx.SQL())
	for _, id := range requestIDs {
		rows, err := q.RequestLinesForValidate(ctx, id)
		if err != nil {
			return err
		}
		links, err := q.RequestLinkedLines(ctx, id)
		if err != nil {
			return err
		}
		anything, full := false, true
		for _, r := range rows {
			var ordered types.Qty
			for _, l := range links {
				if l.RowID == r.ID {
					o, _ := linkedOrdered(l, poID, accepted)
					ordered += o
				}
			}
			if err := q.SetRequestLineOrdered(ctx, dbq.SetRequestLineOrderedParams{OrderedQty: int64(ordered), ID: r.ID}); err != nil {
				return err
			}
			anything = anything || ordered > 0
			full = full && ordered >= types.Qty(r.Qty)
		}
		status := "part_ordered"
		switch {
		case !anything:
			status = "not_ordered"
		case full:
			status = "ordered"
		}
		if err := q.SetRequestOrderStatus(ctx, dbq.SetRequestOrderStatusParams{OrderStatus: status, ID: id}); err != nil {
			return err
		}
	}
	return nil
}
