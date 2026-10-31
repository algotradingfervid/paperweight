package expense

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// checkLock refuses submit, approve and cancel of a claim or advance dated on
// or before the period lock (PO spec §14.2, CD-234), by its claim date or
// advance date. Close runs no hook, so it isn't checked.
func checkLock(ctx context.Context, tx *db.Tx, dt string, id int64) error {
	q := dbq.New(tx.SQL())
	var number, date sql.NullString
	if dt == ClaimDocType {
		h, err := q.ClaimHeader(ctx, id)
		if err != nil {
			return err
		}
		number, date = h.Number, h.ClaimDate
	} else {
		h, err := q.AdvanceHeader(ctx, id)
		if err != nil {
			return err
		}
		number, date = h.Number, h.AdvanceDate
	}
	d, err := dateOf(date)
	if err != nil {
		return err
	}
	t, _ := doctype.Lookup(dt)
	return modkit.CheckNotLocked(ctx, tx, t.Label, naming.Label(number.String, id), d)
}
