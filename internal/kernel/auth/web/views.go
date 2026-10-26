package authweb

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Views renders AUTH's pages; views.Default is the plain templ set. Every
// page struct carries CSRF, Error and Warnings plus its own fields.
type Views interface {
	Message(MessagePage) templ.Component
	Login(LoginPage) templ.Component
	Setup(SetupPage) templ.Component
	Password(PasswordPage) templ.Component
	Sessions(SessionsPage) templ.Component
	Users(UsersPage) templ.Component
	User(UserPage) templ.Component
	Roles(RolesPage) templ.Component
	Matrix(MatrixPage) templ.Component
}

// MessagePage shows one message.
type MessagePage struct {
	CSRF, Error    string
	Warnings       []string
	Title, Message string
}

// LoginPage is the sign-in form.
type LoginPage struct {
	CSRF, Error string
	Warnings    []string
	Email, Next string
}

// SetupPage is the first-administrator form.
type SetupPage struct {
	CSRF, Error string
	Warnings    []string
	Email, Name string
}

// PasswordPage is the change-password form.
type PasswordPage struct {
	CSRF, Error string
	Warnings    []string
	Forced      bool // the user must change their password
}

// SessionsPage lists the user's sessions.
type SessionsPage struct {
	CSRF, Error string
	Warnings    []string
	Sessions    []auth.SessionInfo
}

// UsersPage lists the users.
type UsersPage struct {
	CSRF, Error string
	Warnings    []string
	Users       []auth.UserInfo
}

// UserPage shows one user. Version is the user's version, posted back as the
// hidden version field (CD-94). AccessURL, set for an Administrator, links
// the user's "Site and project access" page.
type UserPage struct {
	CSRF, Error string
	Warnings    []string
	User        auth.UserInfo
	Version     int64
	AccessURL   string
}

// RolesPage lists the roles and shows the rights matrix of every DocType.
type RolesPage struct {
	CSRF     string
	Warnings []string
	Roles    []auth.RoleInfo
	Matrices []MatrixPage
}

// MatrixPage is one DocType's rights matrix. Rights are the rights that
// apply to the DocType; Scopes is auth.ScopeOptions(DocType), so Team is
// offered only where the DocType has an employee field (CD-11). Each right
// shows None plus Scopes.
type MatrixPage struct {
	CSRF           string
	DocType, Label string
	Rights         []auth.Right
	Scopes         []auth.Scope
	Roles          []MatrixRow
}

// MatrixRow is one role's rights on the DocType (absent = none); its forms
// post Role.Version. The Administrator row is fixed.
type MatrixRow struct {
	Role   auth.RoleInfo
	Rights map[auth.Right]auth.Scope
}
