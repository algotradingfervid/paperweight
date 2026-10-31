package approval_test

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

// WaitingDays exports My requests' rule for other modules (CD-280 D4, CD-254):
// IST calendar days from submit while pending and at least OverdueAfterDays,
// else 0.
func TestWaitingDays(t *testing.T) {
	e := at.New(t)
	// Submitted 23:30 IST on 24 Oct (UTC date 24 Oct).
	submitted := time.Date(2026, 10, 24, 18, 0, 0, 0, time.UTC)
	pending := approval.Request{SubmittedAt: submitted}
	decided := approval.Request{SubmittedAt: submitted, Outcome: approval.OutcomeApproved}

	// 23:59 IST on 26 Oct: day 2.
	e.Clock.Set(time.Date(2026, 10, 26, 18, 29, 0, 0, time.UTC))
	if got := e.A.WaitingDays(pending); got != 0 {
		t.Errorf("pending on day 2: WaitingDays = %d, want 0", got)
	}
	// 00:01 IST on 27 Oct: day 3 by the IST calendar, UTC date still 26 Oct.
	e.Clock.Set(time.Date(2026, 10, 26, 18, 31, 0, 0, time.UTC))
	if got := e.A.WaitingDays(pending); got != 3 {
		t.Errorf("pending on day 3: WaitingDays = %d, want 3", got)
	}
	if got := e.A.WaitingDays(decided); got != 0 {
		t.Errorf("decided on day 3: WaitingDays = %d, want 0", got)
	}
}
