package purchaseweb

import (
	"net/url"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// M0-49 (CD-322): Terms' reverse_charge is docscreen's generic Check. Posted
// as the page draws it, ticked saves 1 and unticked (nothing posted) saves 0.
func TestM49_ReverseChargeSwitchSaves(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	terms := p15dDoc(purchase.DocType, po.ID) + "/edit/terms"
	for _, c := range []struct {
		on   bool
		want int
	}{{true, 1}, {false, 0}, {true, 1}} {
		page := w.get(t, env.Priya, terms)
		in := page.doc.Find(`input[type="checkbox"][role="switch"][name="reverse_charge"]`)
		if page.status != 200 || in.Length() != 1 {
			t.Fatalf("GET terms = %d with %d reverse_charge switches: %s", page.status, in.Length(), page.text())
		}
		form := url.Values{"version": {p15aVersion(t, w, po.ID)}}
		if c.on {
			form.Set("reverse_charge", in.AttrOr("value", ""))
		}
		if a := w.do(t, env.Priya, "POST", terms, form); a.status != 303 {
			t.Fatalf("on %v: POST terms = %d: %s", c.on, a.status, a.text())
		}
		var got int
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT reverse_charge FROM purchase_order WHERE id = ?`, po.ID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("on %v: stored reverse_charge %d, want %d", c.on, got, c.want)
		}
	}
}
