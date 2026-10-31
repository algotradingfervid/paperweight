package expense

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// PaidPage is an advance as the Mark as paid and Undo paid pages show it
// (UX §5.18).
type PaidPage struct {
	Label, Employee string
	Amount          types.Money
	Version         int
	PaidOn          *types.Date // nil while the advance is unpaid
	Today           types.Date  // the IST business date, Paid on's default
}

// PaidPageOf reads advance id for p's Mark as paid and Undo paid pages, in
// E9's check order: missing or hidden is AUTH's 404 (CD-245, CD-312), then
// AUTH's 403 without cancel on it.
func (s *Service) PaidPageOf(ctx context.Context, p auth.Principal, id int64) (PaidPage, error) {
	out := PaidPage{Today: types.Today(s.deps.Clock)}
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		a, err := q.E9Advance(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		switch ok, err := s.readsAdvance(ctx, tx, p, id); {
		case err != nil:
			return err
		case !ok:
			return auth.ErrNotFound
		}
		ref := &auth.RecordRef{DocType: AdvanceDocType, ID: a.ID, CreatedBy: types.UserID(a.CreatedBy), EmployeeID: a.EmployeeID.Int64}
		if err := s.deps.Auth.CanTx(ctx, tx, p, AdvanceDocType, auth.RightCancel, ref); err != nil {
			return err
		}
		r, err := q.E4AdvanceRef(ctx, id)
		if err != nil {
			return err
		}
		out.Label, out.Employee = naming.Label(a.Number.String, a.ID), r.SnapEmployeeName
		out.Amount, out.Version = types.Money(r.Amount.Int64), int(a.Version)
		if a.PaidOn.Valid {
			d, err := dateOf(a.PaidOn)
			if err != nil {
				return err
			}
			out.PaidOn = &d
		}
		return nil
	})
	if err != nil {
		return PaidPage{}, err
	}
	return out, nil
}
