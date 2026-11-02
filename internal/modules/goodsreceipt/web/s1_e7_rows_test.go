package grnweb_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
)

// A shape only that icon has, from icons_gen.go.
const (
	mapPinGlyph = `path[d^="M20 10c0 4.993"]`
	cartGlyph   = `circle[cx="18"][cy="20"][r="2"]`
)

// UI v2 E7 (UI v2 §5.6; UX doc §5.2, PROPOSED v2): each order radio has the
// bold PO and vendor, a line with a map pin and its site, and a line with a
// bar and "{p} of {n} lines pending · ordered {dd-mm-yyyy}" from the PO's
// own order date; the bar's value and max are the pending and all lines, so
// a partly received order's bar is part full.
// The radios and the toggle sit in one card headed Order to receive with an
// icon, the receipt date in its own card below.
func TestGRNWeb_ChoosePORowsV2(t *testing.T) {
	w := newS1World(t)
	shree := w.po("SHREE", "1200", "50")
	full := w.po("BHARAT", "5")
	w.receiveAll(full)
	if _, err := w.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order SET order_date = '2026-10-20' WHERE id = ?`, full.ID); err != nil {
		t.Fatal(err)
	}

	type row struct{ value, label, site, pin, pending, bar string }
	rows := func(doc *goquery.Document) []row {
		var out []row
		doc.Find(`label.field__choice:has(input[name="purchase_order_id"])`).Each(func(_ int, s *goquery.Selection) {
			lines := s.Find(".choice-line")
			text := s.Find(".field__choice-text").Clone()
			text.Find(".choice-line").Remove()
			bar := lines.Find("progress")
			out = append(out, row{
				value:   s.Find("input").AttrOr("value", ""),
				label:   strings.TrimSpace(text.Text()),
				site:    strings.TrimSpace(lines.Eq(0).Text()),
				pin:     strconv.Itoa(lines.Eq(0).Children().Filter("svg.icon:has("+mapPinGlyph+")").Length()) + strconv.Itoa(s.Find("svg.icon").Length()),
				pending: strings.TrimSpace(lines.Eq(1).Text()),
				bar:     bar.AttrOr("value", "") + "/" + bar.AttrOr("max", "") + " " + bar.AttrOr("aria-hidden", "") + " " + strconv.Itoa(lines.Eq(1).Find("progress").Length()),
			})
		})
		return out
	}

	r := w.get(w.Suresh, s1New)
	want := []row{{strconv.FormatInt(shree.ID, 10), "PO/26-27/00001 · Shree Steel Traders", "METRO-SITE", "11", "2 of 2 lines pending · ordered 24-10-2026", "2/2 true 1"}}
	if got := rows(r.doc); len(got) != 1 || got[0] != want[0] {
		t.Errorf("rows = %+v, want %+v", got, want)
	}
	card := r.doc.Find(`section.card:has(input[name="purchase_order_id"])`)
	if card.Length() != 1 || card.Find(`a:contains("Show fully received orders")`).Length() != 1 || card.Find(`input[name="receipt_date"]`).Length() != 0 {
		t.Errorf("want one card with the radios and the toggle and without the receipt date")
	}
	if h := card.Find("h2.card-title"); h.Length() != 1 || h.Text() != "Order to receive" || card.Find(".card-icon svg.icon:has("+cartGlyph+")").Length() != 1 {
		t.Errorf("order card heading %q, %d icons, want Order to receive with the shopping-cart icon", h.Text(), card.Find(".card-icon svg.icon:has("+cartGlyph+")").Length())
	}
	if n := r.doc.Find(`section.card:has(input[name="receipt_date"])`).Length(); n != 1 {
		t.Errorf("%d cards hold the receipt date, want 1", n)
	}

	r = w.get(w.Suresh, s1New+"?all=1")
	want = []row{
		{strconv.FormatInt(full.ID, 10), full.Number + " · Bharat Tools", "METRO-SITE", "11", "Fully received · ordered 20-10-2026", "0/1 true 1"},
		{strconv.FormatInt(shree.ID, 10), "PO/26-27/00001 · Shree Steel Traders", "METRO-SITE", "11", "2 of 2 lines pending · ordered 24-10-2026", "2/2 true 1"},
	}
	got := rows(r.doc)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("widened rows =\n%+v\nwant\n%+v", got, want)
	}
	if n := r.doc.Find(`section.card:has(input[name="purchase_order_id"]) input[type="hidden"][name="all"]`).Length(); n != 1 {
		t.Errorf("the order card carries all=1 %d times, want 1", n)
	}

	part := w.po("SHREE", "10", "5", "3")
	w.GRN(t, w.Suresh, part.ID, []grntest.Line{{POLineID: part.Lines[0].ID, Received: "10"}, {POLineID: part.Lines[1].ID, Received: "2"}}, doctype.StateApproved)
	r = w.get(w.Suresh, s1New)
	want = []row{
		{strconv.FormatInt(shree.ID, 10), "PO/26-27/00001 · Shree Steel Traders", "METRO-SITE", "11", "2 of 2 lines pending · ordered 24-10-2026", "2/2 true 1"},
		{strconv.FormatInt(part.ID, 10), part.Number + " · Shree Steel Traders", "METRO-SITE", "11", "2 of 3 lines pending · ordered 24-10-2026", "2/3 true 1"},
	}
	got = rows(r.doc)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("rows with a partly received order =\n%+v\nwant\n%+v", got, want)
	}
}
