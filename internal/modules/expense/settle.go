package expense

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Advance settlement on the claim (spec §3.1, §4 Settlement, §5; CD-264 (6)).
// less_advance NULL means as much as the advance allows: Validate keeps it
// NULL and computes the effective amount for net_payable on every save;
// checkAdvanceLeft writes it at submit, so an approved claim holds a number.

// settleOnSave is the claim's save-time advance rules, called by
// validateClaim with the claim's total. It returns the less advance that
// net_payable subtracts: the stored one, min(left, total) when NULL, and 0
// when no advance can be set off, so net_payable never keeps a figure from an
// advance the claim no longer uses (CD-302). An advance the updater can't read
// was refused by the engine's link check first (CD-240).
func settleOnSave(ctx context.Context, tx *db.Tx, _ Deps, id int64, total types.Money) (types.Money, []doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	h, a, ok, err := claimAdvance(ctx, q, id)
	if err != nil {
		return 0, nil, err
	}
	if !ok {
		if h.LessAdvance.Valid {
			return 0, []doctype.FieldError{{Field: "less_advance", Message: msgChooseAdvance}}, nil
		}
		return 0, nil, nil
	}
	if a.EmployeeID != h.EmployeeID {
		return 0, []doctype.FieldError{{Field: "advance_id", Message: fmt.Sprintf(msgAdvanceOtherEmployee, advanceLabel(a))}}, nil
	}
	if h.LessAdvance.Valid {
		return types.Money(h.LessAdvance.Int64), nil, nil
	}
	left, err := leftOn(ctx, q, a)
	if err != nil {
		return 0, nil, err
	}
	return min(left, total), nil, nil
}

// submitAdvanceFields is the claim's submit-only advance field rules: a typed
// 0 against an advance, and a typed amount above the total.
func submitAdvanceFields(ctx context.Context, tx *db.Tx, _ Deps, id int64) ([]doctype.FieldError, error) {
	h, a, ok, err := claimAdvance(ctx, dbq.New(tx.SQL()), id)
	if err != nil || !ok {
		return nil, err
	}
	var errs []doctype.FieldError
	if h.LessAdvance.Valid && h.LessAdvance.Int64 == 0 {
		errs = append(errs, doctype.FieldError{Field: "less_advance", Message: fmt.Sprintf(msgAdvanceZero, advanceLabel(a))})
	}
	if h.LessAdvance.Int64 > h.Total.Int64 {
		errs = append(errs, doctype.FieldError{Field: "less_advance", Message: msgLessAboveTotal})
	}
	return errs, nil
}

// checkAdvanceLeft runs last in BeforeSubmit, on submit and again on approve
// (D-APR-15), reading the advance inside the transaction (CD-94): it isn't
// closed, it is marked as paid (CD-271 G1), and the less advance is no more
// than what's left after the other approved and closed claims. A NULL less
// advance becomes min(left, total) and is written with net_payable. Validate
// ran in this transaction just before, so the employee rule and the engine's
// source-state check already hold.
func checkAdvanceLeft(ctx context.Context, tx *db.Tx, _ Deps, id int64) error {
	q := dbq.New(tx.SQL())
	h, a, ok, err := claimAdvance(ctx, q, id)
	if err != nil || !ok {
		return err
	}
	label := advanceLabel(a)
	if a.State == string(doc.StateClosed) {
		return settleInvalid("advance_id", fmt.Sprintf(msgAdvanceClosed, label))
	}
	if !a.PaidOn.Valid {
		return settleInvalid("advance_id", fmt.Sprintf(msgAdvanceNotPaid, label))
	}
	left, err := leftOn(ctx, q, a)
	if err != nil {
		return err
	}
	less := types.Money(h.LessAdvance.Int64)
	if !h.LessAdvance.Valid {
		less = min(left, types.Money(h.Total.Int64))
		if less == 0 { // nothing left: the same rule as a typed 0
			return settleInvalid("less_advance", fmt.Sprintf(msgAdvanceZero, label))
		}
		if err := q.E4SetLess(ctx, dbq.E4SetLessParams{
			LessAdvance: sql.NullInt64{Int64: int64(less), Valid: true},
			NetPayable:  sql.NullInt64{Int64: h.Total.Int64 - int64(less), Valid: true},
			ID:          id,
		}); err != nil {
			return err
		}
	}
	if less > left {
		return settleInvalid("less_advance", fmt.Sprintf(msgLessAboveLeft, types.FormatMoney(left), label))
	}
	return nil
}

// claimAdvance reads claim id and the advance it names; ok is false when it
// names none.
func claimAdvance(ctx context.Context, q *dbq.Queries, id int64) (dbq.ClaimHeaderRow, dbq.AdvanceHeaderRow, bool, error) {
	h, err := q.ClaimHeader(ctx, id)
	if err != nil || !h.AdvanceID.Valid {
		return h, dbq.AdvanceHeaderRow{}, false, err
	}
	a, err := q.AdvanceHeader(ctx, h.AdvanceID.Int64)
	return h, a, err == nil, err
}

// leftOn is what's left on advance a: its amount less the sum over its
// approved and closed claims, read in the caller's transaction.
func leftOn(ctx context.Context, q *dbq.Queries, a dbq.AdvanceHeaderRow) (types.Money, error) {
	used, err := q.E4Used(ctx, sql.NullInt64{Int64: a.ID, Valid: true})
	return types.Money(a.Amount.Int64 - used), err
}

// advanceLabel is the advance's number, or Draft {id} before approval.
func advanceLabel(a dbq.AdvanceHeaderRow) string { return naming.Label(a.Number.String, a.ID) }

// settleInvalid is a submit refusal on field, so the check page highlights it.
func settleInvalid(field, msg string) error {
	return doc.Invalid([]doctype.FieldError{{Field: field, Message: msg}})
}
