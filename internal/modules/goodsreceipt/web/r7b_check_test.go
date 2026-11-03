package grnweb_test

import (
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/types"
)

// r7bSection is the check page's section titled title: its status badge and
// the problems it lists.
func r7bSection(p s1Page, title string) (status string, listed []string) {
	sec := p.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s2Norm(s.Find(".card-title").First().Text()) == title
	})
	listed = sec.Find("ul > li").Map(func(_ int, s *goquery.Selection) string { return s2Norm(s.Text()) })
	return s2Norm(sec.Find(".check-status").Text()), listed
}

// R7b (GRN M1): the check page names what submit will refuse beyond DOC's
// own checks, in the section of the step that fixes it: a goods receipt with
// no challan or invoice no. and no kind marks Delivery Needs attention, and a
// rejected quantity with no reason marks Quantities, each listing the submit
// refusal's own text, never as a top warning. The same receipt with both
// filled in is Complete in both; a period lock after its receipt date then
// marks Order, the receipt date's step, with the lock's text.
func TestR7b_CheckSubmitOnlyProblems(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.s2RefPO())
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "410"}, s2Set{"rejected", 1, "10"}, s2Set{"reason", 1, ""}),
		"/edit/check", "save 410 with 10 rejected, no reason")
	check := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page")
	for _, c := range []struct {
		title string
		want  []string
	}{
		{"Delivery", []string{"Challan or invoice no. is required for goods.", "Choose Delivery challan or Tax invoice."}},
		{"Quantities", []string{"Line 1: Reason for rejecting is required."}},
	} {
		status, listed := r7bSection(check, c.title)
		if status != "Needs attention" {
			t.Errorf("%s = %q, want Needs attention", c.title, status)
		}
		if !slices.Equal(listed, c.want) {
			t.Errorf("%s lists %q, want %q", c.title, listed, c.want)
		}
		for _, msg := range c.want {
			if check.s3Has("warning", msg) {
				t.Errorf("a top warning repeats %q", msg)
			}
		}
	}

	w.s2Delivery(id)
	s2WantStep(t, w.s2Save(id, w.s2Items(id), s2Set{"received", 1, "410"}, s2Set{"rejected", 1, "10"}, s2Set{"reason", 1, "Bent pieces"}),
		"/edit/check", "save with the reason")
	check = s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page, filled in")
	for _, title := range []string{"Delivery", "Quantities"} {
		if status, listed := r7bSection(check, title); status != "Complete" || len(listed) != 0 {
			t.Errorf("filled in: %s = %q listing %q, want Complete and nothing", title, status, listed)
		}
	}

	// The period lock, which BeforeSubmit also refuses, marks the step with
	// the receipt date, in the lock's own text.
	w.Lock(t, types.Date{Year: 2026, Month: time.October, Day: 31})
	check = s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page, locked")
	lock := "Goods Receipt Draft " + strconv.FormatInt(id, 10) + " is dated " + s1Today + ", which is locked. The period is locked up to 31-10-2026."
	if status, listed := r7bSection(check, "Order"); status != "Needs attention" || !slices.Equal(listed, []string{lock}) {
		t.Errorf("locked: Order = %q listing %q, want Needs attention listing %q", status, listed, lock)
	}
	for _, title := range []string{"Delivery", "Quantities"} {
		if status, _ := r7bSection(check, title); status != "Complete" {
			t.Errorf("locked: %s = %q, want Complete", title, status)
		}
	}
}

// R7b round 2: on a second receipt the cards show PO line numbers that
// differ from the lines' positions. A reason missing on PO lines 2 and 3
// (positions 1 and 2) links to #line-1 and #line-2, whose cards show PO
// lines 2 and 3: the link names the position, never the number shown.
func TestR7b_CheckLinksOnASecondReceipt(t *testing.T) {
	w := newS1World(t)
	po := w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "100"}, s2Line{"WIRE-18G", "KGS", "50"}, s2Line{"BOLT-M12", "NOS", "10"})
	w.s2Approve(po, map[int]string{1: "100"})
	id := w.draft(po)
	w.s2Delivery(id)
	s2WantStep(t, w.s2Save(id, w.s2Items(id),
		s2Set{"received", 2, "50"}, s2Set{"rejected", 2, "5"}, s2Set{"reason", 2, ""},
		s2Set{"received", 3, "10"}, s2Set{"rejected", 3, "2"}, s2Set{"reason", 3, ""}),
		"/edit/check", "save PO lines 2 and 3 with rejects and no reasons")
	check := s3OK(t, w.get(w.Suresh, stepPath(id, "check")), "check page")
	sec := check.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s2Norm(s.Find(".card-title").First().Text()) == "Quantities"
	})
	for _, c := range []struct{ msg, href, shows string }{
		{"Line 2: Reason for rejecting is required.", "#line-1", "2"},
		{"Line 3: Reason for rejecting is required.", "#line-2", "3"},
	} {
		a := sec.Find("ul > li a").FilterFunction(func(_ int, s *goquery.Selection) bool { return s2Norm(s.Text()) == c.msg })
		if got := a.AttrOr("href", ""); got != c.href {
			t.Errorf("%q links to %q, want %q", c.msg, got, c.href)
			continue
		}
		card := check.doc.Find(c.href)
		if card.Length() != 1 || s2Norm(card.Find(".lines-no").Text()) != c.shows {
			t.Errorf("%s: %d cards, showing %q; want the card of PO line %s", c.href, card.Length(), s2Norm(card.Find(".lines-no").Text()), c.shows)
		}
	}
	if n := check.doc.Find("#line-3").Length(); n != 0 {
		t.Errorf("%d #line-3 on a two-line receipt, want none", n)
	}
}
