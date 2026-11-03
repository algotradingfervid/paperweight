package grnweb_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-GRN S3c: the GRN's line card on the check and document pages (UX doc
// §5.5 P1, GUX-017): numbered by the PO line, titled by the item,
// "Received {qty} Accepted {qty}" in the snapshot UoM, the note
// "Rejected {qty}: {reason}" when anything was rejected, and the accepted
// value as its amount. A field Hide hides stays off the card (CD-319 (9)),
// and Hide is asked with the line's Ctx (CD-339).

// s3cCard is one line card as the page shows it.
type s3cCard struct {
	no, item, code, amount string
	meta                   []string // each .lines-meta row: the quantities, then the note
}

// s3cCards are the page's line cards in page order.
func (p s1Page) s3cCards() []s3cCard {
	var out []s3cCard
	p.doc.Find(".lines-card").Each(func(_ int, s *goquery.Selection) {
		c := s3cCard{
			no:     s2Norm(s.Find(".lines-no").Text()),
			item:   s2Norm(s.Find(".lines-name > span").First().Text()),
			code:   s2Norm(s.Find(".lines-name .code").Text()),
			amount: s2Norm(s.Find(".lines-amount").Text()),
		}
		s.Find(".lines-meta").Each(func(_ int, m *goquery.Selection) {
			if t := s2Norm(m.Text()); t != "" {
				c.meta = append(c.meta, t)
			}
		})
		out = append(out, c)
	})
	return out
}

// s3cPO is an approved PO from SHREE: line 1 WIRE-18G 50 KGS at 96, line 2
// BOLT-M12 10 NOS at 25, line 3 MS-ANGLE-50 1,200 KGS at 62.5.
func (w *s1World) s3cPO() doc.Doc {
	w.t.Helper()
	line := func(item, uom, qty, price string) map[string]string {
		return map[string]string{"item_id": w.id("item", item), "uom_id": w.id("uom", uom), "qty": qty, "price": price}
	}
	return purchasetest.NewOrder(w.t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		line("WIRE-18G", "KGS", "50", "96"), line("BOLT-M12", "NOS", "10", "25"), line("MS-ANGLE-50", "KGS", "1200", "62.5")))
}

// s3cDraft is a draft GRN by Suresh on s3cPO posting PO line 3 first
// (received 400, rejected 10, Bent pieces), then PO line 1 (received 50),
// so each card's position differs from its PO line number.
func (w *s1World) s3cDraft(po doc.Doc) int64 {
	w.t.Helper()
	return w.GRN(w.t, w.Suresh, po.ID, []grntest.Line{
		{POLineID: po.Lines[2].ID, Received: "400", Rejected: "10", Reason: "Bent pieces"},
		{POLineID: po.Lines[0].ID, Received: "50"},
	}, doctype.StateDraft).ID
}

// s3cWant fails unless got equals want, field by field.
func s3cWant(t *testing.T, where string, got []s3cCard, want []s3cCard) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d line cards %+v, want %d", where, len(got), got, len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.no != w.no || g.item != w.item || g.code != w.code || g.amount != w.amount || strings.Join(g.meta, " | ") != strings.Join(w.meta, " | ") {
			t.Errorf("%s: card %d = %+v, want %+v", where, i+1, g, w)
		}
	}
}

// UX §5.5 P1, GUX-017: on the check page and, after submit, the document
// page, each card carries its PO line number (3, then 1, in posted order),
// the item's name and code, the received and accepted quantities in the
// snapshot UoM, the reject note only on the line with a reject, and the
// accepted value (390 × 62.5, 50 × 96).
func TestGRNWeb_LineCard(t *testing.T) {
	w := newS1World(t)
	id := w.s3cDraft(w.s3cPO())
	want := []s3cCard{
		{no: "3", item: s2Angle, code: "MS-ANGLE-50", amount: "₹24,375.00",
			meta: []string{"Received 400 KGS Accepted 390 KGS", "Rejected 10 KGS: Bent pieces"}},
		{no: "1", item: s2Wire, code: "WIRE-18G", amount: "₹4,800.00",
			meta: []string{"Received 50 KGS Accepted 50 KGS"}},
	}
	s3cWant(t, "check page", s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards(), want)
	w.s3Submit(id)
	s3cWant(t, "document page", s3OK(t, w.get(w.Suresh, s3Doc(id)), "document page").s3cCards(), want)
}

