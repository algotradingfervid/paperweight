package docscreen_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"
)

// M0-40b review (CD-256 (7), CD-313): with NoLinePages the Items step's
// cards have no line actions, so neither has the display grid of a module
// that also sets LineGrid: no Change, Remove or Move, no link or form to a
// line page.
func TestLineGrid_DisplayNoLinePages(t *testing.T) {
	m := m40bModule(&m40bCalls{})
	m.NoLinePages = true
	e, id := m01bHooked(t, m)
	page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/items"), "GET items")
	ed := page.Find(".linelist.linelist--grid .line-editor")
	if ed.Length() != 1 || ed.Find(".linegrid-row").Length() != 2 {
		t.Fatalf("%d display grids with %d rows, want one with 2", ed.Length(), ed.Find(".linegrid-row").Length())
	}
	ed.Find("a, button, form").Each(func(_ int, s *goquery.Selection) {
		t.Errorf("the display grid has <%s> %q (%q), want no line actions with NoLinePages", goquery.NodeName(s), norm(s.Text()), s.AttrOr("aria-label", ""))
	})
	if n := page.Find(`a[href*="/lines/"], form[action*="/lines/"]`).Length(); n != 0 {
		t.Errorf("the Items step has %d links or forms to a line page, want none (CD-256 (7))", n)
	}
}
