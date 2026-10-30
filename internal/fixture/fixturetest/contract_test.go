package fixturetest_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// WP3 Task 1 (CD-182): the allow-all AUTH double still refuses what no grant
// can allow: inapplicable rights, an unknown dimension, Forbidden. It also
// finds users by email like the real service (CD-199).
func TestContract_FixturetestAuthRefusals(t *testing.T) {
	a := fixturetest.NewAuth()
	a.Users[99] = auth.UserInfo{ID: 99, Enabled: true} // a blank email: UserByEmail("") must not find it
	next := auth.UserID(100)
	authtest.RunRefusals(t, a, func(email string) auth.Principal {
		next++
		a.Users[next] = auth.UserInfo{ID: next, Email: email, Name: email, Enabled: true, Version: 1}
		return auth.Principal{ID: next, Name: email}
	})
}
