package fixturetest_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// WP3 Task 1 (CD-182): the allow-all AUTH double still refuses what no grant
// can allow: inapplicable rights, an unknown dimension, Forbidden.
func TestContract_FixturetestAuthRefusals(t *testing.T) {
	authtest.RunRefusals(t, fixturetest.NewAuth())
}
