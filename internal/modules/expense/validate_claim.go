package expense

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// validateClaim is the claim's Validate (spec §3.1, §3.2, §5 save rows). It
// runs on every save and again on submit and approve. While the claim is a
// draft it also checks and defaults the employee and writes the line
// defaults and the snapshots, so a later master edit doesn't reach a
// submitted claim. Totals and the period are written every time.
func validateClaim(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	h, err := q.ClaimHeader(ctx, id)
	if err != nil {
		return nil, err
	}
	draft := h.State == string(doctype.StateDraft)
	var errs []doctype.FieldError
	if draft {
		emp, fe, err := checkEmployee(ctx, tx, d, h.EmployeeID, h.UpdatedBy, msgOnlyOwnClaims)
		if err != nil {
			return nil, err
		}
		errs = append(errs, fe...)
		if emp != h.EmployeeID {
			if err := q.E2SetClaimEmployee(ctx, dbq.E2SetClaimEmployeeParams{EmployeeID: emp, ID: id}); err != nil {
				return nil, err
			}
		}
		if emp.Valid {
			e, err := q.E2Employee(ctx, emp.Int64)
			if err != nil {
				return nil, err
			}
			if err := q.E2SnapClaim(ctx, dbq.E2SnapClaimParams{SnapEmployeeCode: e.Code, SnapEmployeeName: e.Name, ID: id}); err != nil {
				return nil, err
			}
		}
	}

	today := types.Today(d.Clock).ISO()
	if h.ClaimDate.Valid && h.ClaimDate.String > today {
		errs = append(errs, doctype.FieldError{Field: "claim_date", Message: msgClaimDateFuture})
	}

	lines, err := q.E2ClaimLines(ctx, id)
	if err != nil {
		return nil, err
	}
	if draft {
		if err := typeDefaults(ctx, tx, d, lines); err != nil {
			return nil, err
		}
		if err := setBilledTo(ctx, tx, lines); err != nil {
			return nil, err
		}
		if err := q.E2SnapClaimLines(ctx, id); err != nil {
			return nil, err
		}
	}

	var total int64
	var from, to sql.NullString
	for _, l := range lines {
		n := int(l.LineNo)
		if date := l.ExpenseDate; date.Valid {
			switch {
			case date.String > today:
				errs = append(errs, doctype.FieldError{Field: "expense_date", Line: n, Message: fmt.Sprintf(msgLineDateFuture, n)})
			case h.ClaimDate.Valid && date.String > h.ClaimDate.String:
				errs = append(errs, doctype.FieldError{Field: "expense_date", Line: n, Message: fmt.Sprintf(msgLineAfterClaim, n)})
			}
			if !from.Valid || date.String < from.String {
				from = date
			}
			if !to.Valid || date.String > to.String {
				to = date
			}
		}
		fe, err := checkGST(ctx, q, l)
		if err != nil {
			return nil, err
		}
		errs = append(errs, fe...)
		total += l.Amount.Int64
	}

	less, fe, err := settleOnSave(ctx, tx, d, id, types.Money(total))
	if err != nil {
		return nil, err
	}
	errs = append(errs, fe...)
	if err := q.E2SetClaimTotals(ctx, dbq.E2SetClaimTotalsParams{
		Total:      sql.NullInt64{Int64: total, Valid: true},
		NetPayable: sql.NullInt64{Int64: total - int64(less), Valid: true},
		PeriodFrom: from, PeriodTo: to, ID: id,
	}); err != nil {
		return nil, err
	}
	return errs, nil
}

// checkEmployee is the employee rule of a draft claim or advance (spec §3.1):
// blank becomes the employee of updatedBy, who must have one; another
// employee needs an updater holding the Administrator role, since updated_by,
// not created_by, is who is saving now. It returns the employee to store.
func checkEmployee(ctx context.Context, tx *db.Tx, d Deps, stored sql.NullInt64, updatedBy int64, onlyOwn string) (sql.NullInt64, []doctype.FieldError, error) {
	u := auth.UserID(updatedBy)
	own, ok, err := d.Directory.EmployeeForUser(ctx, tx, u)
	if err != nil {
		return stored, nil, err
	}
	if !stored.Valid {
		if !ok {
			return stored, []doctype.FieldError{{Field: "employee_id", Message: msgNoEmployee}}, nil
		}
		return sql.NullInt64{Int64: own, Valid: true}, nil, nil
	}
	if ok && stored.Int64 == own {
		return stored, nil, nil
	}
	admin, err := d.Auth.HasRoleTx(ctx, tx, u, auth.RoleAdministrator)
	if err != nil || admin {
		return stored, nil, err
	}
	return stored, []doctype.FieldError{{Field: "employee_id", Message: onlyOwn}}, nil
}
