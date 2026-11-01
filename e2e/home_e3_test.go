//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// e3BigOrder submits a SHREE PO of ₹96,000 before GST as Priya, which waits
// for a Purchase Manager.
func (h *Harness) e3BigOrder() {
	h.t.Helper()
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
		},
		Lines: []map[string]string{{"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "1000", "price": "96"}},
	})
	h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
}

// e3Count is how many elements sel matches on the page.
func e3Count(s *Session, sel string) int {
	s.t.Helper()
	return Eval[int](s, `document.querySelectorAll(`+strconv.Quote(sel)+`).length`)
}

// UIV2-E3 (UI v2 §5.12, A-24): at 320 px the purchase home never scrolls
// sideways: Priya's with counts, 5 Recent rows and the quick actions;
// Ravi's with 3 Waiting rows and the hot To approve card; Deepak's caught-up
// home with its quick action.
func TestE2E_PurchaseHomeAt320(t *testing.T) {
	h := startPurchase(t)
	for range 4 {
		h.e3BigOrder()
	}
	for range 3 {
		h.metroDraft(priya)
	}

	p := h.SignIn(priya, 320)
	p.Open("/modules/purchase")
	p.WaitText("Recent")
	if n := e3Count(p, "main .homesect:has(h2) .datalist-link"); n != 5 {
		t.Errorf("Priya's home rows = %d, want 5 Recent rows", n)
	}
	if e3Count(p, "main a.statcard") == 0 || !p.Shows("New purchase order") {
		t.Errorf("Priya's home lacks counts or the quick actions\n%s", p.page())
	}
	noOverflow(p, "Priya's Purchase home")

	r := h.SignIn(ravi, 320)
	r.Open("/modules/purchase")
	r.WaitText("Waiting for you")
	r.WaitText("See all 4")
	if e3Count(r, "main a.statcard.statcard--hot") != 1 {
		t.Errorf("Ravi's home has no hot To approve card\n%s", r.page())
	}
	noOverflow(r, "Ravi's Purchase home")

	d := h.SignIn(deepak, 320)
	d.Open("/modules/purchase")
	d.WaitText("You're all caught up.")
	if e3Count(d, "main a.newtile") == 0 {
		t.Errorf("Deepak's caught-up home has no quick action\n%s", d.page())
	}
	noOverflow(d, "Deepak's Purchase home")
}
