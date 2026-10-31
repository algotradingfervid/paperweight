package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// POLine is one line of a GRN's PO as the Received quantities step shows
// it (UX doc §5.4).
type POLine struct {
	ID               int64 // purchase_order_line.id
	No               int   // the PO line number
	ItemCode         string
	ItemName         string
	UoM, StockUoM    uom.UoM // Code and WholeNumber only, for uom.CheckLineQty
	Factor           types.Factor
	Ordered, Pending types.Qty // pending before this GRN (spec §4)
}

// POLines are every line of GRN id's PO, in PO line order, with pending
// as Warnings and LineWarnings count it (G5POLines, purchase.Pending): GRN
// id's own accepted not counted (S2). The caller has read on GRN id.
func POLines(ctx context.Context, tx *db.Tx, id int64) ([]POLine, error) {
	q := dbq.New(tx.SQL())
	rows, err := q.S2POLines(ctx, id)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	qty, err := q.G5POLines(ctx, dbq.G5POLinesParams{GrnID: id, PoID: rows[0].PoID})
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]dbq.G5POLinesRow, len(qty))
	for _, r := range qty {
		byID[r.ID] = r
	}
	out := make([]POLine, 0, len(rows))
	for _, r := range rows {
		p := byID[r.ID]
		out = append(out, POLine{
			ID: r.ID, No: int(r.LineNo), ItemCode: r.SnapItemCode, ItemName: r.SnapItemName,
			UoM:      uom.UoM{Code: r.SnapUomCode, WholeNumber: r.LineWhole != 0},
			StockUoM: uom.UoM{Code: r.StockUomCode, WholeNumber: r.StockWhole != 0},
			Factor:   types.Factor(r.Factor), Ordered: types.Qty(p.Qty),
			Pending: purchase.Pending(types.Qty(p.Qty), types.Qty(p.AlreadyReceived), types.Qty(p.OtherAccepted)),
		})
	}
	return out, nil
}
