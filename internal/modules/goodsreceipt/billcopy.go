package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// copyBillOnAmend moves the bill details to an approved amendment (CD-272
// Gap 1, CD-279 (11), (12); plan G7 Do 7): when GRN id amends another, has
// no row, and the amended-from GRN has one, the row is copied to id at
// version 1 with an audit entry on id, by its updated_by, "Copied from {GR
// number}". The amended-from GRN keeps its row.
func copyBillOnAmend(ctx context.Context, tx *db.Tx, d Deps, id int64) error {
	q := dbq.New(tx.SQL())
	g, err := q.G7Receipt(ctx, id)
	if err != nil || !g.AmendedFromID.Valid {
		return err
	}
	if _, err := q.G7Bill(ctx, id); !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	now := d.Clock.Now()
	n, err := q.G7BillCopy(ctx, dbq.G7BillCopyParams{ToID: id, At: types.TimestampOf(now).String(), By: g.UpdatedBy, FromID: g.AmendedFromID.Int64})
	if err != nil || n == 0 {
		return err
	}
	from, err := q.G7Receipt(ctx, g.AmendedFromID.Int64)
	if err != nil {
		return err
	}
	return d.Audit.Append(ctx, tx, audit.Entry{
		DocType: DocType, DocID: id, Action: auditBillCopied, Reason: fmt.Sprintf(auditBillCopiedFrom, from.Number),
		By: types.UserID(g.UpdatedBy), At: now,
	})
}
