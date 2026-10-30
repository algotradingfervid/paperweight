package auth

import (
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Problem is a user-facing error with its HTTP status (CD-44). The field is
// Code because Status is the method.
type Problem struct {
	Code int
	Msg  string
}

func (p *Problem) Error() string                     { return p.Msg }
func (p *Problem) Status() int                       { return p.Code }
func (p *Problem) FieldErrors() []doctype.FieldError { return nil }

// MsgNotFound is the 404 text; it is also ErrNotFound.Error() (CD-54, CD-76).
const MsgNotFound = "This record doesn't exist or you don't have access to it."

// User-facing messages (copy exactly; plan Task 1 message table).
var (
	ErrLoginIncomplete      = &Problem{400, "Enter your email and password."}
	ErrLoginInvalid         = &Problem{401, "Invalid email or password."}
	ErrAccountDisabled      = &Problem{403, "Your account is disabled. Ask your administrator to enable it."}
	ErrAccountLocked        = &Problem{429, "Too many failed sign-in attempts. Try again in 15 minutes, or ask your administrator to unlock your account."}
	ErrIPBlocked            = &Problem{429, "Too many failed sign-in attempts from your network. Try again in 15 minutes."}
	ErrPasswordTooShort     = &Problem{400, "Password must be at least 10 characters."}
	ErrPasswordTooLong      = &Problem{400, "Password must be at most 256 characters."}
	ErrPasswordCommon       = &Problem{400, "This password is too common. Choose a longer or less predictable one."}
	ErrPasswordHasName      = &Problem{400, "Password must not contain your name or email."}
	ErrPasswordSame         = &Problem{400, "New password must be different from your current password."}
	ErrCurrentPasswordWrong = &Problem{400, "Current password is incorrect."}
	ErrPasswordMismatch     = &Problem{400, "New password and confirmation do not match."}
	ErrSessionExpired       = &Problem{401, "Your session has expired. Sign in again."} // CD-17
	ErrSignInRequired       = &Problem{401, "Sign in to continue."}
	ErrCSRF                 = &Problem{403, "Your form has expired. Reload the page and try again."}
	ErrNotAdminPassword     = &Problem{403, "You don't have permission to change other users' passwords."}
	ErrAdminOnlyField       = &Problem{403, "Only an administrator can change this."}
	ErrAdminOnlyRoles       = &Problem{403, "Only an administrator can change roles."}
	ErrAdminOnlyAccess      = &Problem{403, "Only an administrator can change site and project access."}
	ErrAdminOnlyWhoCan      = &Problem{403, "Only an administrator can see who has which rights."}
	ErrAdminOnlyUsers       = &Problem{403, "Only an administrator can manage users."}
	ErrBadForm              = &Problem{400, "The form could not be read. Reload the page and try again."}
	ErrSetupDone            = &Problem{409, "Setup is already complete. Sign in instead."}
	ErrUsersExist           = &Problem{409, "Users already exist. Sign in as an administrator to add more users."}
	ErrEmailInvalid         = &Problem{400, "Enter a valid email address, such as name@company.in."}
	ErrCannotDisableSelf    = &Problem{400, "You can't disable your own account. Ask another administrator."}
	ErrCannotDemoteSelf     = &Problem{400, "You can't remove your own Administrator role. Ask another administrator."}
	ErrLastAdmin            = &Problem{409, "At least one enabled user must have the Administrator role."}
	ErrAdminRoleFixed       = &Problem{400, "The Administrator role can't be changed."}
	ErrChooseSite           = &Problem{422, "Choose a site."}    // CD-194
	ErrChooseProject        = &Problem{422, "Choose a project."} // CD-194
)

// EmailTakenError: another user has email.
func EmailTakenError(email string) *Problem {
	return &Problem{409, fmt.Sprintf("A user with email %s already exists.", email)}
}

// ReferencedUserError: the user can't be deleted (CD-19).
func ReferencedUserError(name string) *Problem {
	return &Problem{409, fmt.Sprintf("%s has created or changed records, so they can't be deleted. Disable them instead.", name)}
}

// UserChangedError: the user was saved by someone else after the page was
// opened (the DOC-069 wording; name is the user's stored name).
func UserChangedError(name string) *Problem {
	return changedError(name)
}

// RoleChangedError: the role was saved by someone else after the page was
// opened (UserChangedError's wording; name is the role's stored name).
func RoleChangedError(name string) *Problem {
	return changedError(name)
}

// changedError is the shared body of UserChangedError and RoleChangedError.
func changedError(name string) *Problem {
	return &Problem{409, fmt.Sprintf("Someone else changed %s after you opened it. Reload it and make your changes again.", name)}
}

// NoRolesWarning is shown after saving a user with no roles.
func NoRolesWarning(name string) string {
	return fmt.Sprintf("%s has no roles and will see nothing after signing in.", name)
}

// RoleInUseError: the role can't be deleted while n users hold it.
func RoleInUseError(role string, n int) *Problem {
	users := fmt.Sprintf("%d users", n)
	if n == 1 {
		users = "1 user"
	}
	return &Problem{409, fmt.Sprintf("Role %s is assigned to %s. Remove it from them before deleting the role.", role, users)}
}

// RoleExistsError: existing is the name of the role that already exists.
func RoleExistsError(existing string) *Problem {
	return &Problem{409, fmt.Sprintf("A role named %s already exists.", existing)}
}

// ReadRequiredError: a matrix row grants a right without read.
func ReadRequiredError(label string) *Problem {
	return &Problem{400, fmt.Sprintf("Read is needed for any other right on %s. Tick Read or clear the other rights.", label)}
}

// NotApplicableError: r doesn't apply to the DocType.
func NotApplicableError(r Right, label string) *Problem {
	return &Problem{400, fmt.Sprintf("%s does not apply to %s. Clear it.", RightLabel(r), label)}
}

// RightLabel is how a right reads on its own: capitalised, "Approve".
func RightLabel(r Right) string {
	name := string(r)
	if name != "" {
		name = strings.ToUpper(name[:1]) + name[1:]
	}
	return name
}

// TeamNotApplicableError: team scope on a DocType without an employee field.
func TeamNotApplicableError(label string) *Problem {
	return &Problem{400, fmt.Sprintf("Team does not apply to %s. Choose Own or All.", label)}
}

// ScopeInvalidError: a matrix right has an empty or unknown scope.
func ScopeInvalidError(r Right) *Problem {
	return &Problem{400, fmt.Sprintf("Scope for %s must be own, team or all.", r)}
}

// verb is how a right reads in a sentence.
func verb(r Right) string {
	if r == RightWrite {
		return "change"
	}
	return string(r)
}

// DeniedError: the principal lacks r on a DocType, or holds it only for own
// or team records. Label and Plural come from the DocType (CD-35).
type DeniedError struct {
	Right             Right
	Label, Plural     string
	OwnOnly, TeamOnly bool
}

func (e *DeniedError) Error() string {
	switch {
	case e.OwnOnly:
		return fmt.Sprintf("You can only %s %s you created.", verb(e.Right), e.Plural)
	case e.TeamOnly:
		return fmt.Sprintf("You can only %s your own %s and those of people who report to you.", verb(e.Right), e.Plural)
	}
	return fmt.Sprintf("You don't have permission to %s %s. Ask your administrator for a role that allows it.", verb(e.Right), e.Label)
}
func (e *DeniedError) Is(target error) bool              { return target == ErrForbidden }
func (e *DeniedError) Status() int                       { return 403 }
func (e *DeniedError) FieldErrors() []doctype.FieldError { return nil }

// ValueError: a proposed Project or Site is outside the user's restrictions.
// Allowed holds the codes the user may use.
type ValueError struct {
	Dim     Dimension
	Allowed []string
}

func (e *ValueError) Error() string {
	noun, plural := "Site", "sites"
	if e.Dim == DimProject {
		noun, plural = "Project", "projects"
	}
	if len(e.Allowed) != 1 {
		noun += "s"
	}
	return fmt.Sprintf("You can only use %s %s. Choose one of your allowed %s.", noun, strings.Join(e.Allowed, ", "), plural)
}
func (e *ValueError) Is(target error) bool              { return target == ErrForbidden }
func (e *ValueError) Status() int                       { return 403 }
func (e *ValueError) FieldErrors() []doctype.FieldError { return nil }

// LinkError: a linked record isn't visible to the user.
type LinkError struct {
	Label, Display string
}

func (e *LinkError) Error() string {
	return fmt.Sprintf("You can't use %s %s. It isn't available to you.", e.Label, e.Display)
}
func (e *LinkError) Is(target error) bool              { return target == ErrForbidden }
func (e *LinkError) Status() int                       { return 403 }
func (e *LinkError) FieldErrors() []doctype.FieldError { return nil }

// ImportRowMessage is the denial text for one import row.
func ImportRowMessage(row int, r Right, label string) string {
	return fmt.Sprintf("Row %d: you don't have permission to %s %s.", row, verb(r), label)
}