// CD-319 (9): with a card field hidden by another Hide, the card leaves out
// what that field holds; the rest stays. Hidden received or accepted drops
// its half of the quantities, and accepted also the accepted value; hidden
// rejected drops the note; hidden reason leaves "Rejected 10 KGS"; hidden
// UoM leaves the bare quantities; a hidden accepted_value, the grand figure,
// drops the card's amount too (CD-319 (8)).
func TestGRNWeb_LineCardHonoursHide(t *testing.T) {
	cases := map[string]s3cCard{
		"received_qty": {amount: "₹24,375.00", meta: []string{"Accepted 390 KGS", "Rejected 10 KGS: Bent pieces"}},
		"accepted_qty": {meta: []string{"Received 400 KGS", "Rejected 10 KGS: Bent pieces"}},
		"rejected_qty": {amount: "₹24,375.00", meta: []string{"Received 400 KGS Accepted 390 KGS"}},
		"reject_reason": {amount: "₹24,375.00",
			meta: []string{"Received 400 KGS Accepted 390 KGS", "Rejected 10 KGS"}},
		"uom_id":         {amount: "₹24,375.00", meta: []string{"Received 400 Accepted 390", "Rejected 10: Bent pieces"}},
		"accepted_value": {meta: []string{"Received 400 KGS Accepted 390 KGS", "Rejected 10 KGS: Bent pieces"}},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			w := newS1World(t)
			id := w.s3cDraft(w.s3cPO())
			w.hidden = map[string]bool{name: true}
			want.no, want.item, want.code = "3", s2Angle, "MS-ANGLE-50"
			got := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards()
			if len(got) == 0 {
				t.Fatal("no line cards on the check page")
			}
			s3cWant(t, "check page with "+name+" hidden", got[:1], []s3cCard{want})
		})
	}
}

// CD-339: the card asks Hide with the line's own Ctx. This Hide hides the
// reason only where the Ctx's stored inputs hold the KGS unit, which
// FieldInput shows by its label only when it is the stored value (CD-217):
// the KGS line's card loses its reason, the NOS line's keeps it. With the
// header's Ctx neither would, and with one Ctx for both lines both would
// match.
func TestGRNWeb_LineCardLineCtx(t *testing.T) {
	w := newS1World(t)
	po := w.s3cPO()
	id := w.GRN(t, w.Suresh, po.ID, []grntest.Line{
		{POLineID: po.Lines[2].ID, Received: "400", Rejected: "10", Reason: "Bent pieces"},
		{POLineID: po.Lines[1].ID, Received: "10", Rejected: "2", Reason: "Wrong size"},
	}, doctype.StateDraft).ID
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	uomField, _ := dt.Field("uom_id")
	kgs := w.id("uom", "KGS")
	w.hideIf = func(c docscreen.Ctx, f doctype.Field) bool {
		if f.Name != "reject_reason" {
			return false
		}
		var b strings.Builder
		if err := docscreen.FieldInput(uomField, "s3c-probe", kgs, "", c.Deps).Render(c.Req.Context(), &b); err != nil {
			t.Errorf("FieldInput: %v", err)
		}
		return strings.Contains(b.String(), "KGS")
	}
	got := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards()
	s3cWant(t, "check page", got, []s3cCard{
		{no: "3", item: s2Angle, code: "MS-ANGLE-50", amount: "₹24,375.00",
			meta: []string{"Received 400 KGS Accepted 390 KGS", "Rejected 10 KGS"}},
		{no: "2", item: s2Bolt, code: "BOLT-M12", amount: "₹200.00",
			meta: []string{"Received 10 NOS Accepted 8 NOS", "Rejected 2 NOS: Wrong size"}},
	})
}

