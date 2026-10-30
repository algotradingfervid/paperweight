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
)

// orgImporters is the Organisation group's RowFuncs (Task 21).
func orgImporters() map[string]rowFunc {
	return map[string]rowFunc{
		project.DocType:  (*Services).importProject,
		site.DocType:     (*Services).importSite,
		employee.DocType: (*Services).importEmployee,
	}
}

func (*Services) importProject(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Project import not implemented")
}

func (*Services) importSite(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Site import not implemented")
}

func (*Services) importEmployee(context.Context, *db.Tx, auth.Principal, map[string]string) (int64, []doctype.FieldError, error) {
	return 0, nil, errors.New("allmasters: Employee import not implemented")
}
