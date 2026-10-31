package expense

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The approval amount and the numbering date (spec §3.1, §4, §7). Built in E1
// rather than E5 so documents can be approved from the start, as MOD-PO P1
// does: a zero date makes naming refuse every approve.

// amountClaim is the claim's total, not net_payable: the approver approves
// the spending (spec §3.1).
func amountClaim(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	h, err := dbq.New(tx.SQL()).ClaimHeader(ctx, id)
	if err != nil {
		return 0, err
	}
	return types.Money(h.Total.Int64), nil
}

// dateClaim is the claim date: the FY of its number and its lock date.
func dateClaim(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	h, err := dbq.New(tx.SQL()).ClaimHeader(ctx, id)
	if err != nil {
		return types.Date{}, err
	}
	return dateOf(h.ClaimDate)
}

// amountAdvance is the advance's amount.
func amountAdvance(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	h, err := dbq.New(tx.SQL()).AdvanceHeader(ctx, id)
	if err != nil {
		return 0, err
	}
	return types.Money(h.Amount.Int64), nil
}

// dateAdvance is the advance date: the FY of its number and its lock date.
func dateAdvance(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	h, err := dbq.New(tx.SQL()).AdvanceHeader(ctx, id)
	if err != nil {
		return types.Date{}, err
	}
	return dateOf(h.AdvanceDate)
}

// dateOf is a stored date column; NULL is the zero date.
func dateOf(s sql.NullString) (types.Date, error) {
	if !s.Valid || s.String == "" {
		return types.Date{}, nil
	}
	return types.DateFromISO(s.String)
}
