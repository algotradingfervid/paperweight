package approval_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
)

func TestApprovalRule_CreateAmountRule(t *testing.T) { // APR-001
	e := at.New(t)
	e.RemoveRules(t, at.Order)
	r, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000),
		ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	at.MustOK(t, err)
	if !r.Enabled || r.FromAmount != 5000000 || r.AllowSelfApproval || r.ID == 0 {
		t.Errorf("%+v", r)
	}
	got := e.Audit.Entries()
	if len(got) != 1 || got[0].Action != "create" || got[0].By != e.Anil.ID || got[0].DocType != "approval.Rule" || got[0].DocID != r.ID {
		t.Fatalf("%+v", got)
	}
	if !slices.ContainsFunc(got[0].Changes, func(c audit.Change) bool { return c.Field == "from_amount" && c.After == "5000000" }) {
		t.Error("from_amount change missing")
	}
}

func TestApprovalRule_OnlyAdminCanEdit(t *testing.T) { // APR-002
	const msg = "You don't have permission to change approval rules. Ask an Administrator."
	e := at.New(t)
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: "Director"}
	_, err := e.A.CreateRule(t.Context(), e.Ravi, in)
	at.WantErr(t, err, approval.ErrNotAdmin, msg)
	at.WantStatus(t, err, http.StatusForbidden)
	_, err = e.A.UpdateRule(t.Context(), e.Ravi, e.R1, 1, in)
	at.WantErr(t, err, approval.ErrNotAdmin, msg)
	at.WantStatus(t, err, http.StatusForbidden)
	err = e.A.SetRuleEnabled(t.Context(), e.Ravi, e.R1, false)
	at.WantErr(t, err, approval.ErrNotAdmin, msg)
	at.WantStatus(t, err, http.StatusForbidden)
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	if len(rules) != 3 || len(e.Audit.Entries()) != 0 {
		t.Errorf("something was saved: %d rules, %d audit entries", len(rules), len(e.Audit.Entries()))
	}
	for _, r := range rules {
		if r.ID == e.R1 && (!r.Enabled || r.FromAmount != at.Rs(50000) || r.Role != "Purchase Manager" || r.Version != 1) {
			t.Errorf("R1 changed: %+v", r)
		}
	}
}

func TestApprovalRule_DuplicateBlocked(t *testing.T) { // APR-010
	e := at.New(t)
	_, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.WantErr(t, err, approval.ErrDuplicateRule, "A Purchase Order rule from ₹50,000.00 already exists. Edit that rule instead.")
	_, err = e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
}

func TestApprovalRule_UsedRuleDisableNotDelete(t *testing.T) { // APR-013
	e := at.New(t)
	for i, id := range []int64{42, 43, 44} {
		d := e.NewDoc(t, at.Order, id, at.Rs(60000+int64(i)), e.Priya)
		at.MustOK(t, e.Submit(e.Priya, d))
	}
	at.WantErr(t, e.A.DeleteRule(t.Context(), e.Anil, e.R1), approval.ErrRuleInUse, "This rule has been used by 3 documents. Disable it instead of deleting.")
	at.MustOK(t, e.A.SetRuleEnabled(t.Context(), e.Anil, e.R1, false))
	fresh, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Director"})
	at.MustOK(t, err)
	at.MustOK(t, e.A.DeleteRule(t.Context(), e.Anil, fresh.ID))
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	for _, r := range rules {
		if r.ID == fresh.ID {
			t.Errorf("deleted rule still listed: %+v", r)
		}
		if r.ID == e.R1 && r.Enabled {
			t.Errorf("R1 still enabled: %+v", r)
		}
	}
	if !slices.ContainsFunc(rules, func(r approval.Rule) bool { return r.ID == e.R1 }) {
		t.Error("used rule R1 was deleted")
	}
}

func TestApprovalRule_TrimsRoleNames(t *testing.T) { // Review Focus 2
	e := at.New(t)
	r, err := e.A.CreateRule(t.Context(), e.Anil, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(200000), ApproverType: approval.ApproverRole, Role: " Purchase Manager "})
	at.MustOK(t, err)
	if r.Role != "Purchase Manager" {
		t.Errorf("role %q", r.Role)
	}
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	if !slices.ContainsFunc(rules, func(s approval.Rule) bool { return s.ID == r.ID && s.Role == "Purchase Manager" }) {
		t.Errorf("stored rule not trimmed: %+v", rules)
	}
}

func TestApprovalRule_StaleEditRefused(t *testing.T) { // Review Focus 3
	e := at.New(t)
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75000), ApproverType: approval.ApproverRole, Role: "Purchase Manager"}
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, in)
	at.MustOK(t, err)
	_, err = e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, in)
	at.WantErr(t, err, approval.ErrRuleConflict, "This rule was changed by someone else. Reload and try again.")
	stale := in
	stale.FromAmount = at.Rs(80000)
	_, err = e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, stale)
	at.WantErr(t, err, approval.ErrRuleConflict, "This rule was changed by someone else. Reload and try again.")
	rules, err := e.A.Rules(t.Context(), e.Anil)
	at.MustOK(t, err)
	if !slices.ContainsFunc(rules, func(r approval.Rule) bool { return r.ID == e.R1 && r.FromAmount == at.Rs(75000) }) {
		t.Errorf("stale save overwrote R1: %+v", rules)
	}
}
