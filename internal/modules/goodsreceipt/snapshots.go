package goodsreceipt

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/types"
)

// writeCopies writes the header copies from the stored purchase_order_id
// (vendor, site, project and the header snapshots; cleared when it is blank),
// defaults a blank receipt date to today, and writes each line's unit,
// snapshots and accepted quantity from its own po_line_id (GRN spec §3.1,
// §3.2). Every save writes them from the current inputs, never leaving an
// old value beside a changed one (CD-302), and amend gets them from the PO,
// not from the cancelled GRN.
func writeCopies(ctx context.Context, q *dbq.Queries, id int64, today types.Date) error {
	if err := q.G2SnapReceipt(ctx, dbq.G2SnapReceiptParams{Today: sql.NullString{String: today.ISO(), Valid: true}, ID: id}); err != nil {
		return err
	}
	return q.G2SnapLines(ctx, id)
}
