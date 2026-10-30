package fixture_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
)

// ATT-001: fixture.Claim opts in to the Attachments section (CD-241, CD-243
// point 4); fixture.Order and fixture.Receipt don't.
func TestFixture_AttachableFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		want bool
	}{
		{fixture.Claim, true},
		{fixture.Order, false},
		{fixture.Receipt, false},
	} {
		if got := mustLookup(t, tc.name).Attachable; got != tc.want {
			t.Errorf("doctype.Lookup(%q).Attachable = %v, want %v", tc.name, got, tc.want)
		}
	}
}
