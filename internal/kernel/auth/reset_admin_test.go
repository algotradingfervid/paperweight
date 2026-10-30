package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// CD-228, CD-231: the command-line recovery. The refusal is its own message and
// changes nothing; a reset works with the account locked.
func TestUser_ResetAdminPassword(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	nisha := w.AddUser(t, t7Nisha, "Nisha Menon", "Employee")
	const temp = "Copper-Lantern-8823"

	if err := w.Auth.ResetAdminPassword(ctx, t7Nisha, temp); !errors.Is(err, auth.ErrResetNotAdmin) {
		t.Errorf("reset of an Employee = %v, want auth.ErrResetNotAdmin", err)
	}
	if u, ok, err := userRow(ctx, w, nisha); err != nil || !ok || u {
		t.Errorf("Nisha after the refusal: must-change %v, found %v, err %v, want unchanged", u, ok, err)
	}
	if err := w.Auth.ResetAdminPassword(ctx, "nobody@vardhan.test", temp); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("reset of an unknown email = %v, want auth.ErrNotFound", err)
	}
	if err := w.Auth.ResetAdminPassword(ctx, "  "+t7Admin+"  ", "short"); !errors.Is(err, auth.ErrPasswordTooShort) {
		t.Errorf("reset with a short password = %v, want auth.ErrPasswordTooShort", err)
	}

	if err := w.Auth.ResetAdminPassword(ctx, "  "+t7Admin+"  ", temp); err != nil {
		t.Fatalf("reset: %v", err)
	}
	res, err := w.Auth.SignIn(ctx, auth.SignInInput{Email: t7Admin, Password: temp, IP: "127.0.0.1"})
	if err != nil || !res.MustChangePassword {
		t.Errorf("sign-in = %+v, %v, want must-change", res, err)
	}
}

func userRow(ctx context.Context, w *authtest.World, u auth.UserID) (mustChange, found bool, err error) {
	var n int
	err = w.DB.SQL().QueryRowContext(ctx, `SELECT must_change_password FROM users WHERE id = ?`, u).Scan(&n)
	return n == 1, err == nil, err
}
