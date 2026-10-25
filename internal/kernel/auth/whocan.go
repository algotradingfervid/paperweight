package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/errs"
)

// WhoCanUser is one enabled user holding a role, with the restriction codes
// from RestrictionLabels (Task 19).
type WhoCanUser struct {
	ID           UserID
	Name         string
	Restrictions map[Dimension][]string
}

// WhoCanRole is one role that holds a right on a DocType, with its scope and
// its enabled users (Task 19).
type WhoCanRole struct {
	Role  string
	Scope Scope
	Users []WhoCanUser
}

// WhoCan lists the roles and users that hold r on doctype (Task 19).
func (a *Auth) WhoCan(ctx context.Context, doctype string, r Right) ([]WhoCanRole, error) {
	return nil, errs.ErrNotImplemented
}
