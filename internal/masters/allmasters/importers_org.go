package allmasters

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// orgImporters is the Organisation group's RowFuncs (Task 21).
func orgImporters() map[string]rowFunc {
	return map[string]rowFunc{
		project.DocType:  (*Services).importProject,
		site.DocType:     (*Services).importSite,
		employee.DocType: (*Services).importEmployee,
	}
}

func (s *Services) importProject(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	branchID, err1 := cellInt64(row["branch_id"])
	customerID, err2 := cellInt64(row["customer_id"])
	start, err3 := cellValue(row["start_date"], types.ParseDate)
	end, err4 := cellValue(row["end_date"], types.ParseDate)
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return 0, nil, err
	}
	saved, err := s.Project.CreateTx(ctx, tx, p, project.Input{
		Code: row["code"], Name: row["name"], BranchID: branchID, CustomerID: customerID, Start: start, End: end,
		Status: project.Status(cellSelect(project.DocType, "status", row["status"])),
	})
	return rowResult(ctx, tx, p, row, saved, err, s.Project.SetDisabledTx)
}

func (s *Services) importSite(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	branchID, err1 := cellInt64(row["branch_id"])
	projectID, err2 := cellInt64(row["project_id"])
	if err := errors.Join(err1, err2); err != nil {
		return 0, nil, err
	}
	saved, err := s.Site.CreateTx(ctx, tx, p, site.Input{
		Code: row["code"], Name: row["name"], BranchID: branchID, ProjectID: projectID,
		Type: site.SiteType(cellSelect(site.DocType, "type", row["type"])),
	})
	return rowResult(ctx, tx, p, row, saved, err, s.Site.SetDisabledTx)
}

// importEmployee refuses a User Email that isn't an enabled user on every row:
// the master's own check skips a Left row (CD-199). Employee has no Disabled
// column: disabled mirrors Status.
func (s *Services) importEmployee(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (int64, []doctype.FieldError, error) {
	branchID, err1 := cellInt64(row["branch_id"])
	managerID, err2 := cellInt64(row["manager_id"])
	joining, err3 := cellValue(row["joining_date"], types.ParseDate)
	birth, err4 := cellValue(row["birth_date"], types.ParseDate)
	relieving, err5 := cellValue(row["relieving_date"], types.ParseDate)
	if err := errors.Join(err1, err2, err3, err4, err5); err != nil {
		return 0, nil, err
	}
	var userID auth.UserID
	if email := row["user_email"]; email != "" {
		user, ok, err := s.env.Auth.UserByEmail(ctx, tx, email)
		if err != nil {
			return 0, nil, err
		}
		if !ok || !user.Enabled {
			return 0, []doctype.FieldError{{Field: "user_email", Message: employee.MsgNoUser}}, nil
		}
		userID = user.ID
	}
	saved, err := s.Employee.CreateTx(ctx, tx, p, employee.Input{
		Code: row["code"], Name: row["name"], BranchID: branchID, UserID: userID, ManagerID: managerID,
		Joining: joining, Birth: birth, Relieving: relieving,
		Status: employee.Status(cellSelect(employee.DocType, "status", row["status"])),
	})
	return savedRow(saved, err)
}
