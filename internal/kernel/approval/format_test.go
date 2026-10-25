package approval

import (
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

func TestFormat_RolesStatesAndStorage(t *testing.T) {
	if got := joinRoles([]string{"Purchase Manager"}); got != "Purchase Manager" {
		t.Error(got)
	}
	if got := joinRoles([]string{"Purchase Manager", "Director"}); got != "Purchase Manager or Director" {
		t.Error(got)
	}
	if got := joinRoles([]string{"A", "B", "C"}); got != "A, B or C" {
		t.Error(got)
	}
	for s, want := range map[doctype.State]string{
		doctype.StateDraft:     "Draft",
		doctype.StatePending:   "Waiting for approval",
		doctype.StateApproved:  "Approved",
		doctype.StateRejected:  "Rejected",
		doctype.StateCancelled: "Cancelled",
		doctype.StateClosed:    "Closed",
	} {
		if got := stateLabel(s); got != want {
			t.Errorf("stateLabel(%q) = %q, want %q", s, got, want)
		}
	}
	at := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if got := ts(at); got != "2026-10-24T05:00:00.000Z" {
		t.Error(got)
	}
	ist := time.FixedZone("IST", 5*3600+1800)
	if got := ts(at.In(ist)); got != "2026-10-24T05:00:00.000Z" {
		t.Errorf("ts of an IST time = %q, want UTC storage text", got)
	}
	if back, err := parseTS("2026-10-24T05:00:00.000Z"); err != nil || !back.Equal(at) {
		t.Errorf("parseTS = %v, %v; want %v", back, err, at)
	}
	// money and IST display are NUM's (types.FormatINR, types.FormatIST), real on main (CD-53) and asserted through the messages below
	err := notEligible("Draft 42", 6000000, "", []string{"Purchase Manager", "Director"})
	if err.Error() != "Only a user with the Purchase Manager or Director role can approve Draft 42 (₹60,000.00)." {
		t.Error(err)
	}
	var h doctype.HTTPError
	if !errors.As(err, &h) || h.Status() != 403 || h.FieldErrors() != nil {
		t.Errorf("HTTPError %v", h) // CD-44
	}
	if !errors.Is(err, ErrNotEligible) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("notEligible must wrap ErrNotEligible and auth.ErrForbidden: %v", err)
	}
	err = notEligible("Draft 10", 250000, "Priya Shah", []string{"Director"})
	if err.Error() != "Only Priya Shah or a user with the Director role can approve Draft 10 (₹2,500.00)." {
		t.Error(err)
	}
}
