package purchase_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P2 Tests ("the builders' StopAtPending is proven here"; PO spec §5,
// §18 PO-019's figures; CD-272 builder pattern): with amountOrder in place,
// NewOrder(StopAtPending) leaves the reference PO (₹94,164.00) waiting on the
// Purchase Manager rule, unnumbered, with one open request for that amount.
func TestPurchasetest_StopAtPending(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p2RefLines(t, env)...))
	if po.State != doctype.StatePending || po.Number != "" || po.CreatedBy != env.Priya.ID {
		t.Fatalf("StopAtPending: state %q number %q by %d, want pending, unnumbered, by Priya", po.State, po.Number, po.CreatedBy)
	}
	var req approval.Request
	var ok bool
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		req, ok, err = env.Approvals.LatestRequest(ctx, tx, purchase.DocType, po.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("the pending PO has no approval request")
	}
	if got := types.FormatMoney(req.Amount); got != "94,164.00" || !slices.Equal(req.ApproverRoles, []string{"Purchase Manager"}) || req.Outcome != "" {
		t.Errorf("approval request: amount %s roles %v outcome %q; want 94,164.00, [Purchase Manager], open", got, req.ApproverRoles, req.Outcome)
	}
}
