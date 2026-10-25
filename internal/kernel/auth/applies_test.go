package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// CD-171: a right not in AllRights never applies, so the service refuses it
// for everyone, Administrator included.
func TestApplies_UnknownRightFailsClosed(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	for _, dt := range doctype.All() {
		if auth.Applies(dt, "foo") {
			t.Errorf("Applies(%s, foo) = true, want false", dt.Name)
		}
	}

	admin := w.P(t, "admin@vardhan.test")
	for _, r := range []auth.Right{"foo", "Approve"} {
		var de *auth.DeniedError
		err := w.Auth.Can(ctx, admin, authtest.DTOrder, r, nil)
		if !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) {
			t.Errorf("Can(admin, %s, %q) = %v, want a *DeniedError (ErrForbidden)", authtest.DTOrder, r, err)
		}
	}
	var de *auth.DeniedError
	if _, err := w.Auth.Scope(ctx, admin, authtest.DTOrder, "foo"); !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Scope(admin, %s, foo) = %v, want a *DeniedError (ErrForbidden)", authtest.DTOrder, err)
	}
	users, err := w.Auth.UsersWithRight(ctx, authtest.DTOrder, "foo", nil)
	if err != nil || len(users) != 0 {
		t.Errorf("UsersWithRight(%s, foo) = %v, %v, want none", authtest.DTOrder, users, err)
	}

	if err := w.Auth.Can(ctx, admin, authtest.DTOrder, auth.RightApprove, nil); err != nil {
		t.Errorf("Can(admin, %s, approve) = %v, want nil", authtest.DTOrder, err)
	}
	if _, err := w.Auth.Scope(ctx, admin, authtest.DTOrder, auth.RightRead); err != nil {
		t.Errorf("Scope(admin, %s, read) = %v, want nil", authtest.DTOrder, err)
	}
}
