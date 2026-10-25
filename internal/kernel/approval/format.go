package approval

import (
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

// joinRoles joins role names for messages: "A", "A or B", "A, B or C".
func joinRoles(roles []string) string {
	if len(roles) < 2 {
		return strings.Join(roles, "")
	}
	return strings.Join(roles[:len(roles)-1], ", ") + " or " + roles[len(roles)-1]
}

// stateLabel is the plain-English name of a document state.
func stateLabel(s doctype.State) string {
	switch s {
	case doctype.StateDraft:
		return "Draft"
	case doctype.StatePending:
		return "Waiting for approval"
	case doctype.StateApproved:
		return "Approved"
	case doctype.StateRejected:
		return "Rejected"
	case doctype.StateCancelled:
		return "Cancelled"
	case doctype.StateClosed:
		return "Closed"
	}
	return string(s)
}

// ts is the storage text of t (types.Timestamp's stored form). It is never
// shown: every display of time is types.FormatIST (CD-33).
func ts(t time.Time) string { return types.TimestampOf(t).String() }

// parseTS reads storage text written by ts.
func parseTS(s string) (time.Time, error) {
	v, err := types.ParseTimestamp(s)
	return v.Time(), err
}
