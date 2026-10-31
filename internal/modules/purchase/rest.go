package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// SetRestNotComing records that the vendor won't send the rest of PO poID, as
// GRN grnID's tick said (spec §27.2). MOD-GRN calls it from its hooks inside
// the engine's tx, with no principal, and by as the GRN's updated_by.
//
// on=true on an approved PO inserts the (po, grn) row and posts the PO comment
// as by; an existing row means no insert and no comment. A PO in any other
// state gets nothing and nil is returned, because GRN refuses such receipts
// itself. on=false deletes the row in any state; a missing row is a no-op and
// the comment stays. An unknown PO id is a plain internal error. The PO's
// state, version and lines are never touched. Frozen for MOD-GRN (CD-276).
func SetRestNotComing(ctx context.Context, tx *db.Tx, poID, grnID int64, by types.UserID, on bool) error {
	deps, err := wiring.Get(tx)
	if err != nil {
		return err
	}
	q := dbq.New(tx.SQL())
	state, err := q.OrderState(ctx, poID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("purchase: set rest not coming: PO %d does not exist", poID)
	}
	if err != nil {
		return err
	}
	if !on {
		return q.DeleteRest(ctx, dbq.DeleteRestParams{PoID: poID, GrnID: grnID})
	}
	if state != string(doctype.StateApproved) {
		return nil
	}
	n, err := q.InsertRest(ctx, dbq.InsertRestParams{
		PoID: poID, GrnID: grnID, CreatedBy: int64(by),
		CreatedAt: types.TimestampOf(deps.Clock.Now()).String(),
	})
	if err != nil || n == 0 {
		return err
	}
	_, err = deps.Comments.AddTx(ctx, tx, by, DocType, poID, msgRestNotComing)
	return err
}
