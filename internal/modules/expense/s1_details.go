package expense

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// What the claim wizard reads beyond AdvanceChoices (MOD-EXP S1,
// UX §5.3).

// OwnEmployee is p's employee, ok false when p has none: on a new claim the
// advances listed are the creator's.
func (s *Service) OwnEmployee(ctx context.Context, p auth.Principal) (id int64, ok bool, err error) {
	err = s.db.Read(ctx, func(tx *db.Tx) error {
		id, ok, err = s.deps.Directory.EmployeeForUser(ctx, tx, p.ID)
		return err
	})
	return id, ok, err
}

// HasUnpaidAdvance reports whether employeeID has an approved advance not
// marked as paid yet, with something left, that p can read (CD-271 G1): the
// details step then explains why it isn't listed. One p can't read doesn't
// count (CD-233).
func (s *Service) HasUnpaidAdvance(ctx context.Context, p auth.Principal, employeeID int64) (bool, error) {
	var found bool
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		ids, err := dbq.New(tx.SQL()).S1UnpaidAdvances(ctx, sql.NullInt64{Int64: employeeID, Valid: true})
		if err != nil {
			return err
		}
		for _, id := range ids {
			if found, err = s.readsAdvance(ctx, tx, p, id); found || err != nil {
				return err
			}
		}
		return nil
	})
	return found, err
}

// BilledGSTIN is claim line lineID's Billed-to GSTIN as snapshotted (CD-271
// G4), "" when it has none: the line card shows it while the company has
// more than one branch GSTIN (UX §5.4, EUX-045).
func BilledGSTIN(ctx context.Context, tx *db.Tx, lineID int64) (string, error) {
	return dbq.New(tx.SQL()).S1BilledGSTIN(ctx, lineID)
}
