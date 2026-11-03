package auth_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const (
	r3Admin = "admin@vardhan.test"
	r3Priya = "priya@vardhan.test"
	r3Old   = "old@vardhan.test"
)

// r3Fail fails email's sign-in the given number of times.
func r3Fail(t *testing.T, w *authtest.World, email string, failures int) {
	t.Helper()
	w.SetPassword(t, email, "Correct-Horse-42")
	for range failures {
		if _, err := w.Auth.SignIn(context.Background(), auth.SignInInput{Email: email, Password: "Wrong-Horse-43", IP: "198.51.100.4", UserAgent: "r3"}); err == nil {
			t.Fatal("a wrong password signed in")
		}
	}
}

// UserAdmin reads what the user's page shows, with the version of those roles,
// and refuses what its siblings refuse.
func TestUserAdmin_Reads(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, r3Admin)
	v, err := w.Auth.UserAdmin(ctx, admin, w.Users[r3Priya])
	if err != nil {
		t.Fatal(err)
	}
	if v.User.ID != w.Users[r3Priya] || v.User.Email != r3Priya || v.User.Name != "Priya Shah" || !v.User.Enabled {
		t.Errorf("user = %+v", v.User)
	}
	_, version, _, err := w.Auth.UserForEdit(ctx, admin, w.Users[r3Priya])
	if err != nil || v.Version != version {
		t.Errorf("version = %d, UserForEdit says %d (%v)", v.Version, version, err)
	}
	if !reflect.DeepEqual(v.Held, []string{"Employee", "Purchase User"}) && !reflect.DeepEqual(v.Held, []string{"Purchase User", "Employee"}) {
		t.Errorf("held = %q", v.Held)
	}
	if !slices.Contains(v.Roles, auth.RoleAdministrator) || !slices.Contains(v.Roles, "Stores") {
		t.Errorf("roles = %q", v.Roles)
	}
	if v.Locked {
		t.Error("an account with no failures is locked")
	}
	if off, err := w.Auth.UserAdmin(ctx, admin, w.Users[r3Old]); err != nil || off.User.Enabled {
		t.Errorf("disabled user = %+v, %v", off.User, err)
	}
}

func TestUserAdmin_Refusals(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	if _, err := w.Auth.UserAdmin(ctx, w.P(t, r3Priya), w.Users[r3Priya]); !errors.Is(err, auth.ErrAdminOnlyUsers) {
		t.Errorf("non-admin on their own record: %v", err)
	}
	if _, err := w.Auth.UserAdmin(ctx, w.P(t, r3Admin), 999999); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
}

// Locked follows the limit and the clock: one failure short isn't locked, the
// limit is, and the window's end frees it.
func TestUserAdmin_LockFollowsLimitAndClock(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	locked := func() bool {
		t.Helper()
		v, err := w.Auth.UserAdmin(ctx, w.P(t, r3Admin), w.Users[r3Priya])
		if err != nil {
			t.Fatal(err)
		}
		return v.Locked
	}
	r3Fail(t, w, r3Priya, auth.AccountLimit-1)
	if locked() {
		t.Error("locked one failure short of the limit")
	}
	r3Fail(t, w, r3Priya, 1)
	if !locked() {
		t.Error("not locked at the limit")
	}
	w.Clock.Advance(auth.LockWindow + time.Second)
	if locked() {
		t.Error("still locked after the window")
	}
}

func r3Must(t *testing.T, w *authtest.World, email string) int {
	t.Helper()
	ctx := context.Background()
	var n int
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT must_change_password FROM users WHERE email = ?`, email).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func r3Name(t *testing.T, w *authtest.World, email string) string {
	t.Helper()
	ctx := context.Background()
	var n string
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT name FROM users WHERE email = ?`, email).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

const r3Pw = "Correct-Horse-42"

// r3Empty is an Auth over a database with no users.
func r3Empty(t *testing.T) *auth.Auth {
	t.Helper()
	return auth.New(auth.Deps{DB: testkit.NewDB(t), Clock: clock.NewFixed(authtest.T0), Directory: authfake.NewDirectory(), Audit: auditfake.New()})
}

// A blank or spaces-only name is refused with its own text; a name is stored trimmed.
func TestCreateUser_Name(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, r3Admin)
	for _, name := range []string{"", "   ", "\t "} {
		_, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: "kiran@vardhan.test", Name: name, Password: r3Pw})
		if !errors.Is(err, auth.ErrNameRequired) {
			t.Errorf("name %q: %v", name, err)
		}
	}
	if _, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: "kiran@vardhan.test", Name: "  Kiran Joshi ", Password: r3Pw}); err != nil {
		t.Fatal(err)
	}
	if got := r3Name(t, w, "kiran@vardhan.test"); got != "Kiran Joshi" {
		t.Errorf("stored %q", got)
	}
}

// The same on an update, after the version check: a stale page gets its 409.
func TestUpdateUser_Name(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, r3Admin)
	id := w.Users[r3Priya]
	_, version, _, err := w.Auth.UserForEdit(ctx, admin, id)
	if err != nil {
		t.Fatal(err)
	}
	blank := "   "
	if _, err := w.Auth.UpdateUser(ctx, admin, id, auth.UserPatch{Name: &blank, Version: &version}); !errors.Is(err, auth.ErrNameRequired) {
		t.Errorf("blank: %v", err)
	}
	old := version - 1
	if _, err := w.Auth.UpdateUser(ctx, admin, id, auth.UserPatch{Name: &blank, Version: &old}); !auth.IsChanged(err) {
		t.Errorf("stale and blank: %v, want the stale refusal", err)
	}
	if got := r3Name(t, w, r3Priya); got != "Priya Shah" {
		t.Errorf("name = %q", got)
	}
	spaced := " Priya S "
	if _, err := w.Auth.UpdateUser(ctx, admin, id, auth.UserPatch{Name: &spaced, Version: &version}); err != nil {
		t.Fatal(err)
	}
	if got := r3Name(t, w, r3Priya); got != "Priya S" {
		t.Errorf("name = %q", got)
	}
}

// MustChange reaches the row, and without it the flag stays off.
func TestCreateUser_MustChange(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, r3Admin)
	for email, must := range map[string]bool{"temp@vardhan.test": true, "kept@vardhan.test": false} {
		if _, _, err := w.Auth.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: "K J", Password: r3Pw, MustChange: must}); err != nil {
			t.Fatal(err)
		}
		want := 0
		if must {
			want = 1
		}
		if got := r3Must(t, w, email); got != want {
			t.Errorf("%s: must change = %d, want %d", email, got, want)
		}
	}
}

// The first administrator's name is trimmed, and a blank one is refused before
// any write.
func TestCreateFirstAdmin_Name(t *testing.T) {
	ctx := context.Background()
	for _, name := range []string{"", "   "} {
		w := r3Empty(t)
		if _, err := w.CreateFirstAdmin(ctx, "admin@vardhan.test", name, r3Pw); !errors.Is(err, auth.ErrNameRequired) {
			t.Errorf("name %q: %v", name, err)
		}
		if done, _ := w.SetupDone(ctx); done {
			t.Errorf("name %q: a user was created", name)
		}
	}
	a := r3Empty(t)
	if _, err := a.CreateFirstAdmin(ctx, "admin@vardhan.test", "  Anil Kapoor ", r3Pw); err != nil {
		t.Fatal(err)
	}
	v, err := a.UserAdmin(ctx, auth.Principal{ID: 1, Roles: []string{auth.RoleAdministrator}}, 1)
	if err != nil || v.User.Name != "Anil Kapoor" {
		t.Errorf("stored %+v, %v", v.User, err)
	}
}
