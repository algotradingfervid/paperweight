package goodsreceipt

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// pickerPageSize is the number of rows on one page of the PO picker.
const pickerPageSize = 25

// POChoice is one row of step 1's PO picker (UX doc §5.2).
type POChoice struct {
	ID         int64  // purchase_order.id
	Display    string // naming.Label of the PO, e.g. PO/26-27/00001
	VendorName string // the PO's snap_vendor_name
	SiteName   string
	Pending    int // lines with pending above 0
	Lines      int // all lines ("{p} of {n} lines pending")
}

// POChoices is page (1-based, below 1 is 1) of the approved POs p may read
// (Auth.Scope, AUTH-110; never a link check, CD-312) with pending above 0 on
// at least one line, or every one when all, matching q (blank is no filter),
// ordered by vendor name, then PO number. Scope is asked before the read
// opens: it has no Tx twin.
func (s *Service) POChoices(ctx context.Context, p auth.Principal, all bool, q string, page int) ([]POChoice, error) {
	pred, err := s.d.Auth.Scope(ctx, p, purchase.DocType, auth.RightRead)
	if err != nil {
		return nil, err
	}
	var out []POChoice
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		readable, err := purchase.ReadableOrders(ctx, tx, pred)
		if err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).G6PickerLines(ctx, q)
		if err != nil {
			return err
		}
		out = choices(rows, readable, all)
		return nil
	})
	if err != nil {
		return nil, err
	}
	from := (max(page, 1) - 1) * pickerPageSize
	if from >= len(out) {
		return nil, nil
	}
	return out[from:min(from+pickerPageSize, len(out))], nil
}

// choices groups rows (ordered, each PO's lines together) into one choice
// per PO in readable, keeping those with pending unless all.
func choices(rows []dbq.G6PickerLinesRow, readable func(int64) bool, all bool) []POChoice {
	var out []POChoice
	for i := 0; i < len(rows); {
		r := rows[i]
		c := POChoice{ID: r.ID, Display: naming.Label(r.Number, r.ID), VendorName: r.SnapVendorName, SiteName: r.SiteName}
		for ; i < len(rows) && rows[i].ID == r.ID; i++ {
			c.Lines++
			if purchase.Pending(types.Qty(rows[i].Qty), types.Qty(rows[i].Already), types.Qty(rows[i].ReceivedQty)) > 0 {
				c.Pending++
			}
		}
		if readable(c.ID) && (all || c.Pending > 0) {
			out = append(out, c)
		}
	}
	return out
}
