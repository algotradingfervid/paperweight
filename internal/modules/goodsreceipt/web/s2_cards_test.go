package grnweb_test

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-GRN S2, step 3 Received quantities: the cards (UX doc §5.4 Cards;
// GRN spec §11; CD-282 Q3, Q4; CD-286 R1, R4). Suresh on the real
// auth.Service (CD-213).

const s2ReasonHint = "For example, Bent pieces or Wrong size. Printed on the GRN."

// GUX-009 (spec §11; UX §5.4): after step 1 on the reference PO with 800
// KGS of line 1 already received on an approved GRN, step 3 shows one card
// per PO line in PO line order, titled "{n} · {item name}", with the item
// code, Received now (label, hint "Ordered … · Pending …" read with it,
// prefilled with pending) in the 56 px QtyInput with the UoM as suffix and
// a decimal keypad for KGS, the accessible name "Received now, line {n},
// {item name}", and M0-26's per-line id for the stored line (R1); a closed
// Some rejected disclosure with Rejected and Reason for rejecting (max 200,
// its hint), each with its own accessible name. Hidden fields aren't
// rendered (CD-319) and no id repeats.
func TestGRNWeb_ItemsCards(t *testing.T) {
	w := newS1World(t)
	po := w.s2RefPO()
	w.s2Approve(po, map[int]string{1: "800"})
	id := w.draft(po)
	r := w.s2Items(id)

	if got := s2Norm(r.doc.Find("h2#step-title").Text()); got != "Received quantities" {
		t.Errorf("h1 = %q, want Received quantities", got)
	}
	if got, want := r.s2Titles(), []string{"1 · " + s2Angle, "2 · " + s2Wire}; !slices.Equal(got, want) {
		t.Fatalf("card titles = %q, want %q", got, want)
	}
	for _, c := range []struct {
		n                         int
		code, item, hint, prefill string
	}{
		{1, "MS-ANGLE-50", s2Angle, "Ordered 1,200 · Pending 400 KGS", "400"},
		{2, "WIRE-18G", s2Wire, "Ordered 50 · Pending 50 KGS", "50"},
	} {
		card := r.s2Card(t, c.n)
		if !strings.Contains(s2Norm(card.Text()), c.code) {
			t.Errorf("card %d has no item code %s", c.n, c.code)
		}
		in := r.s2Input("received", c.n)
		inID := in.AttrOr("id", "")
		if in.Length() != 1 || card.Find(`[id="`+inID+`"]`).Length() != 1 {
			t.Fatalf("card %d: %d Received now inputs, or it isn't inside the card", c.n, in.Length())
		}
		if got, want := in.AttrOr("aria-label", ""), "Received now, line "+strconv.Itoa(c.n)+", "+c.item; got != want {
			t.Errorf("card %d aria-label = %q, want %q", c.n, got, want)
		}
		if want := docscreen.LineFieldID(c.n, "received_qty"); inID != want {
			t.Errorf("card %d Received now id = %q, want %q (M0-26 per-line id of stored line %d)", c.n, inID, want, c.n)
		}
		if got := in.AttrOr("value", ""); got != c.prefill {
			t.Errorf("card %d Received now = %q, want the pending %q", c.n, got, c.prefill)
		}
		if got := s2Norm(card.Find(`label[for="` + inID + `"]`).Text()); got != s2Received+" * (required)" {
			t.Errorf("card %d label = %q, want %s * (required) (CD-341 (2))", c.n, got, s2Received)
		}
		if got := s2Norm(card.Find(`[id="` + inID + `-hint"]`).Text()); got != c.hint {
			t.Errorf("card %d hint = %q, want %q", c.n, got, c.hint)
		}
		if !strings.Contains(in.AttrOr("aria-describedby", ""), inID+"-hint") {
			t.Errorf("card %d: the hint isn't read with the input (aria-describedby %q)", c.n, in.AttrOr("aria-describedby", ""))
		}
		if in.Closest(".field__control--lg").Length() != 1 {
			t.Errorf("card %d: Received now isn't the 56 px LargeQtyInput (CD-282 Q4)", c.n)
		}
		if got := s2Norm(in.Closest(".field__control--lg").Find(".field__suffix").Text()); got != "KGS" {
			t.Errorf("card %d suffix = %q, want KGS", c.n, got)
		}
		if got := in.AttrOr("inputmode", ""); got != "decimal" {
			t.Errorf("card %d inputmode = %q, want decimal for KGS", c.n, got)
		}

		some := s2Disclosure(card, "Some rejected")
		if some.Length() != 1 || s2Open(some) {
			t.Errorf("card %d: %d Some rejected disclosures, open %v; want one, closed", c.n, some.Length(), s2Open(some))
		}
		for _, k := range []string{"rejected", "reason"} {
			f := r.s2Input(k, c.n)
			if f.Length() != 1 || some.Find(`[id="`+f.AttrOr("id", "x")+`"]`).Length() != 1 {
				t.Errorf("card %d: %s isn't one input inside Some rejected", c.n, s2Labels[k])
			}
			if got, want := f.AttrOr("aria-label", ""), s2Labels[k]+", line "+strconv.Itoa(c.n)+", "+c.item; got != want {
				t.Errorf("card %d %s aria-label = %q, want %q", c.n, k, got, want)
			}
		}
		reason := r.s2Input("reason", c.n)
		if got := reason.AttrOr("maxlength", ""); got != "200" {
			t.Errorf("card %d reason maxlength = %q, want 200", c.n, got)
		}
		if got := s2Norm(card.Find(`[id="` + reason.AttrOr("id", "x") + `-hint"]`).Text()); got != s2ReasonHint {
			t.Errorf("card %d reason hint = %q, want %q", c.n, got, s2ReasonHint)
		}
	}
	if d := r.doc.Find(`details.disclosure summary:contains("Fully received lines")`); d.Length() != 0 {
		t.Errorf("no line is fully received, yet the step has %q", d.Text())
	}
	s2WantNoHidden(t, r, "step 3")
	s2WantUniqueIDs(t, r, "step 3")
}

// GUX-010 (CD-282 Q3; UX §5.4 Fully received lines): on a PO with lines 2,
// 4 and 5 fully received, the pending cards 1 and 3 come first, open, then
// a closed "Fully received lines (3)" disclosure holding cards 2, 4 and 5
// with blank inputs and "Pending 0". A value typed into a fully received
// card is saved (it warns, never refuses: a second truck), and the
// disclosure then opens and the card warns (spec §4: a fully received line
// warns on any accepted quantity). On a PO with no pending line every card
// shows open, in line order, with no disclosure.
func TestGRNWeb_ItemsFullyReceived(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "10"}, s2Line{"WIRE-18G", "KGS", "20"}, s2Line{"MS-ANGLE-50", "KGS", "30"},
		s2Line{"WIRE-18G", "KGS", "40"}, s2Line{"MS-ANGLE-50", "KGS", "50"})
	w.s2Approve(po, map[int]string{2: "20", 4: "40", 5: "50"})
	id := w.draft(po)
	r := w.s2Items(id)

	want := []string{"1 · " + s2Angle, "3 · " + s2Angle, "2 · " + s2Wire, "4 · " + s2Wire, "5 · " + s2Angle}
	if got := r.s2Titles(); !slices.Equal(got, want) {
		t.Fatalf("card titles = %q, want %q (pending first, then fully received, each in PO line order)", got, want)
	}
	full := s2Disclosure(r.doc.Find("#wizard-form"), "Fully received lines (3)")
	if full.Length() != 1 || s2Open(full) {
		t.Fatalf("%d Fully received lines (3) disclosures, open %v; want one, closed", full.Length(), s2Open(full))
	}
	inside := func(n int) bool {
		return full.Find(`[id="`+r.s2Input("received", n).AttrOr("id", "x")+`"]`).Length() == 1
	}
	for _, c := range []struct {
		n         int
		hint, val string
		inside    bool
	}{
		{1, "Ordered 10 · Pending 10 KGS", "10", false},
		{3, "Ordered 30 · Pending 30 KGS", "30", false},
		{2, "Ordered 20 · Pending 0 KGS", "", true},
		{4, "Ordered 40 · Pending 0 KGS", "", true},
		{5, "Ordered 50 · Pending 0 KGS", "", true},
	} {
		in := r.s2Input("received", c.n)
		if in.Length() != 1 {
			t.Errorf("card %d has %d Received now inputs", c.n, in.Length())
			continue
		}
		if got := in.AttrOr("value", ""); got != c.val {
			t.Errorf("card %d Received now = %q, want %q", c.n, got, c.val)
		}
		if got := s2Norm(r.doc.Find(`[id="` + in.AttrOr("id", "") + `-hint"]`).Text()); got != c.hint {
			t.Errorf("card %d hint = %q, want %q", c.n, got, c.hint)
		}
		if inside(c.n) != c.inside {
			t.Errorf("card %d inside Fully received lines = %v, want %v", c.n, inside(c.n), c.inside)
		}
	}
	s2WantUniqueIDs(t, r, "step 3 with fully received lines")

	if res := w.s2Save(id, r, s2Set{"received", 2, "5"}); res.status != http.StatusSeeOther || !strings.HasSuffix(res.location, "/edit/check") {
		t.Fatalf("save 5 on fully received line 2 = %d %q %q, want 303 to Check and submit (a warning never refuses)", res.status, res.location, res.errorSummary())
	}
	ids := w.s2LineIDs(id, po)
	if _, ok := ids[2]; !ok || len(ids) != 3 {
		t.Errorf("stored lines by PO line = %v, want 1, 2 and 3", ids)
	}
	r = w.s2Items(id)
	if full = s2Disclosure(r.doc.Find("#wizard-form"), "Fully received lines (3)"); !s2Open(full) {
		t.Errorf("Fully received lines (3) is closed while card 2 inside it has a value")
	}
	warn := "Line 2: Accepted 5 KGS is more than the 0 KGS pending on " + po.Number + "."
	if got := s2Norm(r.s2Card(t, 2).Find(".banner--warning").Text()); !strings.Contains(got, warn) {
		t.Errorf("card 2 warning = %q, want %q", got, warn)
	}

	none := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "7"}, s2Line{"WIRE-18G", "KGS", "8"})
	w.s2Approve(none, map[int]string{1: "7", 2: "8"})
	r = w.s2Items(w.draft(none))
	if got, want := r.s2Titles(), []string{"1 · " + s2Angle, "2 · " + s2Wire}; !slices.Equal(got, want) {
		t.Errorf("fully received PO: card titles = %q, want %q", got, want)
	}
	if d := r.doc.Find(`details.disclosure summary:contains("Fully received lines")`); d.Length() != 0 {
		t.Errorf("fully received PO: the cards sit in %q, want every card open", s2Norm(d.Text()))
	}
	for n := 1; n <= 2; n++ {
		in := r.s2Input("received", n)
		if in.Length() != 1 || in.AttrOr("value", "x") != "" {
			t.Errorf("fully received PO: card %d has %d inputs, value %q; want one, blank", n, in.Length(), in.AttrOr("value", "x"))
		}
	}
}

