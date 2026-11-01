package layout_test

import (
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func renderHome(t *testing.T, p layout.HomeProps) *goquery.Document {
	t.Helper()
	return uitest.RenderCtx(t, ui.WithShell(context.Background(), shell()), layout.ModuleHome(p))
}

var homeCols = []components.Column{
	{Key: "title", Label: "Number", Priority: components.PTitle, Code: true},
	{Key: "state", Label: "State", Priority: components.PState},
}

func homeRows(titles ...string) components.DataListProps {
	p := components.DataListProps{Label: "rows", Columns: homeCols}
	for _, s := range titles {
		p.Rows = append(p.Rows, components.Row{Href: "/doc/x/" + s, Values: map[string]string{"title": s}, State: "draft"})
	}
	return p
}

// h2s are the page's section headings in order.
func h2s(d *goquery.Document) []string {
	var out []string
	d.Find("main h2").Each(func(_ int, s *goquery.Selection) { out = append(out, strings.TrimSpace(s.Text())) })
	return out
}

// The module home (UI v2 §5.12): the h1, the count cards in the given order,
// then Waiting for you with its See all link, Recent with its link, Quick
// actions and the side content.
func TestModuleHome_Sections(t *testing.T) {
	d := renderHome(t, layout.HomeProps{
		Title: "Purchase",
		Counts: []components.StatCardProps{
			{Count: 2, Label: "To approve", Icon: "check-check", Href: "/approvals", Hot: true},
			{Count: 3, Label: "My drafts", Icon: "pencil-line", Href: "/q/drafts"},
		},
		Waiting:    homeRows("PO-1"),
		WaitingAll: &components.Link{Label: "See all 4", Href: "/approvals?type=purchase.Order"},
		Recent:     homeRows("PO-2", "PO-3"),
		RecentAll:  &components.Link{Label: "All purchase orders", Href: "/doc/purchase.Order"},
		Quick:      []components.QuickActionProps{{Icon: "plus", Label: "New purchase order", Href: "/doc/purchase.Order/new"}},
		Side:       text("SIDE"),
	})
	if got := strings.TrimSpace(d.Find("main h1").Text()); got != "Purchase" {
		t.Errorf("h1 = %q", got)
	}
	var labels []string
	d.Find("ul.statcards > li > a.statcard .statcard-label").Each(func(_ int, s *goquery.Selection) { labels = append(labels, s.Text()) })
	if strings.Join(labels, "|") != "To approve|My drafts" {
		t.Errorf("count cards = %q, want To approve then My drafts", labels)
	}
	if got := strings.Join(h2s(d), "|"); got != "Waiting for you|Recent|Quick actions" {
		t.Errorf("sections = %q", got)
	}
	for sel, want := range map[string]string{
		`a[href="/approvals?type=purchase.Order"]`:  "See all 4",
		`a[href="/doc/purchase.Order"]`:             "All purchase orders",
		`a.newtile[href="/doc/purchase.Order/new"]`: "New purchase order",
	} {
		if got := strings.TrimSpace(d.Find("main " + sel).Text()); got != want {
			t.Errorf("%s = %q, want %q", sel, got, want)
		}
	}
	if n := d.Find("main section:has(h2:contains('Waiting for you')) .datalist-link").Length(); n != 1 {
		t.Errorf("Waiting rows = %d, want 1", n)
	}
	if n := d.Find("main section:has(h2:contains('Recent')) .datalist-link").Length(); n != 2 {
		t.Errorf("Recent rows = %d, want 2", n)
	}
	if !strings.Contains(d.Find("main").Text(), "SIDE") {
		t.Error("the side content is missing")
	}
	if strings.Contains(d.Text(), layout.HomeCaughtUp) {
		t.Error("a home with work waiting says it is all caught up")
	}
}

// A-24: with no counts and nothing waiting the home shows `You're all caught
// up.` and still the quick actions; Recent and Quick actions headings only
// show with rows.
func TestModuleHome_CaughtUp(t *testing.T) {
	d := renderHome(t, layout.HomeProps{
		Title: "Purchase",
		Quick: []components.QuickActionProps{{Icon: "plus", Label: "New purchase request", Href: "/doc/purchase.Request/new"}},
	})
	if layout.HomeCaughtUp != "You're all caught up." {
		t.Errorf("HomeCaughtUp = %q", layout.HomeCaughtUp)
	}
	if got := strings.TrimSpace(d.Find("main .empty-state").Text()); got != layout.HomeCaughtUp {
		t.Errorf("empty state = %q, want %q", got, layout.HomeCaughtUp)
	}
	if d.Find("main a.newtile").Length() != 1 {
		t.Error("the caught-up home has no quick action")
	}
	if d.Find("ul.statcards").Length() != 0 {
		t.Error("no counts but a count row is drawn")
	}
	if got := strings.Join(h2s(d), "|"); got != "Waiting for you|Quick actions" {
		t.Errorf("sections = %q, want Waiting for you and Quick actions only", got)
	}

	// Recent rows alone don't count as waiting work.
	d = renderHome(t, layout.HomeProps{Title: "Purchase", Recent: homeRows("PO-9")})
	if !strings.Contains(d.Text(), layout.HomeCaughtUp) {
		t.Error("only Recent rows: want the caught-up state")
	}
	if strings.Contains(strings.Join(h2s(d), "|"), "Quick actions") {
		t.Error("no quick actions but the heading shows")
	}
}

// Counts alone are waiting work: the caught-up state hides, and Waiting for
// you hides while it has no rows. Waiting rows alone hide the caught-up
// state too.
func TestModuleHome_CountsOrRowsAreWork(t *testing.T) {
	d := renderHome(t, layout.HomeProps{Title: "Purchase", Counts: []components.StatCardProps{{Count: 1, Label: "To send", Icon: "send", Href: "/q/to-send"}}})
	if strings.Contains(d.Text(), layout.HomeCaughtUp) {
		t.Error("counts but the caught-up state shows")
	}
	if len(h2s(d)) != 0 {
		t.Errorf("sections = %q, want none", h2s(d))
	}
	d = renderHome(t, layout.HomeProps{Title: "Purchase", Waiting: homeRows("PO-1")})
	if strings.Contains(d.Text(), layout.HomeCaughtUp) {
		t.Error("waiting rows but the caught-up state shows")
	}
}
