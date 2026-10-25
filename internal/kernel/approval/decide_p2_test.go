package approval_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

func TestApprovalRequest_DisabledRuleKeepsPending(t *testing.T) { // APR-026
	setup := func(t *testing.T) (*at.Env, at.Doc) {
		e, d := pending42(t)
		at.MustOK(t, e.A.SetRuleEnabled(t.Context(), e.Anil, e.R1, false))
		if e.State(t, d) != doctype.StatePending {
			t.Errorf("state %s after disabling R1, want pending_approval", e.State(t, d))
		}
		if r, ok := e.Latest(t, d); !ok || r.Outcome != "" || r.RuleID != e.R1 {
			t.Errorf("request %v %+v, want still open under R1 (%d)", ok, r, e.R1)
		}
		return e, d
	}
	e, d := setup(t)
	at.MustOK(t, e.Approve(e.Ravi, d))
	if e.State(t, d) != doctype.StateApproved {
		t.Errorf("state %s, want approved", e.State(t, d))
	}

	e2, d2 := setup(t)
	at.MustOK(t, e2.Reject(e2.Ravi, d2, "Wrong vendor"))
	if e2.State(t, d2) != doctype.StateRejected {
		t.Errorf("state %s, want rejected", e2.State(t, d2))
	}
}

func TestApprovalApprove_RefusalIsAnError(t *testing.T) { // APR-031
	e, d := pending42(t)
	if err := e.Approve(e.Priya, d); !errors.Is(err, approval.ErrNotEligible) {
		t.Errorf("Priya's approve: error %v, want ErrNotEligible", err)
	}
	if r, ok := e.Latest(t, d); !ok || r.Outcome != "" {
		t.Errorf("request %v %+v, want still open", ok, r)
	}
	if e.State(t, d) != doctype.StatePending || len(e.History(t, d)) != 1 {
		t.Errorf("state %s, history rows %d; want pending_approval, 1 (submitted)", e.State(t, d), len(e.History(t, d)))
	}
}

func TestApprovalSelf_AllowedByRule(t *testing.T) { // APR-038
	e := at.New(t)
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R2, 1, approval.RuleInput{
		DocType: at.Order, FromAmount: at.Rs(5_00_000), ApproverType: approval.ApproverRole, Role: "Director", AllowSelfApproval: true,
	})
	at.MustOK(t, err)
	e.Auth.AddRole(e.Meena.ID, "Purchase User") // so she can create and submit; she stays the only Director
	d := e.NewDoc(t, at.Order, 50, at.Rs(6_00_000), e.Meena)
	at.MustOK(t, e.Submit(e.Meena, d))
	if e.State(t, d) != doctype.StatePending || e.Number(t, d) != "" {
		t.Errorf("after submit: state %s number %q, want pending_approval with no number", e.State(t, d), e.Number(t, d))
	}
	at.MustOK(t, e.Approve(e.Meena, d))
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	if last := lastHist(t, e, d); last.Action != approval.HistApproved || last.By != e.Meena.ID || !last.SelfApproved {
		t.Errorf("last history %+v, want approved by Meena (%d) and self-approved", last, e.Meena.ID)
	}
}

func TestApprovalExpense_FallbackRoleCanApprove(t *testing.T) { // APR-046
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	if r, ok := e.Latest(t, d); !ok || r.ManagerUser != e.Priya.ID {
		t.Fatalf("request %v %+v, want designated to Priya (%d)", ok, r, e.Priya.ID)
	}
	at.MustOK(t, e.Approve(e.Meena, d))
	if e.State(t, d) != doctype.StateApproved {
		t.Errorf("state %s, want approved", e.State(t, d))
	}
	if r, _ := e.Latest(t, d); r.Outcome != approval.OutcomeApproved || r.DecidedBy != e.Meena.ID {
		t.Errorf("request %+v, want approved by Meena (%d)", r, e.Meena.ID)
	}
}

func TestApprovalExpense_SelfManagerFallsBack(t *testing.T) { // APR-050
	e := at.New(t)
	e.Dir.SetManager(e.Emp("EMP-006"), e.Emp("EMP-006"))
	d := e.NewDoc(t, at.Claim, 11, at.Rs(2000), e.Kavya)
	at.MustOK(t, e.Submit(e.Kavya, d))
	r, ok := e.Latest(t, d)
	if !ok || r.ManagerUser != 0 || !slices.Equal(r.ApproverRoles, []string{"Director"}) {
		t.Errorf("request %v %+v, want no designated user and roles [Director]", ok, r)
	}
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
}

func TestApprovalApprove_OptionalComment(t *testing.T) { // APR-054
	e, d := pending42(t)
	const comment = "Rate checked against last PO."
	at.MustOK(t, e.Approve(e.Ravi, d, at.Comment(comment)))
	if last := lastHist(t, e, d); last.Action != approval.HistApproved || last.Reason != comment {
		t.Errorf("last history %+v, want approved with the comment %q", last, comment)
	}

	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d43))
	at.MustOK(t, e.Approve(e.Ravi, d43))
	if last := lastHist(t, e, d43); last.Action != approval.HistApproved || last.Reason != "" {
		t.Errorf("last history %+v, want approved with no comment", last)
	}
}

func TestApprovalReject_ReasonLength(t *testing.T) { // APR-059
	e, d := pending42(t)
	at.WantErr(t, e.Reject(e.Ravi, d, strings.Repeat("a", 1001)), approval.ErrReasonTooLong, "Reason must be 1,000 characters or fewer.")
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
	body := strings.Repeat("a", 1000)
	at.MustOK(t, e.Reject(e.Ravi, d, "  "+body+"  "))
	if last := lastHist(t, e, d); last.Action != approval.HistRejected || last.Reason != body {
		t.Errorf("last history %s with a %d-character reason, want rejected with the trimmed 1,000-character reason", last.Action, len(last.Reason))
	}
	if r, _ := e.Latest(t, d); r.Reason != body {
		t.Errorf("request reason of %d characters, want the trimmed 1,000", len(r.Reason))
	}
}

func TestApprovalResubmit_SameApproverMayApprove(t *testing.T) { // APR-068
	e, d := pending42(t)
	at.MustOK(t, e.Reject(e.Ravi, d, "Wrong vendor"))
	at.MustOK(t, e.Revise(e.Priya, d))
	at.MustOK(t, e.Submit(e.Priya, d))
	at.MustOK(t, e.Approve(e.Ravi, d))
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	if r, _ := e.Latest(t, d); r.Outcome != approval.OutcomeApproved || r.DecidedBy != e.Ravi.ID {
		t.Errorf("latest request %+v, want approved by Ravi (%d)", r, e.Ravi.ID)
	}
}
