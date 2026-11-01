package goodsreceipt

import (
	"context"

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
// read it.
func (s *Service) Lines(ctx context.Context, p auth.Principal, id int64) (map[int64]ReceiptLine, error) {
	out := map[int64]ReceiptLine{}
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := s.d.Auth.CheckLinkTx(ctx, tx, p, DocType, id); err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).S3cLines(ctx, id)
		for _, r := range rows {
			out[r.ID] = ReceiptLine{PONo: int(r.PoLineNo), ItemCode: r.ItemCode, ItemName: r.ItemName, UoM: r.UomCode, Rate: types.Rate(r.Rate)}
		}
		return err
	})
	return out, err
}
