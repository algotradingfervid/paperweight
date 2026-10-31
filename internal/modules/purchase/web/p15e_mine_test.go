package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P15e, CD-292 (4): approval/web's My submissions (mine.templ) lists
// approval requests only. purchase.Request isn't Approvable, so a submitted
// request opens none and never appears there, and the generic state word
// it would show can't reach a request; a submitted PO does appear. If this
// fails, mine.templ must pass the DocType so a request reads Submitted.
func TestPurchaseWeb_RequestsNotInMySubmissions(t *testing.T) {
	env := p15eRuleEnv(t)
	pr := purchasetest.NewRequest(t, env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	page := types.Page{Number: 1, Size: 25}

	mine, err := env.Approvals.MyRequests(t.Context(), env.Suresh, page)
	if err != nil {
		t.Fatalf("MyRequests(Suresh): %v", err)
	}
	for _, it := range mine.Rows {
		if it.DocType == purchase.RequestDocType {
			t.Errorf("Suresh's My submissions lists request %d (%s): %+v", it.DocID, pr.Number, it)
		}
	}
	priya, err := env.Approvals.MyRequests(t.Context(), env.Priya, page)
	if err != nil {
		t.Fatalf("MyRequests(Priya): %v", err)
	}
	found := false
	for _, it := range priya.Rows {
		found = found || (it.DocType == purchase.DocType && it.DocID == po.ID)
	}
	if !found {
		t.Errorf("Priya's My submissions = %+v, want her pending PO %d", priya.Rows, po.ID)
	}
}
