package partyweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/masters/party"
)

// No case ID: UI v2 C5 review. role is the stored columns as the form's
// choice, and none when neither is set: the form never pre-selects a role
// the party doesn't have.
func TestRole_FromColumns(t *testing.T) {
	for _, c := range []struct {
		vendor, customer bool
		want             string
	}{
		{true, false, roleVendor},
		{false, true, roleCustomer},
		{true, true, roleBoth},
		{false, false, ""},
	} {
		if got := role(party.Party{IsVendor: c.vendor, IsCustomer: c.customer}); got != c.want {
			t.Errorf("role(vendor=%v, customer=%v) = %q; want %q", c.vendor, c.customer, got, c.want)
		}
	}
}
