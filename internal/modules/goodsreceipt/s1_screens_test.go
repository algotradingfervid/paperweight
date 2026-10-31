package goodsreceipt_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// MOD-GRN S1 (UX doc §5.2, §5.3; CD-217, CD-228): Facts is the GRN's own
// snapshot of its PO and whether any line is goods, for a viewer who may
// read the GRN. Suresh reads his goods receipt and his service receipt;
// Kavya (no GRN rights) is refused with AUTH's 403, and nothing is shown.
// On the real auth.Service (CD-213).
func TestGRN_FactsS1(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	g := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateDraft)
	svc := g2ServicePO(t, env)
	s := env.GRN(t, env.Suresh, svc.ID, []grntest.Line{{POLineID: svc.Lines[0].ID, Received: "8"}}, doctype.StateDraft)

	f, err := env.Service.Facts(t.Context(), env.Suresh, g.ID)
	want := goodsreceipt.ReceiptFacts{PONumber: "PO/26-27/00001", VendorName: "Shree Steel Traders", SiteName: "METRO-SITE", HasGoods: true}
	if err != nil || f != want {
		t.Errorf("Facts(goods receipt) = %+v, %v; want %+v", f, err, want)
	}
	if f, err := env.Service.Facts(t.Context(), env.Suresh, s.ID); err != nil || f.HasGoods {
		t.Errorf("Facts(service receipt) = %+v, %v; want HasGoods false", f, err)
	}
	f, err = env.Service.Facts(t.Context(), env.Kavya, g.ID)
	if !errors.Is(err, auth.ErrForbidden) || f != (goodsreceipt.ReceiptFacts{}) {
		t.Errorf("Facts as Kavya = %+v, %v; want AUTH's refusal and nothing", f, err)
	}
}
