// Package auth holds rights, scopes, principals and the user directory
// contract (spec §5). AUTH owns it.
package auth

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

type UserID = types.UserID

type Right string // read create write submit cancel amend delete approve print export import

type Scope string // own team all (CD-11)

// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
const (
	ScopeOwn  Scope = "own"
	ScopeTeam Scope = "team"
	ScopeAll  Scope = "all"
)

type Principal struct {
	ID    UserID
	Name  string
	Roles []string
}

type RecordRef struct {
	DocType             string
	ID                  int64
	CreatedBy           UserID
	EmployeeID          int64
	ProjectIDs, SiteIDs []int64
}

type Predicate struct { // CD-16 naming; "" = no filter
	SQL  string
	Args []any
}

type UserInfo struct {
	ID          UserID
	Email, Name string
	Enabled     bool
}

// forbiddenError is the WP0 placeholder behind ErrForbidden (CD-44). AUTH may
// replace the value but keeps doctype.HTTPError. A non-empty msg is
// Forbidden's custom message (CD-174).
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
type forbiddenError struct{ msg string }

func (e *forbiddenError) Error() string {
	if e.msg != "" {
		return e.msg
	}
	return "You don't have permission to do this. Ask your administrator for a role that allows it."
}
func (*forbiddenError) Is(target error) bool              { return target == ErrForbidden }
func (*forbiddenError) Status() int                       { return 403 }
func (*forbiddenError) FieldErrors() []doctype.FieldError { return nil }

// ErrForbidden (403) and ErrNotFound (404). ErrNotFound is doctype.ErrNotFound (CD-54).
//
// Working WP0 body; the owning WP may move it out of contract.go (CD-84).
var (
	ErrForbidden error = &forbiddenError{}
	ErrNotFound        = doctype.ErrNotFound
)

type Service interface {
	Can(ctx context.Context, p Principal, doctype string, r Right, rec *RecordRef) error
	Scope(ctx context.Context, p Principal, doctype string, r Right) (Predicate, error)
	CheckValues(ctx context.Context, p Principal, doctype string, values map[string][]int64) error // proposed link ids vs restrictions (CD-12)
	CheckLink(ctx context.Context, p Principal, target string, id int64) error                     // linked record visible?
	UsersWithRight(ctx context.Context, doctype string, r Right, rec *RecordRef) ([]UserID, error)
	HasRole(ctx context.Context, u UserID, role string) (bool, error)
	User(ctx context.Context, tx *db.Tx, u UserID) (UserInfo, bool, error)
	DisableUser(ctx context.Context, tx *db.Tx, u UserID, reason string) error
	EnableUser(ctx context.Context, tx *db.Tx, u UserID) error
	Restricted(ctx context.Context, tx *db.Tx, p Principal, dimension string) (bool, error) // any restriction on dimension? (CD-174)
}

type Directory interface { // CD-13: implemented by MST, wired by internal/app
	EmployeeForUser(ctx context.Context, tx *db.Tx, u UserID) (employeeID int64, ok bool, err error)
	UserForEmployee(ctx context.Context, tx *db.Tx, employeeID int64) (UserID, bool, error)
	ManagerChain(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error)  // direct manager first
	DirectReports(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) // active only
	ProjectCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error)
	SiteCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error)
}
