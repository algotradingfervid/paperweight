package auth_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// F3 (CD-381): only a stale version is IsChanged, wrapped too; it stays a
// Problem to errors.As, and auth's other 409s aren't stale.
func TestIsChanged(t *testing.T) {
	for _, err := range []error{auth.RoleChangedError("Buyer"), auth.UserChangedError("Priya"), fmt.Errorf("saving: %w", auth.RoleChangedError("Buyer"))} {
		var p *auth.Problem
		if !auth.IsChanged(err) || !errors.As(err, &p) || p.Status() != 409 {
			t.Errorf("%v: IsChanged %v, Problem %+v; want stale, a 409 Problem", err, auth.IsChanged(err), p)
		}
	}
	if auth.IsChanged(auth.ErrLastAdmin) {
		t.Error("ErrLastAdmin is a 409 refusal, not a stale version")
	}
}
