package components_test

import (
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestComponents_EscapeUserText(t *testing.T) {
	for _, evil := range []string{`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`} {
		cases := map[string]templ.Component{
			"Banner":      components.Banner(components.BannerProps{Kind: components.Warning, Title: evil, Message: evil}),
			"Warnings":    components.Warnings([]string{evil, evil}),
			"SummaryList": components.SummaryList([]components.Pair{{Label: evil, Value: evil}}),
			"Card":        components.Card(evil, &components.Link{Label: "Change", Href: "/x"}),
			"Chip":        components.FilterBar(components.FilterBarProps{Action: "/x", Chips: []components.Chip{{Label: evil, RemoveURL: "/x"}}, ClearURL: "/x"}),
			"EmptyState":  components.EmptyState(evil, &components.Link{Label: evil, Href: "/x"}),
			"Toast":       components.Toast("success", evil),
			"Pagination":  components.Pagination(components.PageInfo{Page: 1, From: 1, To: 25, Total: 312, Noun: evil}),
		}
		for name, c := range cases {
			doc := uitest.Render(t, c)
			if n := doc.Find("script, img").Length(); n != 0 {
				t.Errorf("%s: input %q became %d element(s)", name, evil, n)
			}
			if name == "Pagination" {
				if !strings.Contains(doc.Find("nav").AttrOr("aria-label", ""), evil) {
					t.Errorf("%s: Noun not kept as text", name)
				}
				continue
			}
			if !strings.Contains(doc.Text(), evil) {
				t.Errorf("%s: text %q not visible", name, evil)
			}
		}
	}
}
