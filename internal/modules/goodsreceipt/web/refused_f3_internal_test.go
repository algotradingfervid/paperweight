package grnweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// CD-381: on the bill card only a stale version is the ConflictBanner; a
// refused 409 is the danger message.
func TestBill_Refused409StaysDanger(t *testing.T) {
	const msg = "Can't do this now."
	card := goodsreceipt.BillCard{State: doctype.StateApproved}
	view := func(err *doc.Error) billView {
		t.Helper()
		k := billPostOf(goodsreceipt.BillInput{}, 2, err)
		v, ok := billViewOf(7, card, &k, false, false)
		if !ok {
			t.Fatal("no card")
		}
		return v
	}
	v := view(doc.Refused(msg))
	if v.Conflict != "" || v.Reload != "" || v.Msg == nil || v.Msg.Kind != components.Danger || v.Msg.Message != msg {
		t.Errorf("refused 409 = %+v, want the danger message and no Reload", v)
	}
	v = view(&doc.Error{Kind: doc.KindStale, Message: msg})
	if v.Conflict != msg || v.Reload != docPath(7)+"#bill" || v.Msg != nil {
		t.Errorf("stale = %+v, want the ConflictBanner to the page's bill card", v)
	}
}
