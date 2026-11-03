package expenseweb_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
)

// R14 (design-system §8.2): a 21-line claim (CardsOnly) shows its first 20
// line cards and "Show all 21 lines" on the check page and, once approved,
// the document page; ?all=1 shows all 21 with the 21st the link's
// #line-reveal target.
func TestR14_ClaimShowAll(t *testing.T) {
	t.Parallel()
	e := s2New(t)
	deepak := e.P(t, s2Deepak)
	lines := make([]expensetest.Values, 21)
	draft := e.DraftClaim(t, deepak, nil, lines...)
	approved := e.ApprovedClaim(t, deepak, nil, lines...)
	for _, path := range []string{s2ClaimPath(draft.ID) + "/edit/check", s2ClaimPath(approved.ID)} {
		page := e.page(t, s2Deepak, path)
		if n := page.Find(".linelist li.lines-card").Length(); n != 20 {
			t.Errorf("GET %s: %d cards, want 20", path, n)
		}
		all := page.Find(".linelist a").FilterFunction(func(_ int, s *goquery.Selection) bool {
			return strings.Join(strings.Fields(s.Text()), " ") == "Show all 21 lines"
		})
		href, _ := all.Attr("href")
		if u, err := url.Parse(href); all.Length() != 1 || err != nil || u.Path != path || u.RawQuery != "all=1" || u.Fragment != "line-reveal" {
			t.Errorf("GET %s: %d Show all, href %q; want one to ?all=1#line-reveal", path, all.Length(), href)
		}
		full := e.page(t, s2Deepak, path+"?all=1")
		cards := full.Find(".linelist li.lines-card")
		if cards.Length() != 21 {
			t.Errorf("GET %s?all=1: %d cards, want 21", path, cards.Length())
		}
		if target := full.Find("#line-reveal"); target.Length() != 1 || !target.IsSelection(cards.Eq(20)) || target.AttrOr("tabindex", "") != "-1" {
			t.Errorf("GET %s?all=1: #line-reveal is %d elements, want the 21st card with tabindex -1", path, target.Length())
		}
	}
}
