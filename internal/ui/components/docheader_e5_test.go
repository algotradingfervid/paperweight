package components_test

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The document header (UI v2 §5.13 item 3, N14) is one card: the h1 the
// layout would have drawn (id page-title, focusable), the state badge, the
// amount with its words, the key facts as a list of label and value pairs,
// and the progress list last.
func TestDocHeader_Parts(t *testing.T) {
	doc := uitest.Render(t, components.DocHeader(components.DocHeaderProps{
		Title: "PO/26-27/00041", State: doctype.StatePending,
		Amount: "₹2,83,640.00", InWords: "Rupees Two Lakh Eighty Three Thousand Six Hundred Forty Only",
		Facts: []components.Pair{{Label: "Vendor", Value: "Shree Steel Traders"}, {Label: "Order date", Value: "30-10-2026", Num: true}},
		Steps: []components.LifeStep{{Label: "Draft", Mark: components.LifeDone}, {Label: "Pending approval", Mark: components.LifeCurrent}},
	}))
	card := doc.Find("section.card .doc-head")
	if card.Length() != 1 {
		t.Fatalf("want one .doc-head inside a card, got %d", card.Length())
	}
	h1 := card.Find("h1")
	if h1.Length() != 1 || h1.AttrOr("id", "") != "page-title" || h1.AttrOr("tabindex", "") != "-1" || squash(h1.Text()) != "PO/26-27/00041" {
		t.Errorf("h1 = %d id=%q tabindex=%q %q, want one #page-title tabindex -1 PO/26-27/00041",
			h1.Length(), h1.AttrOr("id", ""), h1.AttrOr("tabindex", ""), squash(h1.Text()))
	}
	if got := squash(card.Find(".doc-head__top .badge--pending_approval").Text()); got != "Pending approval" {
		t.Errorf("state badge beside the h1 = %q, want Pending approval", got)
	}
	if got := card.Find("p.doc-head__amount.num").Text(); got != "₹2,83,640.00" {
		t.Errorf("amount = %q", got)
	}
	if got := card.Find("p.in-words").Text(); got != "Rupees Two Lakh Eighty Three Thousand Six Hundred Forty Only" {
		t.Errorf("in words = %q", got)
	}
	var facts []string
	card.Find("dl.doc-head__facts > div").Each(func(_ int, s *goquery.Selection) {
		facts = append(facts, s.Find("dt").Text()+"="+s.Find("dd").Text()+"/"+s.Find("dd").AttrOr("class", ""))
	})
	if got, want := facts, []string{"Vendor=Shree Steel Traders/", "Order date=30-10-2026/num"}; !slices.Equal(got, want) {
		t.Errorf("facts = %q, want %q", got, want)
	}
	if !card.Children().Last().Is("ol.lifecycle") {
		t.Error("the progress list is not the header's last part")
	}
}

// A per-DocType state label replaces the badge's word; no amount, words or
// facts render no empty elements; no steps render no list.
func TestDocHeader_Optional(t *testing.T) {
	doc := uitest.Render(t, components.DocHeader(components.DocHeaderProps{Title: "Draft 57", State: doctype.StateApproved, StateLabel: "Submitted"}))
	if got := squash(doc.Find(".doc-head__top .badge--approved").Text()); got != "Submitted" {
		t.Errorf("badge = %q, want Submitted", got)
	}
	for _, sel := range []string{".doc-head__amount", ".in-words", ".doc-head__facts", ".lifecycle"} {
		if n := doc.Find(sel).Length(); n != 0 {
			t.Errorf("%s: %d elements, want none", sel, n)
		}
	}
	// Amount without words: the figure only.
	doc = uitest.Render(t, components.DocHeader(components.DocHeaderProps{Title: "Draft 57", State: doctype.StateDraft, Amount: "₹1.00"}))
	if doc.Find(".doc-head__amount").Length() != 1 || doc.Find(".in-words").Length() != 0 {
		t.Error("an amount without words: want the figure and no words line")
	}
}
