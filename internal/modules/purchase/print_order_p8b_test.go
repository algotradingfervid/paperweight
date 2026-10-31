package purchase_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// P8b (CD-316 (4); PO spec §7, §9): on a reverse-charge PO each tax row reads
// "{tax} (reverse charge, payable by us) {rate}%" (the text is in the tax
// name), and the rows stay out of the Grand Total. Rows on a PO without
// reverse charge are unchanged.

// p8bRC follows the tax name on a reverse-charge PO's rows (CD-316 (4)).
const p8bRC = " (reverse charge, payable by us)"

// CD-316 (4): an intra-state reverse-charge PO (SHREE to METRO-SITE, both
// Maharashtra) labels its CGST and SGST rows as payable by us, and the Grand
// Total is the taxable value alone.
func TestPO_PrintReverseChargeRowsIntraState(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}))
	html := p8Print(t, env, env.Priya, po.ID)
	want := "CGST" + p8bRC + " 9% ₹7,182.00|SGST" + p8bRC + " 9% ₹7,182.00"
	if got := strings.Join(p8TaxRows(html), "|"); got != want {
		t.Errorf("intra-state reverse-charge tax rows %q, want %q:\n%s", got, want, html)
	}
	p8Contains(t, "intra-state reverse charge", html,
		"<p>CGST"+p8bRC+" 9% ₹7,182.00</p>",
		"<p>SGST"+p8bRC+" 9% ₹7,182.00</p>",
		"<p>Taxable Value ₹79,800.00</p>",
		`<p class="grand-total">Grand Total ₹79,800.00</p>`)
	p8Lacks(t, "intra-state reverse charge", html, "Grand Total ₹94,164.00")
}

// CD-316 (4): an inter-state reverse-charge PO (BHARAT, Karnataka, to
// METRO-SITE in Maharashtra) labels its IGST row as payable by us, and the
// Grand Total is the taxable value alone.
func TestPO_PrintReverseChargeRowsInterState(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT"), "reverse_charge": "true"}))
	html := p8Print(t, env, env.Priya, po.ID)
	if got, want := strings.Join(p8TaxRows(html), "|"), "IGST"+p8bRC+" 18% ₹14,364.00"; got != want {
		t.Errorf("inter-state reverse-charge tax rows %q, want %q:\n%s", got, want, html)
	}
	p8Contains(t, "inter-state reverse charge", html,
		"<p>IGST"+p8bRC+" 18% ₹14,364.00</p>",
		`<p class="grand-total">Grand Total ₹79,800.00</p>`)
	p8Lacks(t, "inter-state reverse charge", html, "CGST", "SGST")
}

// CD-316 (4): without reverse charge the rows keep their plain labels, both
// intra-state (CGST and SGST) and inter-state (IGST).
func TestPO_PrintNoReverseChargeRowsPlain(t *testing.T) {
	env := purchasetest.New(t)
	intra := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	inter := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}))
	for _, c := range []struct {
		what string
		id   int64
		want string
	}{
		{"intra-state", intra.ID, "CGST 9% ₹7,182.00|SGST 9% ₹7,182.00"},
		{"inter-state", inter.ID, "IGST 18% ₹14,364.00"},
	} {
		html := p8Print(t, env, env.Priya, c.id)
		if got := strings.Join(p8TaxRows(html), "|"); got != c.want {
			t.Errorf("%s without reverse charge: tax rows %q, want %q:\n%s", c.what, got, c.want, html)
		}
		p8Lacks(t, c.what+" without reverse charge", html, "(reverse charge")
	}
}
