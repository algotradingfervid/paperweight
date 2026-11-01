package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// P43 · CD-370, UI v2 §5.5 item 5 and §5.8: the order's and the request's
// docscreen pages show each field's placeholder.

// p43Want checks that the page has one control for each key (a picker's
// search box for "q:field", else the control named field) with that
// placeholder.
func p43Want(t *testing.T, name string, p p15dPage, want map[string]string) {
	t.Helper()
	if p.status != 200 {
		t.Fatalf("%s: status %d: %s", name, p.status, p.text())
	}
	for field, text := range want {
		sel := `[name="` + field + `"]`
		if field[:2] == "q:" {
			sel = `input[name="q"][id^="f-` + field[2:] + `"]`
		}
		in := p.doc.Find(sel)
		if got, _ := in.Attr("placeholder"); in.Length() != 1 || got != text {
			t.Errorf("%s %s: %d controls, placeholder %q; want one with %q", name, field, in.Length(), got, text)
		}
	}
}

// Earlier PO no. shows only without approval rules (back entry), as in P40.
func TestP43_OrderPlaceholders(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t, purchasetest.WithNoRules()), nil)
	env := w.env
	p43Want(t, "new PO", w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new"), map[string]string{
		"q:vendor_id":  "Name, code or GSTIN",
		"q:site_id":    "Name or code",
		"q:project_id": "Name or code",
	})
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	p43Want(t, "PO terms", w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/terms"), map[string]string{
		"vendor_quote_ref": "e.g. QT-2026-118",
		"earlier_po_no":    "e.g. SI/PO/0412",
		"remarks":          "e.g. Rate agreed with the vendor on phone",
	})
	p43Want(t, "PO line", w.get(t, env.Priya, p15dDoc(purchase.DocType, po.ID)+"/lines/new"), map[string]string{
		"q:item_id": "Name or code",
	})
}

func TestP43_RequestPlaceholders(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	p43Want(t, "new request", w.get(t, env.Suresh, "/doc/"+purchase.RequestDocType+"/new"), map[string]string{
		"q:site_id":    "Name or code",
		"q:project_id": "Name or code",
		"purpose":      "e.g. Slab casting on level 3, Tower B",
	})
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	p43Want(t, "request line", w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/lines/new"), map[string]string{
		"q:item_id": "Name or code",
		"note":      "e.g. UltraTech, 53 grade",
	})
}
