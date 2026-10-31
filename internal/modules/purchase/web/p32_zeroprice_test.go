package purchaseweb

import (
	"net/url"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// CD-337 (4): the check page, where the PO is submitted, shows `Price is ₹0
// on line {n}.` in its warning banner with the other OrderWarnings, one per
// ₹0 line; a ₹0.01 line shows none. It is a warning, not a refusal: the
// submit from that page goes through.
func TestPO_ZeroPriceCheckWarning(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	h := p15aHeader(t, w, "SHREE", "METRO-SITE", "PRJ-METRO")
	id := p15aSave(t, env, env.Priya, h, p15aCement(t, w, "40", "0"), p15aCement(t, w, "10", "345"), p15aCement(t, w, "5", "0"))
	want := []string{"Price is ₹0 on line 1.", "Price is ₹0 on line 3."}
	if got := p15aWarnings(p15aCheck(t, w, id)); !slices.Equal(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	cent := p15aSave(t, env, env.Priya, h, p15aCement(t, w, "40", "0.01"))
	if got := p15aWarnings(p15aCheck(t, w, cent)); len(got) != 0 {
		t.Errorf("₹0.01 warnings = %q, want none", got)
	}

	var version int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if a := w.do(t, env.Priya, "POST", p15dDoc(purchase.DocType, id)+"/submit", url.Values{"version": {strconv.Itoa(version)}}); a.status != 303 {
		t.Fatalf("submit = %d: %s", a.status, a.text())
	}
	var state string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT state FROM purchase_order WHERE id = ?`, id).Scan(&state); err != nil || state == string(doctype.StateDraft) {
		t.Errorf("state after submit = %q, %v; want it out of draft", state, err)
	}
}
