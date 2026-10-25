package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// CD-157 (C): authfake's forbidden text is the real service's DeniedError
// text for the same right and DocType.
func TestAuthfake_DeniedTextMatchesReal(t *testing.T) {
	const clerk = "clerk@vardhan.test"
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.AddUser(t, clerk, "Clerk User")
	p := w.P(t, clerk)
	f := authfake.New()
	for _, r := range []auth.Right{auth.RightRead, auth.RightWrite} {
		t.Run(string(r), func(t *testing.T) {
			real, fake := w.Auth.Can(ctx, p, authtest.DTOrder, r, nil), f.Can(ctx, p, authtest.DTOrder, r, nil)
			var rd, fd *auth.DeniedError
			if !errors.As(real, &rd) || !errors.As(fake, &fd) {
				t.Fatalf("real = %v, authfake = %v; want *auth.DeniedError from both", real, fake)
			}
			if rd.Error() != fd.Error() {
				t.Errorf("authfake text = %q, real = %q", fd.Error(), rd.Error())
			}
		})
	}
}
