package purchaseweb

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// UI v2 C3c (A-22, CD-376 (2)): a draft PO's Items footer shows its saved
// lines and Taxable value (net_total); the default draft has one line of
// 100 at ₹62.50, so ₹6,250.00.
func TestC3c_OrderItemsFooterTotal(t *testing.T) {
	w := newP15dWeb(t)
	po := w.order(t, doctype.StateDraft)
	pg := w.get(t, w.env.Priya, p15dDoc(purchase.DocType, po.ID)+"/edit/items")
	got := strings.Join(strings.Fields(pg.doc.Find("#wizard .actionbar #footer-total").Text()), " ")
	if want := "1 item · Taxable value ₹6,250.00"; got != want {
		t.Errorf("Items footer total = %q, want %q", got, want)
	}
}
