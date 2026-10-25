package cli_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/cli"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Once users exist, AdminCreate is refused with the users-exist text even
// when the password would fail the policy.
func TestAdminCreate_UsersExistBeforePolicy(t *testing.T) {
	ctx := context.Background()
	d := testkit.NewDB(t)
	a := auth.New(auth.Deps{DB: d, Clock: clock.NewFixed(authtest.T0), Directory: authfake.NewDirectory(), Audit: auditfake.New()})
	if _, err := a.CreateFirstAdmin(ctx, "admin@vardhan.test", "Anil Kapoor", "Correct-Horse-42"); err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}

	var out bytes.Buffer
	args := []string{"--email", "meena@vardhan.test", "--name", "Meena Iyer"}
	err := cli.AdminCreate(ctx, a, args, strings.NewReader("short\nshort\n"), &out)
	if !errors.Is(err, auth.ErrUsersExist) {
		t.Fatalf("AdminCreate with a short password once users exist = %v, want auth.ErrUsersExist", err)
	}
	if err.Error() != t11UsersExist || !strings.Contains(out.String(), t11UsersExist) {
		t.Errorf("error %q, output %q, want %q", err.Error(), out.String(), t11UsersExist)
	}
}

// UserDisable disables a user, refuses the last enabled Administrator and
// reports an unknown email as not found.
func TestUserDisable(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)

	var out bytes.Buffer
	if err := cli.UserDisable(ctx, w.Auth, []string{"Priya@Vardhan.test"}, &out); err != nil {
		t.Fatalf("UserDisable priya: %v", err)
	}
	if want := "Disabled priya@vardhan.test.\n"; out.String() != want {
		t.Errorf("output %q, want %q", out.String(), want)
	}
	var enabled int
	if err := w.DB.SQL().QueryRowContext(ctx, `SELECT enabled FROM users WHERE email = 'priya@vardhan.test'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Errorf("priya enabled = %d, want 0", enabled)
	}

	err := cli.UserDisable(ctx, w.Auth, []string{"admin@vardhan.test"}, &out)
	if !errors.Is(err, auth.ErrLastAdmin) || err.Error() != auth.ErrLastAdmin.Msg {
		t.Errorf("UserDisable last admin = %v, want %q", err, auth.ErrLastAdmin.Msg)
	}
	if err := w.DB.SQL().QueryRowContext(ctx, `SELECT enabled FROM users WHERE email = 'admin@vardhan.test'`).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Errorf("admin enabled = %d, want 1", enabled)
	}

	err = cli.UserDisable(ctx, w.Auth, []string{"nobody@vardhan.test"}, &out)
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("UserDisable unknown email = %v, want auth.ErrNotFound", err)
	}
}
