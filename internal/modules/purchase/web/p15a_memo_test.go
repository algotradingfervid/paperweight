package purchaseweb

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// Plan P15a (HasRules and LastPricesFor read once per request): the memo
// keeps a request's value until the request's context ends, then drops it,
// so a later request reads again.
func TestPO_PerRequestMemo(t *testing.T) {
	var memo perRequest[int]
	loads := 0
	load := func() int { loads++; return loads }
	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequestWithContext(ctx, "GET", "/", nil)
	if a, b := memo.get(r, load), memo.get(r, load); a != 1 || b != 1 {
		t.Errorf("one request got %d then %d, want the first load twice", a, b)
	}
	other := httptest.NewRequest("GET", "/", nil)
	if got := memo.get(other, load); got != 2 {
		t.Errorf("another request got %d, want its own load", got)
	}
	cancel()
	deadline := 0
	for {
		if _, ok := memo.m.Load(r); !ok {
			break
		}
		if deadline++; deadline > 1000 {
			t.Fatal("the ended request's value is still held")
		}
		time.Sleep(time.Millisecond)
	}
}

// PUX-010 (UX §5.5 radio option `{city} · No GSTIN`): an unregistered
// vendor's address without a GSTIN reads `{city} · No GSTIN`; a 422 that
// posts no address choice redraws with the primary billing address checked.
func TestPUX010_NoGSTINAndUnchosen(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	id := p15aSave(t, env, env.Priya, p15aHeader(t, w, "LOCAL", "METRO-SITE", "PRJ-METRO"))
	terms := p15dDoc(purchase.DocType, id) + "/edit/terms"
	if got := p15aRadios(w.get(t, env.Priya, terms)); len(got) != 1 || got[0].label[:len("Pune · No GSTIN")] != "Pune · No GSTIN" || !got[0].checked {
		t.Errorf("LOCAL's radios = %+v, want one, Pune · No GSTIN, checked", got)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	a := w.do(t, env.Priya, "POST", p15dDoc(purchase.DocType, po.ID)+"/edit/terms", url.Values{"version": {p15aVersion(t, w, po.ID)}, "earlier_po_date": {"31-02-2026"}})
	if a.status != 422 {
		t.Fatalf("POST a bad date with no address = %d, want 422", a.status)
	}
	if got := p15aRadios(a); len(got) != 3 || !got[0].checked {
		t.Errorf("422 with no address posted: radios %+v, want the primary billing (Mumbai) checked", got)
	}
}
