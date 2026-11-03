package docscreen_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m40Modules are Hooked's two row sizes: with no Hide its row has 4 cells
// (project, item, qty, note), which the grid doesn't lay out; hiding note
// leaves 3, which it does (CD-315).
var m40Modules = []struct {
	name string
	m    docscreen.Module
	grid bool
}{
	{"4 cells", docscreen.Module{}, false},
	{"3 cells", docscreen.Module{Hide: m01bHide("note")}, true},
}

// M0-40, CD-315: through the public routes, a row of 4 cells shows the
// cards with no grid on the items step, a grid row save answers as the
// line page does, and a line error links to its card (RR1a). The 3-cell
// Module is the control, so each case sees the grid when it should.
func TestLineGrid_FallbackOnEveryPath(t *testing.T) {
	for _, c := range m40Modules {
		t.Run(c.name+" GET items", func(t *testing.T) {
			e, id := m01bHooked(t, c.m)
			page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/items"), "GET items")
			n := page.Find(`[id^="grid-1-"], .linegrid-row`).Length()
			if (n > 0) != c.grid {
				t.Errorf("GET items: %d grid inputs and rows, want the grid %v", n, c.grid)
			}
		})
		t.Run(c.name+" htmx POST line", func(t *testing.T) {
			e, id := m01bHooked(t, c.m)
			base := m01bURL(id)
			rec := e.post(t, base+"/lines/1", e.m01bLine1(m01bVersion(t, e, id)), "HX-Request", "true")
			if c.grid {
				if rec.Code != http.StatusOK || uitest.Doc(t, rec).Find("#grid-row-1").Length() != 1 {
					t.Errorf("grid row save = %d, want 200 with #grid-row-1; body %.300s", rec.Code, rec.Body)
				}
				return
			}
			if loc := rec.Header().Get("HX-Redirect"); rec.Code != http.StatusOK || !strings.HasSuffix(loc, "/edit/items") {
				t.Errorf("line save = %d to %q, want HX-Redirect to the items step", rec.Code, loc)
			}
		})
		t.Run(c.name+" line error link", func(t *testing.T) {
			e, id := m01bHooked(t, c.m)
			e.fx.Exec(t, `UPDATE docscreentest_hooked_line SET qty = 0 WHERE parent_id = ? AND line_no = 2`, id)
			rec := e.post(t, m01bURL(id)+"/submit", url.Values{"version": {m02Version(t, e, id)}})
			page := m02Page(t, rec, http.StatusUnprocessableEntity, "POST submit with line 2 quantity 0")
			if got := norm(page.Find("h2#step-title").Text()); got != "Items" {
				t.Fatalf("heading = %q, want the submit to land on Items", got)
			}
			var hrefs []string
			page.Find(".error-summary a").Each(func(_ int, s *goquery.Selection) { hrefs = append(hrefs, s.AttrOr("href", "")) })
			want := "#line-2" // the line's card (R7b, RR1a)
			if c.grid {
				want = "#line-editor"
			}
			if !slices.Contains(hrefs, want) || slices.Contains(hrefs, "#line-editor") != c.grid {
				t.Errorf("error summary links = %q, want the line error linked to %s", hrefs, want)
			}
		})
	}
}
