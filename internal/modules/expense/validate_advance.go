package expense

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// validateAdvance is the advance's Validate (spec §4): the claim's employee
// rule and snapshots while it is a draft, the advance date not in the
// future, and amount as the sum of the lines.
func validateAdvance(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	h, err := q.AdvanceHeader(ctx, id)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	if h.State == string(doctype.StateDraft) {
		emp, fe, err := checkEmployee(ctx, tx, d, h.EmployeeID, h.UpdatedBy, msgOnlyOwnAdvances)
		if err != nil {
			return nil, err
		}
		errs = append(errs, fe...)
		if emp != h.EmployeeID {
			if err := q.E2SetAdvanceEmployee(ctx, dbq.E2SetAdvanceEmployeeParams{EmployeeID: emp, ID: id}); err != nil {
				return nil, err
			}
		}
		var e dbq.E2EmployeeRow // blank with no employee (CD-302)
		if emp.Valid {
			if e, err = q.E2Employee(ctx, emp.Int64); err != nil {
				return nil, err
			}
		}
		if err := q.E2SnapAdvance(ctx, dbq.E2SnapAdvanceParams{SnapEmployeeCode: e.Code, SnapEmployeeName: e.Name, ID: id}); err != nil {
			return nil, err
		}
		if err := q.E2SnapAdvanceLines(ctx, id); err != nil {
			return nil, err
		}
	}

	if h.AdvanceDate.Valid && h.AdvanceDate.String > types.Today(d.Clock).ISO() {
		errs = append(errs, doctype.FieldError{Field: "advance_date", Message: msgAdvanceDateFuture})
	}

	amounts, err := q.E2AdvanceLines(ctx, id)
	if err != nil {
		return nil, err
	}
	var amount int64
	for _, a := range amounts {
		amount += a.Int64
	}
	if err := q.E2SetAdvanceAmount(ctx, dbq.E2SetAdvanceAmountParams{Amount: sql.NullInt64{Int64: amount, Valid: true}, ID: id}); err != nil {
		return nil, err
	}
	return errs, nil
}
