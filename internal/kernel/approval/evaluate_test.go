package approval_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

func TestApprovalSubmit_BelowThresholdApproved(t *testing.T) { // APR-016
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 41, types.Money(4999999), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != doctype.StateApproved || e.Number(t, d) != "PO/26-27/00001" {
		t.Errorf("state %s number %q, want approved PO/26-27/00001", e.State(t, d), e.Number(t, d))
	}
	if r, ok := e.Latest(t, d); ok {
		t.Errorf("no request expected, got %+v", r)
	}
}

func TestApprovalMatch_UsesGrandTotalWithGST(t *testing.T) { // APR-019 (grand total is DOC's Hooks.Amount; APR stores what it is given)
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, types.Money(5003200), e.Priya, at.Party("Bharat Tools"))
	at.MustOK(t, e.Submit(e.Priya, d))
	r, ok := e.Latest(t, d)
	if e.State(t, d) != doctype.StatePending || !ok || r.Amount != 5003200 || r.RuleID != e.R1 {
		t.Errorf("state %s, request %v %+v; want pending_approval, amount 5003200 under R1 (%d)", e.State(t, d), ok, r, e.R1)
	}
}

func TestApprovalMatch_ExpenseUsesClaimTotal(t *testing.T) { // APR-020
	e := at.New(t)
	e2 := e.AddRule(t, approval.RuleInput{DocType: at.Claim, FromAmount: at.Rs(25000), ApproverType: approval.ApproverRole, Role: "Director"})
	d := e.NewDoc(t, at.Claim, 10, at.Rs(18000)+at.Rs(7000), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, ok := e.Latest(t, d)
	if e.State(t, d) != doctype.StatePending || !ok || r.Amount != 2500000 || r.RuleID != e2 {
		t.Errorf("state %s, request %v %+v; want pending_approval, amount 2500000 under E2 (%d)", e.State(t, d), ok, r, e2)
	}
}

func TestApprovalMatch_ZeroThresholdAlwaysApplies(t *testing.T) { // APR-023
	e := at.New(t)
	e.RemoveRules(t, at.Order)
	e.AddRule(t, approval.RuleInput{DocType: at.Order, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	d := e.NewDoc(t, at.Order, 42, at.Rs(1500), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("state %s, want pending_approval", e.State(t, d))
	}
}

func TestApprovalRequest_SnapshotsRule(t *testing.T) { // APR-025
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	r, _ := e.Latest(t, d)
	if want := []string{"Purchase Manager", "Director"}; !reflect.DeepEqual(r.ApproverRoles, want) {
		t.Errorf("snapshot roles %v, want %v", r.ApproverRoles, want)
	}
	if e.State(t, d) != doctype.StatePending {
		t.Errorf("Draft 42 state %s, want pending_approval", e.State(t, d))
	}
	if got := e.InboxTitles(t, e.Ravi); !slices.Contains(got, "Draft 42") {
		t.Errorf("Ravi's inbox %v, want it to contain Draft 42", got)
	}
	later := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, later))
	if e.State(t, later) != doctype.StateApproved {
		t.Errorf("state %s: a ₹60,000 PO submitted after the edit is below the new ₹75,000 rule", e.State(t, later))
	}
}

func TestApprovalSubmit_NoEligibleApproverRefused(t *testing.T) { // APR-040
	e := at.New(t)
	e.Auth.AddRole(e.Ravi.ID, "Purchase User")
	e.RemoveRole(t, e.Meena.ID, "Director")
	d := e.NewDoc(t, at.Order, 43, at.Rs(70000), e.Ravi)
	at.WantErr(t, e.Submit(e.Ravi, d), approval.ErrNoApprover,
		"No one else can approve this Purchase Order. Give the Purchase Manager role to another active user, then submit again.")
	if e.State(t, d) != doctype.StateDraft || len(e.History(t, d)) != 0 {
		t.Errorf("state %s, history %v: nothing may change", e.State(t, d), e.History(t, d))
	}
	if r, ok := e.Latest(t, d); ok {
		t.Errorf("no request expected, got %+v", r)
	}
}

func TestApprovalExpense_ReportingManagerApproves(t *testing.T) { // APR-044
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, _ := e.Latest(t, d)
	if e.State(t, d) != doctype.StatePending || r.ManagerUser != e.Priya.ID || r.ManagerName != "Priya Shah" {
		t.Fatalf("state %s, request %+v; want pending_approval with designated approver Priya Shah", e.State(t, d), r)
	}
	at.MustOK(t, e.Approve(e.Priya, d))
}

func TestApprovalExpense_NoManagerUsesFallback(t *testing.T) { // APR-047
	e := at.New(t)
	d := e.NewDoc(t, at.Claim, 11, at.Rs(2000), e.Anil, at.ForEmployee("EMP-001"))
	at.MustOK(t, e.Submit(e.Anil, d))
	r, _ := e.Latest(t, d)
	if e.State(t, d) != doctype.StatePending || r.ManagerUser != 0 || !slices.Equal(r.ApproverRoles, []string{"Director"}) {
		t.Errorf("state %s, request %+v; want pending_approval, no designated user, roles [Director]", e.State(t, d), r)
	}
}

func TestApprovalExpense_DisabledManagerUsesFallback(t *testing.T) { // APR-048
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, e *at.Env)
	}{
		{"manager disabled", func(t *testing.T, e *at.Env) { e.Disable(t, e.Priya.ID) }},
		{"manager has no user", func(t *testing.T, e *at.Env) { e.Unlink(t, "EMP-004") }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := at.New(t)
			c.setup(t, e)
			d := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
			at.MustOK(t, e.Submit(e.Deepak, d))
			r, ok := e.Latest(t, d)
			if e.State(t, d) != doctype.StatePending || !ok || r.ManagerUser != 0 || r.ManagerName != "" || !slices.Equal(r.ApproverRoles, []string{"Director"}) {
				t.Errorf("state %s, request %v %+v; want pending_approval, no designated user, roles [Director]", e.State(t, d), ok, r)
			}
		})
	}
}

func TestApprovalExpense_HighAmountGoesToRole(t *testing.T) { // APR-051
	e := at.New(t)
	e2 := e.AddRule(t, approval.RuleInput{DocType: at.Claim, FromAmount: at.Rs(25000), ApproverType: approval.ApproverRole, Role: "Director"})
	d := e.NewDoc(t, at.Claim, 10, at.Rs(30000), e.Deepak)
	at.MustOK(t, e.Submit(e.Deepak, d))
	r, ok := e.Latest(t, d)
	if !ok || r.RuleID != e2 || r.ManagerUser != 0 {
		t.Errorf("request %v %+v; want E2 (%d) with no designated user", ok, r, e2)
	}
	at.WantErr(t, e.Approve(e.Priya, d), approval.ErrNotEligible, "Only a user with the Director role can approve Draft 10 (₹30,000.00).")
}
