package purchase_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P20 fix round 2 (CD-309 (4)): the reset applies only when the
// vendor changed. A SHREE draft holding SHREE's default NET30, NET30 then
// disabled, keeps NET30 through a remarks-only step save (vendor
// unchanged), though the vendor's default terms now resolve to none.
func TestPO_SameVendorKeepsDisabledDefaultTerms(t *testing.T) {
	env := purchasetest.New(t)
	net30 := p20Terms(t, env, "NET30", "Net 30 days")
	p20SetDefaultTerms(t, env, "SHREE", net30)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
	p20WantTerms(t, "SHREE draft", env, po.ID, net30, "Net 30 days")

	if err := paymentterms.New(p2Kit(env)).SetDisabled(t.Context(), env.Admin, net30, 1, true); err != nil {
		t.Fatalf("disable NET30: %v", err)
	}
	if _, err := p2Step(t, env, env.Priya, po.ID, []string{"remarks"}, map[string]string{"remarks": "Same vendor"}, nil); err != nil {
		t.Fatalf("remarks step save: %v", err)
	}
	if got := p2Snap(t, env, po.ID, "remarks")["remarks"]; got != "Same vendor" {
		t.Fatalf("remarks step save: remarks %q, want %q (save did not commit)", got, "Same vendor")
	}
	p20WantTerms(t, "remarks step save after NET30 was disabled", env, po.ID, net30, "Net 30 days")
}
