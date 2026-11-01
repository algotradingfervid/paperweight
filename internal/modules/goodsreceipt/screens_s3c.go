package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ReceiptLine is what a GRN line's card shows beyond its quantities (UX doc
// §5.5 P1): its PO line number and the line's own snapshots of the item,
// the unit and the PO line's net rate (spec §3.2).
type ReceiptLine struct {
	PONo               int
	ItemCode, ItemName string
	UoM                string // the unit's code
	Rate               types.Rate
}

// Lines are GRN id's ReceiptLines by line id, for p, who must be able to
// read it: a hidden or missing GRN is AUTH's 404 (CanTx read with the GRN's
// record, as Page and BillCard; CD-245).
func (s *Service) Lines(ctx context.Context, p auth.Principal, id int64) (map[int64]ReceiptLine, error) {
	out := map[int64]ReceiptLine{}
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		g, err := q.G7Receipt(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, grnRef(id, g.CreatedBy, g.ProjectID, g.SiteID)); err != nil {
			return err
		}
		rows, err := q.S3cLines(ctx, id)
		for _, r := range rows {
			out[r.ID] = ReceiptLine{PONo: int(r.PoLineNo), ItemCode: r.ItemCode, ItemName: r.ItemName, UoM: r.UomCode, Rate: types.Rate(r.Rate)}
		}
		return err
	})
	return out, err
}