// GUX-013 and the reject boundary: a draft may keep a reject without its
// reason until submit, and its note is then "Rejected 10 KGS" with no
// dangling colon; the smallest reject on a decimal unit (0.001 KGS) has the
// note too.
func TestGRNWeb_LineCardRejects(t *testing.T) {
	cases := map[string]struct {
		line grntest.Line
		want s3cCard
	}{
		"no reason": {grntest.Line{Received: "410", Rejected: "10"}, s3cCard{amount: "₹25,000.00",
			meta: []string{"Received 410 KGS Accepted 400 KGS", "Rejected 10 KGS"}}},
		"0.001 rejected": {grntest.Line{Received: "400", Rejected: "0.001", Reason: "Bent"}, s3cCard{amount: "₹24,999.94",
			meta: []string{"Received 400 KGS Accepted 399.999 KGS", "Rejected 0.001 KGS: Bent"}}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newS1World(t)
			po := w.s3cPO()
			c.line.POLineID = po.Lines[2].ID
			id := w.GRN(t, w.Suresh, po.ID, []grntest.Line{c.line}, doctype.StateDraft).ID
			c.want.no, c.want.item, c.want.code = "3", s2Angle, "MS-ANGLE-50"
			got := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards()
			s3cWant(t, "check page", got, []s3cCard{c.want})
		})
	}
}

// The card's lines are read once per request, never reused by the next
// one: after the check page is drawn, Suresh adds PO line 1 on step 3, and
// the next check page gives the new line its own card, not the default.
func TestGRNWeb_LineCardEachRequest(t *testing.T) {
	w := newS1World(t)
	po := w.s3cPO()
	id := w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[2].ID, Received: "400"}}, doctype.StateDraft).ID
	angle := s3cCard{no: "3", item: s2Angle, code: "MS-ANGLE-50", amount: "₹25,000.00", meta: []string{"Received 400 KGS Accepted 400 KGS"}}
	s3cWant(t, "check page before", s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards(), []s3cCard{angle})
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "50"}), "/edit/check", "add PO line 1")
	s3cWant(t, "check page after", s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page").s3cCards(), []s3cCard{
		{no: "1", item: s2Wire, code: "WIRE-18G", amount: "₹4,800.00", meta: []string{"Received 50 KGS Accepted 50 KGS"}},
		angle,
	})
}

// A line whose accepted value is over the money maximum (exactly twice an
// ordered 4,000 KGS at ₹20,00,000.00: ₹16,00,00,00,000.00) shows no amount.
// Step 3's Save refuses it whole, so it is stored the way a step save of
// another field keeps Validate's writes beside the total error (CD-302,
// CD-308; g2 "total limit").
func TestGRNWeb_LineCardOverMaximum(t *testing.T) {
	w := newS1World(t)
	po := purchasetest.NewOrder(t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(
		map[string]string{"item_id": w.id("item", "MS-ANGLE-50"), "uom_id": w.id("uom", "KGS"), "qty": "4000", "price": "2000000"}))
	g := w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}}, doctype.StateDraft)
	in := doc.ActionInput{Version: g.Version, Values: map[string]string{"remarks": "Twice"}, Only: []string{"remarks"},
		Lines: []map[string]string{{"id": strconv.FormatInt(g.Lines[0].ID, 10), "received_qty": "8000"}}}
	if _, err := w.Engine.Do(t.Context(), w.Suresh, goodsreceipt.DocType, g.ID, doctype.ActionSave, in); err != nil {
		t.Fatalf("save 8,000 KGS: %v", err)
	}
	got := s3OK(t, w.get(w.Suresh, stepPath(g.ID, "check")), "check page").s3cCards()
	s3cWant(t, "check page", got, []s3cCard{{no: "1", item: s2Angle, code: "MS-ANGLE-50",
		meta: []string{"Received 8,000 KGS Accepted 8,000 KGS"}}})
}
