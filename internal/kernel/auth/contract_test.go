package auth_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// realSetup is authtest.Setup through the real admin API: CreateRole,
// CreateUser, SetRights and ChangeRestrictions, acting as the World's
// Administrator.
type realSetup struct {
	w     *authtest.World
	roles map[string]bool // roles this setup created
}

func (s *realSetup) Admin(t *testing.T) auth.Principal {
	t.Helper()
	return s.load(t, s.w.Users["admin@vardhan.test"])
}

func (s *realSetup) load(t *testing.T, u auth.UserID) auth.Principal {
	t.Helper()
	p, _, err := s.w.Auth.LoadPrincipal(context.Background(), u)
	if err != nil {
		t.Fatalf("LoadPrincipal(%d): %v", u, err)
	}
	return p
}

func (s *realSetup) role(t *testing.T, role string) {
	t.Helper()
	if s.roles[role] {
		return
	}
	if err := s.w.Auth.CreateRole(context.Background(), s.Admin(t), role); err != nil {
		t.Fatalf("CreateRole(%q): %v", role, err)
	}
	s.roles[role] = true
}

func (s *realSetup) User(t *testing.T, email string, roles ...string) auth.Principal {
	t.Helper()
	for _, r := range roles {
		s.role(t, r)
	}
	if roles == nil {
		roles = []string{} // nil would mean [Employee]
	}
	u, _, err := s.w.Auth.CreateUser(context.Background(), s.Admin(t), auth.NewUser{
		Email: email, Name: email, Password: "Correct-Horse-42", Roles: roles,
	})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", email, err)
	}
	return s.load(t, u)
}

func (s *realSetup) Grant(t *testing.T, role, doctype string, r auth.Right, sc auth.Scope) {
	t.Helper()
	ctx := context.Background()
	s.role(t, role)
	rights, err := s.w.Auth.RoleRights(ctx, role, doctype)
	if err != nil {
		t.Fatalf("RoleRights(%q, %s): %v", role, doctype, err)
	}
	if rights == nil {
		rights = map[auth.Right]auth.Scope{}
	}
	rights[r] = sc
	if err := s.w.Auth.SetRights(ctx, s.Admin(t), role, doctype, rights); err != nil {
		t.Fatalf("SetRights(%q, %s, %v): %v", role, doctype, rights, err)
	}
}

func (s *realSetup) Restrict(t *testing.T, u auth.UserID, projects, sites []int64) {
	t.Helper()
	for _, d := range []struct {
		dim auth.Dimension
		ids []int64
	}{{auth.DimProject, projects}, {auth.DimSite, sites}} {
		if d.ids == nil {
			continue
		}
		if err := s.w.Auth.ChangeRestrictions(context.Background(), s.Admin(t), u, d.dim, d.ids, nil, nil); err != nil {
			t.Fatalf("ChangeRestrictions(%d, %s, %v): %v", u, d.dim, d.ids, err)
		}
	}
}

func (s *realSetup) DB() *db.DB { return s.w.DB }

func (s *realSetup) Directory() *authfake.Directory { return s.w.Dir }

// WP3 Task 1 (CD-182): the real service passes the auth contract suite.
func TestContract_Auth(t *testing.T) {
	authtest.Run(t, func(t *testing.T) (auth.Service, authtest.Setup) {
		w := authtest.NewWorld(t)
		return w.Auth, &realSetup{w: w, roles: map[string]bool{}}
	})
	w := authtest.NewWorld(t)
	authtest.RunUnknownDocType(t, w.Auth, (&realSetup{w: w}).Admin(t))
}
