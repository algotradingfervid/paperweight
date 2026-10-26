package auth_test

import (
	"context"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// userService is the part of auth.Service the CD-189 tests drive.
type userService interface {
	User(ctx context.Context, tx *db.Tx, u auth.UserID) (auth.UserInfo, bool, error)
	DisableUser(ctx context.Context, tx *db.Tx, u auth.UserID, reason string) error
	EnableUser(ctx context.Context, tx *db.Tx, u auth.UserID) error
}

// userVersion reads u's Version through svc.User in a read transaction.
func userVersion(t *testing.T, w *authtest.World, svc userService, u auth.UserID) int {
	t.Helper()
	ctx := context.Background()
	var info auth.UserInfo
	var ok bool
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		info, ok, err = svc.User(ctx, tx, u)
		return err
	}); err != nil || !ok {
		t.Fatalf("User(%d) = ok %v, err %v; want the user", u, ok, err)
	}
	return info.Version
}

// inWrite runs fn in a write transaction, failing t on error.
func inWrite(t *testing.T, w *authtest.World, what string, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.DB.Write(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// CD-189: User().Version is users.version: it starts at 1, stays across
// reads and moves on every enable, disable and admin edit of the user.
func TestService_UserVersionMoves(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")
	u, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: "kiran@vardhan.test", Name: "Kiran Joshi", Password: t17Pass})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	v := userVersion(t, w, w.Auth, u)
	if v != 1 {
		t.Errorf("Version of a new user = %d, want 1", v)
	}
	if again := userVersion(t, w, w.Auth, u); again != v {
		t.Errorf("Version after a second read = %d, want %d (a read doesn't move it)", again, v)
	}
	name, enabled, roles, projects := "Kiran J", false, []string{auth.RoleEmployee, "Clerk"}, []int64{w.Projects["PRJ-METRO"]}
	w.Grant(t, "Clerk", authtest.DTOrder, auth.RightRead, auth.ScopeAll)
	steps := []struct {
		name string
		do   func(t *testing.T)
	}{
		{"DisableUser", func(t *testing.T) {
			inWrite(t, w, "DisableUser", func(ctx context.Context, tx *db.Tx) error { return w.Auth.DisableUser(ctx, tx, u, "left") })
		}},
		{"EnableUser", func(t *testing.T) {
			inWrite(t, w, "EnableUser", func(ctx context.Context, tx *db.Tx) error { return w.Auth.EnableUser(ctx, tx, u) })
		}},
		{"UpdateUser name", func(t *testing.T) {
			if _, err := w.Auth.UpdateUser(ctx, admin, u, auth.UserPatch{Name: &name}); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
		}},
		{"UpdateUser roles", func(t *testing.T) {
			if _, err := w.Auth.UpdateUser(ctx, admin, u, auth.UserPatch{Roles: &roles}); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
		}},
		{"UpdateUser projects", func(t *testing.T) {
			if _, err := w.Auth.UpdateUser(ctx, admin, u, auth.UserPatch{Projects: &projects}); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
		}},
		{"ChangeRestrictions", func(t *testing.T) {
			if err := w.Auth.ChangeRestrictions(ctx, admin, u, auth.DimSite, []int64{w.Sites["PUNE-WH"]}, nil, nil); err != nil {
				t.Fatalf("ChangeRestrictions: %v", err)
			}
		}},
		{"UpdateUser disable", func(t *testing.T) {
			if _, err := w.Auth.UpdateUser(ctx, admin, u, auth.UserPatch{Enabled: &enabled}); err != nil {
				t.Fatalf("UpdateUser: %v", err)
			}
		}},
	}
	for _, s := range steps {
		s.do(t)
		got := userVersion(t, w, w.Auth, u)
		if got != v+1 {
			t.Errorf("Version after %s = %d, want %d", s.name, got, v+1)
		}
		v = got
	}
}

// CD-189 parity: the same add, disable, enable sequence gives the same
// Versions on the real service and on authfake.
func TestAuthfake_UserVersionMatchesReal(t *testing.T) {
	const email = "kiran@vardhan.test"
	w := authtest.NewWorld(t)
	u := w.AddUser(t, email, "Kiran Joshi")
	f := authfake.New()
	f.AddUser(auth.UserInfo{ID: u, Email: email, Name: "Kiran Joshi", Enabled: true})

	for _, side := range []struct {
		name string
		svc  userService
	}{{"real", w.Auth}, {"authfake", f}} {
		got := []int{userVersion(t, w, side.svc, u)}
		inWrite(t, w, side.name+" DisableUser", func(ctx context.Context, tx *db.Tx) error { return side.svc.DisableUser(ctx, tx, u, "left") })
		got = append(got, userVersion(t, w, side.svc, u))
		inWrite(t, w, side.name+" EnableUser", func(ctx context.Context, tx *db.Tx) error { return side.svc.EnableUser(ctx, tx, u) })
		got = append(got, userVersion(t, w, side.svc, u))
		if want := []int{1, 2, 3}; !slices.Equal(got, want) {
			t.Errorf("%s Versions after add, disable, enable = %v, want %v", side.name, got, want)
		}
	}
}