// CD-256 (7), UX §5.4: with NoLinePages the GRN's line URLs answer 404 and
// step 3 links to none of them; lines come only from the cards.
func TestGRNWeb_ItemsNoLinePages(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.s2RefPO())
	base := "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10)
	for _, p := range []string{"/lines/1", "/lines/new", "/lines/1/remove"} {
		if r := w.get(w.Suresh, base+p); r.status != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, r.status)
		}
	}
	r := w.s2Items(id)
	if n := r.doc.Find(`a[href*="/lines/"]`).Length(); n != 0 {
		t.Errorf("step 3 has %d links to line pages", n)
	}
	if len(r.s2Cards()) != 2 {
		t.Errorf("step 3 has %d cards, want 2", len(r.s2Cards()))
	}
}

// R4 (CD-282, CD-286), the HTTP half of GUX-012: the step form's first
// submit button, the one a browser presses on Enter (the keypad's Go key),
// is the copy of Save and continue, before Receive all pending and Clear
// all, and posts no fill. Pressing Enter in a Received now input is S6's
// browser test.
func TestGRNWeb_ItemsEnterPrimary(t *testing.T) {
	w := newS1World(t)
	r := w.s2Items(w.draft(w.s2RefPO()))
	var submits []*goquery.Selection
	r.doc.Find("#wizard-form button, #wizard-form input[type=submit]").Each(func(_ int, b *goquery.Selection) {
		if typ := b.AttrOr("type", "submit"); typ == "submit" {
			submits = append(submits, b)
		}
	})
	labels := make([]string, len(submits))
	for i, b := range submits {
		labels[i] = s2Norm(b.Text())
	}
	fill, clear := slices.Index(labels, "Receive all pending"), slices.Index(labels, "Clear all")
	if fill < 0 || clear < 0 {
		t.Fatalf("step form submits = %q, want Receive all pending and Clear all among them", labels)
	}
	first := submits[0]
	if !first.HasClass("enter-submit") || first.AttrOr("formaction", "") != "" || first.AttrOr("value", "") == "stay" {
		t.Errorf("the form's first submit = %q (class %q, formaction %q, value %q), want the unseen copy of Save and continue (R4, components.EnterSubmit)",
			labels[0], first.AttrOr("class", ""), first.AttrOr("formaction", ""), first.AttrOr("value", ""))
	}
	if !slices.Contains(labels[max(fill, clear):], "Save and continue") {
		t.Errorf("step form submits = %q, want Save and continue in the bar after the fill buttons", labels)
	}
}
