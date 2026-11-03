package authweb

import (
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Views renders AUTH's pages; views.Default is the plain templ set. Every
// page struct carries CSRF and Error plus its own fields.
type Views interface {
	Message(MessagePage) templ.Component
	Login(LoginPage) templ.Component
	Setup(SetupPage) templ.Component
	Password(PasswordPage) templ.Component
	Sessions(SessionsPage) templ.Component
	Users(UsersPage) templ.Component
	User(UserPage) templ.Component
	NewUser(NewUserPage) templ.Component
	ResetPassword(ResetPasswordPage) templ.Component
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
	NameError   bool // Error belongs to the name, not the email
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
// the user's "Site and project access" page. Admin shows the forms; Roles,
// Held and Locked are theirs. Self is the viewer's own page. Stale says Error
// is a stale version (CD-381).
type UserPage struct {
	CSRF, Error string
	Warnings    []string
	User        auth.UserInfo
	Version     int64
	AccessURL   string
	Admin, Self bool
	Roles, Held []string
	Locked      bool
	Stale       bool
}

// NewUserPage is the add-a-user form. Email, Name, Mobile and Held are what it
// shows; the password is never sent back. ErrorField names the field Error
// belongs to.
type NewUserPage struct {
	CSRF, Error, ErrorField string
	Email, Name, Mobile     string
	Roles, Held             []string
}

// ResetPasswordPage sets User's temporary password.
type ResetPasswordPage struct {
	CSRF, Error string
	User        auth.UserInfo
}
