package print_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
)

// tradeAdditions is the reference PO with every MOD-PO addition filled.
func tradeAdditions() print.TradeDoc {
	d := printtest.ReferencePO()
	d.DeliverTo = []string{"Metro Site Store", "Plot 12, MIDC Andheri East", "Mumbai 400093"}
	d.Refs = []print.DocRef{
		{Label: "Required by", Value: "15-11-2026"},
		{Label: "Payment terms", Value: "Net 30"},
		{Label: "Vendor quote ref", Value: "SQ-2291"},
		{Label: "Earlier PO no.", Value: "PO/2526/0042"},
	}
	d.PlaceReason = "Intra-state: the supplier and the delivery site are both in Maharashtra."
	d.ReverseChargeNote = "Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor."
	return d
}

func pageDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse print: %v", err)
	}
	return doc
}

// MOD-PO §9: the four additions print when set.
func TestPrint_TradeAdditionsGolden(t *testing.T) {
	d := tradeAdditions()
	html := renderTrade(t, d, true)
	printtest.Golden(t, "trade_additions.golden.html", []byte(html))

	doc := pageDoc(t, html)
	if got := doc.Find(".deliver-to h3").Text(); got != "Deliver to" {
		t.Errorf("deliver-to heading = %q, want %q", got, "Deliver to")
	}
	var lines []string
	doc.Find(".deliver-to .address").Each(func(_ int, s *goquery.Selection) { lines = append(lines, s.Text()) })
	if want := strings.Join(d.DeliverTo, "|"); strings.Join(lines, "|") != want {
		t.Errorf("deliver-to lines = %q, want %q", lines, d.DeliverTo)
	}
	var refs []string
	doc.Find(".refs .ref").Each(func(_ int, s *goquery.Selection) { refs = append(refs, s.Text()) })
	wantRefs := []string{"Required by: 15-11-2026", "Payment terms: Net 30", "Vendor quote ref: SQ-2291", "Earlier PO no.: PO/2526/0042"}
	if strings.Join(refs, "|") != strings.Join(wantRefs, "|") {
		t.Errorf("refs = %q, want %q", refs, wantRefs)
	}
	if got := doc.Find(".place-reason").Text(); got != d.PlaceReason {
		t.Errorf("place reason = %q, want %q", got, d.PlaceReason)
	}
	if got := doc.Find(".reverse-charge").Text(); got != d.ReverseChargeNote {
		t.Errorf("reverse-charge note = %q, want %q", got, d.ReverseChargeNote)
	}
}

// FILE-064: each addition is left out entirely, with no empty heading or
// wrapper, when it is empty.
func TestPrint_TradeAdditionsOmittedWhenEmpty(t *testing.T) {
	cases := map[string]struct {
		clear    func(*print.TradeDoc)
		selector string
		text     string
	}{
		"DeliverTo":         {func(d *print.TradeDoc) { d.DeliverTo = nil }, ".deliver-to", "Deliver to"},
		"Refs":              {func(d *print.TradeDoc) { d.Refs = nil }, ".refs", "Vendor quote ref"},
		"PlaceReason":       {func(d *print.TradeDoc) { d.PlaceReason = "" }, ".place-reason", "Intra-state"},
		"ReverseChargeNote": {func(d *print.TradeDoc) { d.ReverseChargeNote = "" }, ".reverse-charge", "Reverse charge"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := tradeAdditions()
			c.clear(&d)
			doc := pageDoc(t, renderTrade(t, d, false))
			if n := doc.Find(c.selector).Length(); n != 0 {
				t.Errorf("%s rendered %d times when empty", c.selector, n)
			}
			if strings.Contains(doc.Text(), c.text) {
				t.Errorf("page text has %q when %s is empty", c.text, name)
			}
			for other, o := range cases {
				if other != name && doc.Find(o.selector).Length() == 0 {
					t.Errorf("%s missing when only %s is empty", o.selector, name)
				}
			}
		})
	}
}

// The place reason and reverse-charge note stay with the totals and terms
// when the page breaks (FILE-057).
func TestPrintCSS_TradeNotesStayTogether(t *testing.T) {
	want := ".place-reason, .reverse-charge { break-inside: avoid; break-after: avoid; }"
	if !strings.Contains(print.CSS, want) {
		t.Errorf("print.CSS missing %q", want)
	}
}
