package grnweb_test

import (
	"net/url"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// R14 (design-system §8.2): a 21-line GRN's check page and document page
// show its first 20 line cards and "Show all 21 lines"; ?all=1 shows all
// 21, the 21st by place the link's #line-reveal target, though GRN's cards
// carry their PO line numbers (posted in reverse, so the 21st card is PO
// line 1).
func TestGRNWeb_R14ShowAll(t *testing.T) {
	w := newS1World(t)
	items, uoms := []string{"WIRE-18G", "BOLT-M12", "MS-ANGLE-50"}, []string{"KGS", "NOS", "KGS"}
	var lines []map[string]string
	for i := range 21 {
		lines = append(lines, map[string]string{"item_id": w.id("item", items[i%3]), "uom_id": w.id("uom", uoms[i%3]), "qty": "10", "price": "25"})
	}
	po := purchasetest.NewOrder(t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(lines...))
	var posted []grntest.Line
	for i := 20; i >= 0; i-- {
		posted = append(posted, grntest.Line{POLineID: po.Lines[i].ID, Received: "1"})
	}
	id := w.GRN(t, w.Suresh, po.ID, posted, doctype.StateDraft).ID
	check := func(path string) {
		t.Helper()
		page := s3OK(t, w.get(w.Suresh, path), "GET "+path)
		cards := page.s3cCards()
		if len(cards) != 20 || cards[0].no != "21" || cards[19].no != "2" {
			t.Errorf("GET %s: %d cards, first %+v; want 20, PO lines 21 down to 2", path, len(cards), cards[0])
		}
		all := page.doc.Find(".linelist a").FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Norm(s.Text()) == "Show all 21 lines" })
		href, _ := all.Attr("href")
		if u, err := url.Parse(href); all.Length() != 1 || err != nil || u.Path != path || u.RawQuery != "all=1" || u.Fragment != "line-reveal" {
			t.Errorf("GET %s: %d Show all, href %q; want one to ?all=1#line-reveal", path, all.Length(), href)
		}
		full := s3OK(t, w.get(w.Suresh, path+"?all=1"), "GET "+path+"?all=1")
		if n := len(full.s3cCards()); n != 21 {
			t.Errorf("GET %s?all=1: %d cards, want 21", path, n)
		}
		target := full.doc.Find("#line-reveal")
		if target.Length() != 1 || s2Norm(target.Find(".lines-no").Text()) != "1" || target.AttrOr("tabindex", "") != "-1" {
			t.Errorf("GET %s?all=1: #line-reveal = %d, %q; want the 21st card (PO line 1), tabindex -1", path, target.Length(), s2Norm(target.Text()))
		}
	}
	check(stepPath(id, "check"))
	w.s3Submit(id)
	check(s3Doc(id))
}
