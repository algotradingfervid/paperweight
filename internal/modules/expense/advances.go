package expense

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// An employee's open advances for the screens (MOD-EXP E4 Do 5 to 7). Each
// read checks the viewer's read right on every advance it names in the same
// read (CD-240, CD-248).

// AdvanceChoice is one advance a claim may set off: the claim's details step
// shows one radio per choice (UX §5.3).
type AdvanceChoice struct {
	ID      int64  // posted as advance_id
	Label   string // the advance's number
	Date    types.Date
	Purpose string
	Amount  types.Money
	Left    types.Money // amount − used
}

// AdvanceChoices are employeeID's approved advances marked as paid with
// something left that p can read, oldest first.
func (s *Service) AdvanceChoices(ctx context.Context, p auth.Principal, employeeID int64) ([]AdvanceChoice, error) {
	var out []AdvanceChoice
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).E4OpenAdvances(ctx, sql.NullInt64{Int64: employeeID, Valid: true})
		if err != nil {
			return err
		}
		for _, r := range rows {
			ok, err := s.readsAdvance(ctx, tx, p, r.ID, r.CreatedBy, r.EmployeeID)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			date, err := dateOf(r.AdvanceDate)
			if err != nil {
				return err
			}
			out = append(out, AdvanceChoice{
				ID: r.ID, Label: naming.Label(r.Number.String, r.ID), Date: date, Purpose: r.Purpose,
				Amount: types.Money(r.Amount.Int64), Left: types.Money(r.Amount.Int64 - r.UsedAmount),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LeftOn is what's left on advance advanceID (amount − used), for its close
// page (EXP-059). An advance p can't read is missing (CD-245).
func (s *Service) LeftOn(ctx context.Context, p auth.Principal, advanceID int64) (types.Money, error) {
	var left types.Money
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		a, err := s.readAdvance(ctx, tx, p, advanceID)
		left = types.Money(a.Amount.Int64 - a.UsedAmount)
		return err
	})
	if err != nil {
		return 0, err
	}
	return left, nil
}

// AdvanceWarnings are the G7 warnings on advance advanceID's check and
// approval pages (CD-271 G7, CD-281 d): one per other approved advance of
// its employee, marked as paid, with something left, that p can read: an
// advance p can't read gives no warning, since its amount would leak
// (CD-305 (1); CD-233, CD-240, CD-248). They never refuse.
func (s *Service) AdvanceWarnings(ctx context.Context, p auth.Principal, advanceID int64) ([]string, error) {
	var out []string
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		a, err := s.readAdvance(ctx, tx, p, advanceID)
		if err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).E4OpenAdvances(ctx, a.EmployeeID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if r.ID == advanceID {
				continue
			}
			ok, err := s.readsAdvance(ctx, tx, p, r.ID, r.CreatedBy, r.EmployeeID)
			if err != nil {
				return err
			}
			if !ok {
				continue
			}
			left := types.FormatMoney(types.Money(r.Amount.Int64 - r.UsedAmount))
			out = append(out, fmt.Sprintf(msgAdvanceLeft, a.SnapEmployeeName, left, naming.Label(r.Number.String, r.ID)))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// readAdvance reads advance id for p: missing or hidden is doc.Gone (hidden
// looks the same as missing, CD-245).
func (s *Service) readAdvance(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) (dbq.E4AdvanceRefRow, error) {
	a, err := dbq.New(tx.SQL()).E4AdvanceRef(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return a, doc.Gone()
	}
	if err != nil {
		return a, err
	}
	ok, err := s.readsAdvance(ctx, tx, p, id, a.CreatedBy, a.EmployeeID)
	if err == nil && !ok {
		err = doc.Gone()
	}
	return a, err
}

// readsAdvance reports whether p may read advance id: a record outside p's
// read scope is ErrNotFound, no read grant at all is ErrForbidden. The
// advance declares no ProjectFields or SiteFields (spec §4).
func (s *Service) readsAdvance(ctx context.Context, tx *db.Tx, p auth.Principal, id, createdBy int64, employee sql.NullInt64) (bool, error) {
	rec := auth.RecordRef{DocType: AdvanceDocType, ID: id, CreatedBy: auth.UserID(createdBy), EmployeeID: employee.Int64}
	err := s.deps.Auth.CanTx(ctx, tx, p, AdvanceDocType, auth.RightRead, &rec)
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}
