package purchaseweb

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// Check page texts (UX §5.8; spec §7, §7.1, §24.1, §27.3).
const (
	p15aIntra      = "Intra-state: the supplier and the delivery site are both in Maharashtra."
	p15aState      = "Site PUNE-WH is under branch PUNE in Maharashtra, but project PRJ-TECHPARK belongs to branch BLR in Karnataka. Check the tax with your CA."
	p15aOwnerText  = "You own this order. Only you can change or submit it."
	p15aRavisWords = "Please check the cement rate with Shree."
)

// p15aCheck is PO id's check page as p, which must answer 200.
func p15aCheck(t *testing.T, w *p15dWeb, id int64) p15dPage {
	t.Helper()
	page := w.get(t, w.env.Priya, p15dDoc(purchase.DocType, id)+"/edit/check")
	if page.status != 200 {
		t.Fatalf("GET check = %d: %s", page.status, page.text())
	}
	return page
}

// p15aNotes are the check page's info banners but the approval preview.
func p15aNotes(p p15dPage) []string {
	var out []string
	for _, b := range p.banners("info") {
		if !strings.HasPrefix(b, "No approval needed") && !strings.HasPrefix(b, "This purchase order needs approval") {
			out = append(out, b)
		}
	}
	return out
}

// p15aWarnings are the messages of the check page's warning banner.
func p15aWarnings(p p15dPage) []string {
	if ws := p.find("main .banner--warning .banner-items li"); len(ws) > 0 {
		return ws
	}
	return p.banners("warning")
}

// UX §5.8 item 4 (spec §7, §7.1): the check page's notes are the place
// reason, then, under reverse charge, `Reverse charge applies. GST of ₹{x}
// is payable by us, not to the vendor.` A draft with no vendor or no site
// has no place reason, and one without reverse charge no reverse-charge
// note.
func TestPO_CheckNotes(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	plain := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p15aCement(t, w, "40", "345")))
	if got := p15aNotes(p15aCheck(t, w, plain.ID)); !slices.Equal(got, []string{p15aIntra}) {
		t.Errorf("notes = %q, want [%q]", got, p15aIntra)
	}
	rc := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}),
		purchasetest.OrderLines(p15aCement(t, w, "40", "345")))
	want := []string{p15aIntra, "Reverse charge applies. GST of ₹2,484.00 is payable by us, not to the vendor."}
	if got := p15aNotes(p15aCheck(t, w, rc.ID)); !slices.Equal(got, want) {
		t.Errorf("reverse charge notes = %q, want %q", got, want)
	}
	for name, h := range map[string]map[string]string{
		"no vendor": p15aHeader(t, w, "", "METRO-SITE", "PRJ-METRO"),
		"no site":   p15aHeader(t, w, "SHREE", "", "PRJ-METRO"),
	} {
		id := p15aSave(t, env, env.Priya, h)
		if got := p15aNotes(p15aCheck(t, w, id)); len(got) != 0 {
			t.Errorf("%s: notes = %q, want none", name, got)
		}
	}
}

// PUX-014 (PO-064, PO-005; spec §24.1): a copy whose source's payment terms
// NET30 and line 2's item WIRE-18G are now disabled shows, in order, Copy's
// notes (P12's texts) and then the place reason.
func TestPUX014_CopyNotes(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
	if err != nil {
		t.Fatal(err)
	}
	terms, err := paymentterms.New(p15aKit(env)).Create(t.Context(), env.Admin, paymentterms.Input{Code: "NET30", Name: "Net 30 days", Rows: rows})
	if err != nil {
		t.Fatal(err)
	}
	src := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"payment_terms_id": strconv.FormatInt(terms.ID, 10)}),
		purchasetest.OrderLines(p15aCement(t, w, "40", "345"), map[string]string{"item_id": w.id(t, "item", "WIRE-18G"), "hsn_sac": "7217"}))
	p15aDisable(t, env, "payment_terms", "NET30")
	p15aDisable(t, env, "item", "WIRE-18G")
	id, err := env.Service.Copy(t.Context(), env.Priya, src.ID)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	want := []string{
		"Copied from " + src.Number + ". Rates are as on that order; check them before you submit.",
		"Payment terms NET30 is disabled, so it was left blank.",
		"Line 2 of " + src.Number + " was left out: item WIRE-18G is disabled.",
		p15aIntra,
	}
	if got := p15aNotes(p15aCheck(t, w, id)); !slices.Equal(got, want) {
		t.Errorf("copy notes = %q, want %q", got, want)
	}
}

// PUX-036 (PO-084, spec §27.3; CD-280 D1) and PO-032's page part: the check
// page's warning banner holds OrderWarnings, Already received per line first,
// then the site-and-project state warning; a PO with neither has no warning.
func TestPUX036_CheckWarnings(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	h := p15aHeader(t, w, "SHREE", "PUNE-WH", "PRJ-TECHPARK")
	h["earlier_po_no"] = "P-17"
	l := p15aCement(t, w, "10", "345")
	l["already_received"] = "12"
	id := p15aSave(t, env, env.Arjun, h, l)
	page := w.get(t, env.Arjun, p15dDoc(purchase.DocType, id)+"/edit/check")
	if page.status != 200 {
		t.Fatalf("Arjun GET check = %d: %s", page.status, page.text())
	}
	want := []string{"Line 1: Already received is more than the quantity.", p15aState}
	if got := p15aWarnings(page); !slices.Equal(got, want) {
		t.Errorf("warnings = %q, want %q", got, want)
	}
	plain := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	if got := p15aWarnings(p15aCheck(t, w, plain.ID)); len(got) != 0 {
		t.Errorf("warnings on a plain draft = %q, want none", got)
	}
}

// PUX-006's check-page half (PO-060, UX §5.8 item 8) and PUX-007 (PO-062,
// G2): Priya's check page has the Owner card, `You own this order. Only you
// can change or submit it.` with `Hand over` to the hand-over page, then the
// Comments card with Ravi's comment, both before the grand total. An
// Administrator, who may write the draft, gets the Comments card but no
// Owner card.
func TestPUX006_CheckCards(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	if _, err := env.Comments.AddComment(t.Context(), env.Ravi, purchase.DocType, po.ID, p15aRavisWords); err != nil {
		t.Fatalf("Ravi's comment: %v", err)
	}
	page := p15aCheck(t, w, po.ID)
	text := page.text()
	if !p15aContains(text, "Owner", p15aOwnerText, "Hand over", "Comments", p15aRavisWords, "₹7,375.00") {
		t.Errorf("check page = %q, want the Owner card, then Comments with Ravi's words, then the total", text)
	}
	hand := page.doc.Find("main a").FilterFunction(func(_ int, s *goquery.Selection) bool { return p15dCollapse(s.Text()) == "Hand over" })
	if href := hand.AttrOr("href", ""); hand.Length() != 1 || href != "/modules/purchase/orders/"+strconv.FormatInt(po.ID, 10)+"/handover" {
		t.Errorf("%d Hand over links (href %q), want one to the hand-over page", hand.Length(), href)
	}

	admin := w.get(t, env.Admin, p15dDoc(purchase.DocType, po.ID)+"/edit/check")
	if admin.status != 200 {
		t.Fatalf("Admin GET check = %d", admin.status)
	}
	if strings.Contains(admin.text(), p15aOwnerText) || !strings.Contains(admin.text(), p15aRavisWords) {
		t.Errorf("Administrator's check page = %q, want Comments and no Owner card", admin.text())
	}
}
