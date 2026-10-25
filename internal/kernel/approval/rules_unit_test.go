package approval_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
)

func TestApprovalRule_FromAmountNotNegative(t *testing.T) { // APR-003
	cat := at.Lookup
	in := approval.RuleInput{DocType: at.Order, FromAmount: -100, ApproverType: approval.ApproverRole, Role: "Purchase Manager"}
	at.WantErr(t, approval.ValidateRule(in, cat), approval.ErrInvalidRule, "From amount must be ₹0.00 or more.")
	in.FromAmount = 0
	at.MustOK(t, approval.ValidateRule(in, cat))
}

func TestApprovalRule_RoleRequired(t *testing.T) { // APR-005
	in := approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(50000), ApproverType: approval.ApproverRole, Role: "  "}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule, "Choose an approver role for this rule.")
}

func TestApprovalRule_ManagerNeedsFallbackRole(t *testing.T) { // APR-007
	in := approval.RuleInput{DocType: at.Claim, ApproverType: approval.ApproverManager}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule,
		"Choose a fallback role for claims whose employee has no reporting manager.")
}

func TestApprovalRule_OnlyApprovableDocTypes(t *testing.T) { // APR-009
	in := approval.RuleInput{DocType: at.ItemMaster, ApproverType: approval.ApproverRole, Role: "Director"}
	at.WantErr(t, approval.ValidateRule(in, at.Lookup), approval.ErrInvalidRule,
		"Item is a master and can't need approval. Choose a document type.")
	e := at.New(t)
	var labels []string
	for _, d := range e.A.ApprovableDocTypes() {
		labels = append(labels, d.Label)
	}
	if want := []string{"Expense Claim", "Goods Receipt Note", "Purchase Order"}; !slices.Equal(want, labels) {
		t.Errorf("approvable doc types = %q, want %q", labels, want)
	}
}
