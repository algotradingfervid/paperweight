package grnweb_test

import (
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"
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
// filled in is Complete in both.
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
}
