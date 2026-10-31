package expense

import (
	"context"
	"database/sql"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// actionEffect is the audit action of a change one document makes on
// another, as the engine's roll-ups write it (doc/rollup.go).
const actionEffect = "effect"

// applyClaim recomputes the named advance's used_amount when the claim is
// approved (ApplyEffects).
func applyClaim(ctx context.Context, tx *db.Tx, d Deps, id int64) error {
	return recomputeUsed(ctx, tx, d, id, "approve")
}

// reverseClaim recomputes the named advance's used_amount when the claim is
// cancelled (ReverseEffects).
func reverseClaim(ctx context.Context, tx *db.Tx, d Deps, id int64) error {
	return recomputeUsed(ctx, tx, d, id, "cancel")
}

// recomputeUsed sets the used_amount of the advance claim id names to the
// sum over its approved and closed claims (recompute, never increment;
// D-DOC-13), bumps the advance's version and appends an "effect" audit entry
// caused by the claim's action cause. A hook has no principal, so the change
// is by the claim's updated_by, who is acting now. As doc/rollup.go does.
func recomputeUsed(ctx context.Context, tx *db.Tx, d Deps, id int64, cause string) error {
	q := dbq.New(tx.SQL())
	h, a, ok, err := claimAdvance(ctx, q, id)
	if err != nil || !ok {
		return err
	}
	used, err := q.E4Used(ctx, sql.NullInt64{Int64: a.ID, Valid: true})
	if err != nil {
		return err
	}
	now := d.Clock.Now()
	if err := q.E4SetUsed(ctx, dbq.E4SetUsedParams{
		UsedAmount: used, UpdatedAt: types.TimestampOf(now).String(), UpdatedBy: h.UpdatedBy, ID: a.ID,
	}); err != nil {
		return err
	}
	return audit.Append(ctx, tx, audit.Entry{
		DocType: AdvanceDocType, DocID: a.ID, Action: actionEffect, By: types.UserID(h.UpdatedBy), At: now,
		Changes: []audit.Change{{
			Field: "used_amount", Before: strconv.FormatInt(a.UsedAmount, 10), After: strconv.FormatInt(used, 10), Op: audit.OpSet,
		}},
		CausedBy: &audit.Ref{DocType: ClaimDocType, DocID: id, Number: h.Number.String, Action: cause},
	})
}
