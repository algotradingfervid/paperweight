package app_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-326 (7), UX §5.12: the advance close note on what's left is a warning
// banner; with nothing left the page has the hint alone and no banner.
func TestAdv_CloseNoteWarns(t *testing.T) {
	e := newExpWorld(t)
	adv := e.advance("10000.00")
	e.paid(adv.ID)
	e.claim(adv.ID, "8500.00", false, "8500.00", "2026-10-24")
	empty := e.advance("500.00")
	e.paid(empty.ID)
	e.claim(empty.ID, "500.00", false, "500.00", "2026-10-24")
	meena := e.signedIn(meenaEmail)

	r := meena.get(docPath(expense.AdvanceDocType, adv.ID) + "/close")
	note := r.doc.Find(".banner").FilterFunction(func(_ int, b *goquery.Selection) bool {
		return strings.Contains(b.Text(), "is left on this advance")
	})
	if note.Length() != 1 || !note.HasClass("banner--warning") {
		t.Errorf("close note: %d banners, classes %q; want one warning banner", note.Length(), note.AttrOr("class", ""))
	}

	r = meena.get(docPath(expense.AdvanceDocType, empty.ID) + "/close")
	shown := r.doc.Find(".banner").FilterFunction(func(_ int, b *goquery.Selection) bool {
		return b.ParentsFiltered("template").Length() == 0 // not the notice templates
	})
	if shown.Length() != 0 {
		t.Errorf("close, nothing left: %d banners (%q), want the hint alone", shown.Length(), shown.Text())
	}
}

// CD-326 (4): the claim check page's details leave out the fields its
// totals show (Advance, Less advance, Total, To pay), which the totals
// still show; without an advance there are no totals, and the details keep
// Total and To pay.
func TestS1_CheckTotalsCoverDetails(t *testing.T) {
	s := s1New(t)
	deepak := s.principal(deepakID)
	adv := s.advance(deepak, s.principal(priyaID), "4000.00", true)
	c := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(adv, 10), "less_advance": "300.00"},
		map[string]string{"amount": "450.00"})
	plain := s.claim(deepak, nil, nil)
	dk := s.signedIn(deepakEmail)

	r := s1Page(t, dk, s1URL(c.ID, "/edit/check"))
	details, totals := m053Labels(r, true), m053Labels(r, false)
	for _, l := range []string{"Advance", "Less advance", "Total", "To pay"} {
		if strings.Contains(details, "|"+l+"|") {
			t.Errorf("with an advance: the details summarise %s (%s)", l, details)
		}
	}
	if want := "|Total|Less advance (EA/26-27/00001)|To pay|"; totals != want {
		t.Errorf("with an advance: totals %s, want %s", totals, want)
	}

	r = s1Page(t, dk, s1URL(plain.ID, "/edit/check"))
	if details := m053Labels(r, true); !strings.Contains(details, "|Total|To pay|") {
		t.Errorf("no advance: details %s, want Total and To pay", details)
	}
}

// m053Labels are r's summary labels inside the step sections (inCard) or
// outside them, as "|a|b|".
func m053Labels(r *response, inCard bool) string {
	labels := "|"
	r.doc.Find(".summary-row dt").Each(func(_ int, dt *goquery.Selection) {
		if (dt.ParentsFiltered("section.card").Length() > 0) == inCard {
			labels += strings.TrimSpace(dt.Text()) + "|"
		}
	})
	return labels
}
