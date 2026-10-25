package authfake_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// fakeSetup is authtest.Setup through authfake's seeding helpers (CD-56).
// The probe tables come from an authtest World, whose Auth is not used.
type fakeSetup struct {
	f     *authfake.Fake
	d     *db.DB
	next  auth.UserID
	admin *auth.Principal
}

// Admin is an enabled user holding Administrator, which the fake models as
// the "*" grant of every right (CD-86).
func (s *fakeSetup) Admin(t *testing.T) auth.Principal {
	t.Helper()
	if s.admin == nil {
		p := s.User(t, "admin@contract.test", auth.RoleAdministrator)
		for _, r := range auth.AllRights {
			s.f.Grant(auth.RoleAdministrator, "*", r, auth.ScopeAll)
		}
		s.admin = &p
	}
	return *s.admin
}

func (s *fakeSetup) User(t *testing.T, email string, roles ...string) auth.Principal {
	t.Helper()
	s.next++
	u := s.next
	s.f.AddUser(auth.UserInfo{ID: u, Email: email, Name: email, Enabled: true})
	for _, r := range roles {
		s.f.AddRole(u, r)
	}
	return auth.Principal{ID: u, Name: email, Roles: append([]string(nil), roles...)}
}

func (s *fakeSetup) Grant(t *testing.T, role, doctype string, r auth.Right, sc auth.Scope) {
	t.Helper()
	s.f.Grant(role, doctype, r, sc)
}

func (s *fakeSetup) Restrict(t *testing.T, u auth.UserID, projects, sites []int64) {
	t.Helper()
	s.f.Restrict(u, projects, sites)
}

func (s *fakeSetup) DB() *db.DB { return s.d }

// WP3 Task 1 (CD-182): authfake passes the same contract suite as the real
// service, so tests written against it stay trustworthy.
func TestContract_Authfake(t *testing.T) {
	authtest.Run(t, func(t *testing.T) (auth.Service, authtest.Setup) {
		f := authfake.New()
		return f, &fakeSetup{f: f, d: authtest.NewWorld(t).DB, next: 100}
	})
}
