package components_test

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A count card is one link to its queue: the queue's icon (hidden from
// screen readers), the label, the figure with Indian grouping and the
// optional detail line, in that order, so the link's name reads "My drafts
// 9,999 …"; CSS draws the figure on top (UI v2 §5.12 item 2, N13).
func TestStatCard_LinkWithFigureLabelIcon(t *testing.T) {
	doc := uitest.Render(t, components.StatCard(components.StatCardProps{
		Count: 9999, Label: "My drafts", Detail: "9,999 orders · 2 need changes", Icon: "pencil-line", Href: "/modules/purchase/q/drafts",
	}))
	a := doc.Find("a.statcard")
	if a.Length() != 1 {
		t.Fatalf("want one a.statcard, got %d", a.Length())
	}
	if href, _ := a.Attr("href"); href != "/modules/purchase/q/drafts" {
		t.Errorf("href = %q", href)
	}
	if a.HasClass("statcard--hot") {
		t.Error("a card that isn't Hot has statcard--hot")
	}
	if got, _ := a.Find(".statcard-icon").Attr("aria-hidden"); got != "true" || a.Find(".statcard-icon svg").Length() != 1 {
		t.Errorf("icon tile aria-hidden = %q with %d svg, want true and 1", got, a.Find(".statcard-icon svg").Length())
	}
	for sel, want := range map[string]string{
		".statcard-count": "9,999", ".statcard-label": "My drafts", ".statcard-detail": "9,999 orders · 2 need changes",
	} {
		if got := a.Find(sel).Text(); got != want {
			t.Errorf("%s = %q, want %q", sel, got, want)
		}
	}
	if got := texts(a.Children()); got != "|My drafts|9,999|9,999 orders · 2 need changes" {
		t.Errorf("link text = %q, want label, figure, detail", got)
	}
}

// The hot card (To approve above 0) carries statcard--hot; a card with no
// detail has no empty detail line.
func TestStatCard_HotNoDetail(t *testing.T) {
	doc := uitest.Render(t, components.StatCard(components.StatCardProps{Count: 2, Label: "To approve", Icon: "check-check", Href: "/approvals", Hot: true}))
	a := doc.Find("a.statcard")
	if !a.HasClass("statcard--hot") {
		t.Error("a Hot card has no statcard--hot")
	}
	if doc.Find(".statcard-detail").Length() != 0 {
		t.Error("a card without Detail renders a detail line")
	}
	if got := texts(a.Children()); got != "|To approve|2" {
		t.Errorf("link parts = %q, want the icon, To approve, 2", got)
	}
}

// squash collapses runs of white space to one space.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// texts is each node's text, joined with "|" in tree order.
func texts(sel *goquery.Selection) string {
	var out []string
	sel.Each(func(_ int, s *goquery.Selection) { out = append(out, squash(s.Text())) })
	return strings.Join(out, "|")
}
