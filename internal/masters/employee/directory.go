package employee

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee/dbq"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Directory is MST's auth.Directory (CD-13, M10): employees, their users and
// managers, and project and site codes.
type Directory struct {
	Employees *Service
	Projects  *project.Service
	Sites     *site.Service
}

var _ auth.Directory = (*Directory)(nil)

// EmployeeForUser is u's active employee (at most one, CD-39).
func (d *Directory) EmployeeForUser(ctx context.Context, tx *db.Tx, u auth.UserID) (int64, bool, error) {
	r, err := dbq.New(tx.SQL()).ActiveEmployeeForUser(ctx, dbq.ActiveEmployeeForUserParams{UserID: nullID(int64(u)), ID: 0})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return r.ID, true, nil
}

// UserForEmployee is the employee's user; ok is false when the employee is
// missing, has left (CD-54) or has no user.
func (d *Directory) UserForEmployee(ctx context.Context, tx *db.Tx, employeeID int64) (auth.UserID, bool, error) {
	e, err := d.Employees.Get(ctx, tx, employeeID)
	if errors.Is(err, doctype.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	if e.Status != Active || e.UserID == 0 {
		return 0, false, nil
	}
	return e.UserID, true, nil
}

// ManagerChain is Service.ManagerChain: direct manager first.
func (d *Directory) ManagerChain(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	return d.Employees.ManagerChain(ctx, tx, employeeID)
}

// DirectReports is Service.DirectReports: active only, by code.
func (d *Directory) DirectReports(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	return d.Employees.DirectReports(ctx, tx, employeeID)
}

// ProjectCodes maps project ids to codes.
func (d *Directory) ProjectCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	return d.Projects.Codes(ctx, tx, ids)
}

// SiteCodes maps site ids to codes.
func (d *Directory) SiteCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	return d.Sites.Codes(ctx, tx, ids)
}
