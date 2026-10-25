package approval_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

func TestApproval_Withdraw_LeavesInbox(t *testing.T) { // APR-093
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	for _, p := range []struct {
		name  string
		inbox []string
	}{{"Ravi", e.InboxTitles(t, e.Ravi)}, {"Meena", e.InboxTitles(t, e.Meena)}} {
		if !slices.Equal(p.inbox, []string{"Draft 42"}) {
			t.Fatalf("before withdraw %s's inbox = %q, want [Draft 42]", p.name, p.inbox)
		}
	}
	withdrawnAt := time.Date(2026, 10, 24, 5, 30, 0, 0, time.UTC)
	e.Clock.Set(withdrawnAt)
	at.MustOK(t, e.Withdraw(e.Priya, d))
	if got := e.InboxTitles(t, e.Ravi); len(got) != 0 {
		t.Errorf("Ravi's inbox = %q, want empty", got)
	}
	if got := e.InboxTitles(t, e.Meena); len(got) != 0 {
		t.Errorf("Meena's inbox = %q, want empty", got)
	}
	res, err := e.A.MyRequests(t.Context(), e.Priya, types.Page{Number: 1, Size: 25})
	at.MustOK(t, err)
	if res.Total != 1 || len(res.Rows) != 1 {
		t.Fatalf("Priya's requests = %+v, want one row", res)
	}
	r := res.Rows[0]
	if r.Title != "Draft 42" || r.Outcome != approval.OutcomeWithdrawn || r.DecidedByName != "Priya Shah" ||
		!r.DecidedAt.Equal(withdrawnAt) || r.State != string(doctype.StateDraft) {
		t.Errorf("Priya's request = %+v, want Draft 42 withdrawn by Priya Shah at %s, state draft", r, withdrawnAt)
	}
}

func TestApproval_Withdraw_ResubmitNewRequest(t *testing.T) { // APR-096
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	first, ok := e.Latest(t, d)
	if !ok || first.RuleID != e.R1 {
		t.Fatalf("first request %v %+v, want open under R1 (%d)", ok, first, e.R1)
	}
	at.MustOK(t, e.Withdraw(e.Priya, d))
	e.SetAmount(t, d, at.Rs(550000))
	at.MustOK(t, e.Submit(e.Priya, d))
	now, ok := e.Latest(t, d)
	if !ok || now.ID == first.ID || now.RuleID != e.R2 || now.Outcome != "" || now.Amount != at.Rs(550000) {
		t.Errorf("latest request %v %+v, want a new open request under R2 (%d) for 55000000 paise", ok, now, e.R2)
	}
	if old := e.RequestByID(t, first.ID); old.Outcome != approval.OutcomeWithdrawn {
		t.Errorf("first request outcome %q, want withdrawn", old.Outcome)
	}
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
	at.WantErr(t, e.Approve(e.Ravi, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 42 (₹5,50,000.00).")
	at.MustOK(t, e.Approve(e.Meena, d))
	if e.State(t, d) != doctype.StateApproved {
		t.Errorf("state %s after Meena's approval, want approved", e.State(t, d))
	}
}
