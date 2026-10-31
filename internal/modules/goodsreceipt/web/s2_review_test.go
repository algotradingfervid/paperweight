package grnweb_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN S2 review rows (fix loop): pending on the cards, Receive all
// pending on a fully received line, the summary line, and the two
// disclosures on a 422 redraw (UX doc §5.4). Suresh on the real
// auth.Service and engine (CD-213).

// Spec §4, UX §5.4 Cards (CD-238): a card's Pending is qty − already_received
// − accepted on other approved GRNs. Walk on one draft: with 200 KGS
// already received the hint reads Pending 1,000; another DRAFT GRN of 100
// on the same PO line doesn't lower it; an approved GRN of 300 does (700).
// A closed GRN can't happen (CloseNone, CD-277 (1)).
func TestGRNWeb_ItemsPendingReview(t *testing.T) {
	w := newS1World(t)
	po := purchasetest.NewOrder(t, w.Env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"earlier_po_no": "OLD-77"}), purchasetest.OrderLines(
			map[string]string{"qty": "1200", "already_received": "200"},
			map[string]string{"item_id": w.id("item", "WIRE-18G"), "qty": "50", "hsn_sac": "7217"},
		))
	id := w.draft(po)
	hint := func(what, want string) {
		t.Helper()
		r := w.s2Items(id)
		in := r.s2Input("received", 1)
		if got := s2Norm(r.doc.Find(`[id="` + in.AttrOr("id", "x") + `-hint"]`).Text()); got != want {
			t.Errorf("%s: card 1 hint = %q, want %q", what, got, want)
		}
	}
	hint("200 already received", "Ordered 1,200 · Pending 1,000 KGS")

	w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}}, doctype.StateDraft)
	hint("another draft GRN of 100", "Ordered 1,200 · Pending 1,000 KGS")

	w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "300"}}, doctype.StateApproved)
	hint("an approved GRN of 300", "Ordered 1,200 · Pending 700 KGS")
}

// UX §5.4 Receive all pending: it sets only cards with pending above 0, so
// a quantity typed on a fully received line (a second truck) stays.
func TestGRNWeb_ItemsFillKeepsFullyReceived(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "1200"}, s2Line{"WIRE-18G", "KGS", "50"}, s2Line{"MS-ANGLE-50", "KGS", "9"})
	w.s2Approve(po, map[int]string{3: "9"})
	id := w.draft(po)

	s2WantStep(t, w.s2Press(id, w.s2Items(id), "Receive all pending", s2Set{"received", 3, "4"}), "/edit/items", "Receive all pending with 4 on line 3")
	now := w.s2LineIDs(id, po)
	want := []string{s2Want(t, 1, "1200", "0", "", now[1]), s2Want(t, 2, "50", "0", "", now[2]), s2Want(t, 3, "4", "0", "", now[3])}
	if got := w.s2Lines(id, po); !slices.Equal(got, want) {
		t.Errorf("after Receive all pending: lines %q, want %q (line 3's typed 4 kept)", got, want)
	}
}

// UX §5.4 copy: the summary line "{PO number} · {vendor} · {site}".
func TestGRNWeb_ItemsSummaryLine(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	var site string
	if err := w.DB.SQL().QueryRowContext(t.Context(), `SELECT name FROM site WHERE code = 'METRO-SITE'`).Scan(&site); err != nil {
		t.Fatal(err)
	}
	r := w.s2Items(w.draft(po))
	want := po.Number + " · Shree Steel Traders · " + site
	n := r.doc.Find("#wizard-form p").FilterFunction(func(_ int, p *goquery.Selection) bool { return s2Norm(p.Text()) == want }).Length()
	if n != 1 {
		t.Errorf("%d paragraphs read %q on step 3, want 1", n, want)
	}
}

// UX §5.4: on a 422 redraw the Fully received lines disclosure opens when a
// card inside it has any typed value, Rejected or Reason for rejecting as
// well as Received now ("opens when any of its inputs has a value").
func TestGRNWeb_ItemsFullOpenOnTyped(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "100"}, s2Line{"WIRE-18G", "KGS", "50"})
	w.s2Approve(po, map[int]string{2: "50"})
	id := w.draft(po)
	page := w.s2Items(id)
	for _, c := range []struct {
		name  string
		set   s2Set
		blank []s2Set // the card's other inputs, posted blank so they can't open it
	}{
		{"rejected typed", s2Set{"rejected", 2, "1"}, nil},
		{"reason typed", s2Set{"reason", 2, "Wet"}, nil},
		{"received typed", s2Set{"received", 2, "5"}, []s2Set{{"rejected", 2, ""}, {"reason", 2, ""}}},
	} {
		r := w.s2Save(id, page, append([]s2Set{{"received", 1, "abc"}, c.set}, c.blank...)...)
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: save = %d, want 422 (card 1 can't be read)", c.name, r.status)
			continue
		}
		if d := s2Disclosure(r.doc.Find("#wizard-form"), "Fully received lines (1)"); d.Length() != 1 || !s2Open(d) {
			t.Errorf("%s: Fully received lines (1) is closed (%d found) while card 2 inside it has a typed value", c.name, d.Length())
		}
		if got := r.s2Value(c.set.key, 2); got != c.set.val {
			t.Errorf("%s: redraw lost card 2's %s %q: %q", c.name, c.set.key, c.set.val, got)
		}
	}
}

// UX §5.4 Some rejected: "open when rejected is above 0 or has an error",
// and the error on Reason for rejecting too: a reason over 200 characters
// (a crafted post past maxlength) with nothing rejected is refused by the
// engine on reject_reason, and card 1's Some rejected opens around it; a
// Rejected NUM can't read opens it as well.
func TestGRNWeb_ItemsSomeRejectedOpenOnError(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	id := w.draft(po)
	page := w.s2Items(id)
	for _, c := range []struct {
		name, field string
		key         string
		sets        []s2Set
	}{
		{"reason over 200", "reject_reason", "reason", []s2Set{{"received", 1, "10"}, {"rejected", 1, ""}, {"reason", 1, strings.Repeat("x", 201)}}},
		{"rejected unreadable", "rejected_qty", "rejected", []s2Set{{"received", 1, "10"}, {"rejected", 1, "x"}}},
	} {
		r := w.s2Save(id, page, c.sets...)
		if r.status != http.StatusUnprocessableEntity {
			t.Errorf("%s: save = %d, want 422", c.name, r.status)
			continue
		}
		in := r.s2Input(c.key, 1)
		linked := false
		for _, href := range r.s2Links() {
			linked = linked || (href == "#"+in.AttrOr("id", "x"))
		}
		if !linked || !strings.HasPrefix(in.AttrOr("id", ""), "f-lines-") || !strings.HasSuffix(in.AttrOr("id", ""), "-"+c.field) {
			t.Errorf("%s: no summary link to card 1's %s input (id %q); links %q", c.name, c.key, in.AttrOr("id", ""), r.s2Links())
		}
		if d := s2Disclosure(r.s2Card(t, 1), "Some rejected"); d.Length() != 1 || !s2Open(d) {
			t.Errorf("%s: card 1's Some rejected is closed around its %s error", c.name, c.field)
		}
	}
}
