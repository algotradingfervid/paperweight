package grntest

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G4 helpers (PO interplay). The GRN test seed has no approval
// rules (00-fixtures.md); a G4 case that needs a document waiting for
// approval adds one through APR, never by writing rule rows.

// AddRule adds an approval rule for DocType dt from amount from, approved by
// role, as the Administrator through APR: Purchase Manager for
// purchase.Order (the Purchase Order row of the approval rules table,
// GRN-037), Administrator for goodsreceipt.Receipt (PO-025's pending GRN;
// only the Administrator holds approve on it, so APR refuses a submit with
// no eligible approver under any other role).
func (e *Env) AddRule(t testing.TB, dt string, from types.Money, role string) {
	t.Helper()
	if _, err := e.Approvals.CreateRule(t.Context(), e.Admin, approval.RuleInput{
		DocType: dt, FromAmount: from, ApproverType: approval.ApproverRole, Role: role,
	}); err != nil {
		t.Fatalf("grntest: approval rule for %s from %s: %v", dt, types.FormatMoney(from), err)
	}
}
