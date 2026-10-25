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
			"DataList": components.DataList(components.DataListProps{
				Label: "Purchase orders",
				Columns: []components.Column{
					{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
					{Key: "date", Label: "Date", Priority: components.PMeta},
					{Key: "by", Label: "Raised by", Priority: components.PLow},
				},
				Rows: []components.Row{{Href: "/x", Title: evil, Party: evil, Values: map[string]string{"amount": evil, "date": evil, "by": evil}}},
			}),
			"LineList": components.LineList(components.LineListProps{
				Cards: []components.LineCard{{No: 1, Item: evil, Code: evil, QtyRate: "1 kg × ₹1.00", Amount: "₹1.00", Tax: "GST 18%"}},
				Total: 1, TaxableTotal: "₹1.00",
			}),
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
			if name == "DataList" || name == "LineList" {
				if n := strings.Count(doc.Text(), evil); n < map[string]int{"DataList": 5, "LineList": 2}[name] {
					t.Errorf("%s: %q shown %d times, want every user value as text", name, evil, n)
				}
			}
		}
	}
}
