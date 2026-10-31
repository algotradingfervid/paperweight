package goodsreceipt

import (
	"context"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DraftFromPO is step 1's create values for PO poID (GRN spec §3.1, §11;
// plan G6 Do 2): purchase_order_id, site_id and project_id from the PO and
// receipt_date today, and one line per PO line with pending above 0
// (received = pending, rejected 0). A blank PO is refused first: a
// restricted create with no site_id would be the engine's 404. A PO p
// can't read is auth.CheckLinkTx's refusal as is (CD-240). No state check:
// the engine refuses a PO that isn't approved (GRN-037).
func (s *Service) DraftFromPO(ctx context.Context, p auth.Principal, poID int64) (map[string]string, []map[string]string, error) {
	if poID == 0 {
		return nil, nil, doc.Invalid([]doc.FieldError{{Field: "purchase_order_id", Message: msgChoosePO}})
	}
	var values map[string]string
	var lines []map[string]string
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := s.d.Auth.CheckLinkTx(ctx, tx, p, purchase.DocType, poID); err != nil {
			return err
		}
		q := dbq.New(tx.SQL())
		po, err := q.G6PO(ctx, poID)
		if err != nil {
			return err
		}
		poLines, err := q.G6POLines(ctx, poID)
		if err != nil {
			return err
		}
		values = map[string]string{
			"purchase_order_id": strconv.FormatInt(poID, 10),
			"site_id":           strconv.FormatInt(po.SiteID.Int64, 10),
			"project_id":        strconv.FormatInt(po.ProjectID.Int64, 10),
			"receipt_date":      types.Today(s.d.Clock).ISO(),
		}
		for _, l := range poLines {
			pending := purchase.Pending(types.Qty(l.Qty), types.Qty(l.Already), types.Qty(l.ReceivedQty))
			if pending > 0 {
				lines = append(lines, map[string]string{
					"po_line_id": strconv.FormatInt(l.ID, 10), "received_qty": pending.Plain(), "rejected_qty": "0",
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return values, lines, nil
}
