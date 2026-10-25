package approval

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// History is the document's approval history, oldest first.
func (a *Approvals) History(ctx context.Context, tx *db.Tx, dt string, id int64) ([]HistoryRow, error) {
	rows, err := dbq.New(tx.SQL()).ListHistory(ctx, dbq.ListHistoryParams{DocType: dt, DocID: id})
	if err != nil {
		return nil, err
	}
	out := make([]HistoryRow, 0, len(rows))
	for _, r := range rows {
		at, err := parseTS(r.At)
		if err != nil {
			return nil, err
		}
		out = append(out, HistoryRow{
			Action: r.Action, By: types.UserID(r.UserID), ByName: r.UserName, At: at,
			Amount: types.Money(r.Amount.Int64), RuleID: r.RuleID.Int64, Reason: r.Reason, SelfApproved: r.SelfApproved != 0,
		})
	}
	return out, nil
}

// writeHistory appends p's action now. A non-nil r gives the row its
// request, amount and rule; with r nil the row carries amount, if valid.
func (a *Approvals) writeHistory(ctx context.Context, tx *db.Tx, dt string, id int64, action string, p auth.Principal, r *Request, amount sql.NullInt64, reason string, self bool) error {
	name, err := a.userName(ctx, tx, p.ID)
	if err != nil {
		return err
	}
	h := dbq.InsertHistoryParams{
		DocType: dt, DocID: id, Action: action, UserID: int64(p.ID), UserName: name,
		At: ts(a.clock.Now()), Amount: amount, Reason: reason,
	}
	if self {
		h.SelfApproved = 1
	}
	if r != nil {
		h.RequestID = sql.NullInt64{Int64: r.ID, Valid: true}
		h.Amount = sql.NullInt64{Int64: int64(r.Amount), Valid: true}
		h.RuleID = sql.NullInt64{Int64: r.RuleID, Valid: true}
	}
	_, err = dbq.New(tx.SQL()).InsertHistory(ctx, h)
	return err
}
