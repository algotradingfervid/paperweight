package approval_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// roleDouble is a Deps.Roles double: name → active; absent names don't exist.
type roleDouble map[string]bool

func (r roleDouble) RoleStatus(_ context.Context, name string) (exists, active bool, err error) {
	active, exists = r[name]
	return exists, active, nil
}

func wantField(t *testing.T, err error, field string) {
	t.Helper()
	var h doctype.HTTPError
	if !errors.As(err, &h) || len(h.FieldErrors()) != 1 || h.FieldErrors()[0].Field != field {
		t.Errorf("error %v: want one field error on %q", err, field)
	}
}

func ruleByID(t *testing.T, e *at.Env, id int64) approval.Rule {
	t.Helper()
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	i := slices.IndexFunc(rules, func(r approval.Rule) bool { return r.ID == id })
	if i < 0 {
		t.Fatalf("rule %d not found", id)
	}
	return rules[i]
}

func TestApprovalRule_RoleMustBeActive(t *testing.T) { // APR-006
	e := at.New(t)
	a := approval.New(approval.Deps{
		DB: e.DB, Clock: e.Clock, Auth: e.Auth, Info: e.Docs, Directory: e.Dir, Audit: e.Audit,
		Lookup: at.Lookup, All: at.DocTypes,
		Roles: roleDouble{"Site Auditor": false, "Director": true},
	})
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: "Site Auditor"}
	_, err := a.CreateRule(t.Context(), e.Anil, in)
	at.WantErr(t, err, approval.ErrInvalidRule, "Role Site Auditor is disabled. Choose an active role.")
	wantField(t, err, "role")

	// A role that doesn't exist gets the same message.
	in.Role = "Night Watch"
	_, err = a.CreateRule(t.Context(), e.Anil, in)
	at.WantErr(t, err, approval.ErrInvalidRule, "Role Night Watch is disabled. Choose an active role.")

	// A reporting-manager rule checks its fallback role.
	_, err = a.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Claim, FromAmount: at.Rs(100000),
		ApproverType: approval.ApproverManager, FallbackRole: "Site Auditor"})
	at.WantErr(t, err, approval.ErrInvalidRule, "Role Site Auditor is disabled. Choose an active role.")
	wantField(t, err, "fallback_role")

	if n := len(e.Audit.Entries()); n != 0 {
		t.Fatalf("%d audit entries after refused saves", n)
	}
	in.Role = "Director"
	_, err = a.CreateRule(t.Context(), e.Anil, in)
	at.MustOK(t, err)
}

func TestApprovalRule_ManagerOnlyForExpenses(t *testing.T) { // APR-008
	e := at.New(t)
	_, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000),
		ApproverType: approval.ApproverManager, FallbackRole: "Director"})
	at.WantErr(t, err, approval.ErrInvalidRule, "Reporting manager approval is only available for Expense Claim. Choose a role.")
	wantField(t, err, "role")
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	if len(rules) != 3 || len(e.Audit.Entries()) != 0 {
		t.Errorf("something was saved: %d rules, %d audit entries", len(rules), len(e.Audit.Entries()))
	}
}

func TestApprovalRule_UnknownApproverTypeRefused(t *testing.T) { // Task 3 review: unknown type is a 422, not a DB CHECK 500
	for _, typ := range []approval.ApproverType{"", "Role", "anyone"} {
		in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: typ, Role: "Director"}
		at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule, "Choose an approver role for this rule.")
	}
	e := at.New(t)
	_, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), Role: "Director"})
	at.WantErr(t, err, approval.ErrInvalidRule, "Choose an approver role for this rule.")
	at.WantStatus(t, err, 422)
}

func TestApprovalRule_DisabledIgnored(t *testing.T) { // APR-011
	e := at.New(t)
	at.MustOK(t, e.A.SetRuleEnabled(t.Context(), e.Anil, e.R1, false))
	nr, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000),
		ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	r, ok := e.Latest(t, d)
	if !ok || r.RuleID != nr.ID || r.RuleID == e.R1 {
		t.Fatalf("request %v %+v; want rule %d (not R1 %d)", ok, r, nr.ID, e.R1)
	}
	if !slices.Equal(r.ApproverRoles, []string{"Director"}) {
		t.Errorf("approver roles %q, want [Director]", r.ApproverRoles)
	}
}

func TestApprovalRule_ReenableRechecksDuplicate(t *testing.T) { // APR-012
	e := at.New(t)
	b, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(60000),
		ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	at.MustOK(t, e.A.SetRuleEnabled(t.Context(), e.Anil, b.ID, false))
	// A disabled rule may sit on R1's amount.
	_, err = e.A.UpdateRule(t.Context(), e.Anil, b.ID, ruleByID(t, e, b.ID).Version, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000),
		ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	before := len(e.Audit.Entries())

	err = e.A.SetRuleEnabled(t.Context(), e.Anil, b.ID, true)
	at.WantErr(t, err, approval.ErrDuplicateRule, "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead.")
	if r := ruleByID(t, e, b.ID); r.Enabled || r.FromAmount != at.Rs(50000) {
		t.Errorf("rule B after refused enable: %+v", r)
	}
	if n := len(e.Audit.Entries()); n != before {
		t.Errorf("refused enable wrote %d audit entries", n-before)
	}
}

func TestApprovalRule_ChangesAudited(t *testing.T) { // APR-014
	e := at.New(t)
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000),
		ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	at.MustOK(t, err)
	got := e.Audit.Entries()
	if len(got) != 1 {
		t.Fatalf("%d audit entries, want 1: %+v", len(got), got)
	}
	en := got[0]
	if en.By != e.Anil.ID || en.DocType != "approval.Rule" || en.DocID != e.R1 || en.Action != "save" {
		t.Errorf("entry %+v; want save of approval.Rule %d by Anil (%d)", en, e.R1, e.Anil.ID)
	}
	// doctype, approver_type and role are unchanged, so only from_amount is listed.
	want := []audit.Change{{Field: "from_amount", Before: "5000000", After: "7500000", Op: "set"}}
	if !reflect.DeepEqual(en.Changes, want) {
		t.Errorf("changes %+v, want %+v", en.Changes, want)
	}
}
