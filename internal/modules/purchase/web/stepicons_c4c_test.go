package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4c: each step's Check card has its own tile.
func TestCheckTiles_Order(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	po := purchasetest.NewOrder(t, w.env, purchasetest.StopAtDraft())
	uitest.WantCheckTiles(t, p15aCheck(t, w, po.ID).doc,
		uitest.CheckTile{Title: "Details", Icon: "handshake"},
		uitest.CheckTile{Title: "Terms", Icon: "calendar-clock"},
		uitest.CheckTile{Title: "Items", Icon: "list-checks"})
}

func TestCheckTiles_Request(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t, purchasetest.StopAtDraft())
	page := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/check")
	if page.status != 200 {
		t.Fatalf("GET check = %d: %s", page.status, page.text())
	}
	uitest.WantCheckTiles(t, page.doc,
		uitest.CheckTile{Title: "Details", Icon: "clipboard-list"},
		uitest.CheckTile{Title: "Items", Icon: "list-checks"})
}
