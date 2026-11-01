package components_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A quick action is one link drawn as the New sheet's tile: the icon in its
// tile (hidden from screen readers) and the label, which is the link's whole
// name (UI v2 §5.12 item 5, N13).
func TestQuickAction_TileLink(t *testing.T) {
	doc := uitest.Render(t, components.QuickAction(components.QuickActionProps{Icon: "shopping-cart", Label: "New purchase order", Href: "/doc/purchase.Order/new"}))
	a := doc.Find("a.newtile")
	if a.Length() != 1 {
		t.Fatalf("want one a.newtile, got %d", a.Length())
	}
	if href, _ := a.Attr("href"); href != "/doc/purchase.Order/new" {
		t.Errorf("href = %q", href)
	}
	if got, _ := a.Find(".newtile-icon").Attr("aria-hidden"); got != "true" || a.Find(".newtile-icon svg").Length() != 1 {
		t.Errorf("icon tile aria-hidden = %q, want true with one svg", got)
	}
	if got := a.Find(".newtile-label").Text(); got != "New purchase order" {
		t.Errorf("label = %q", got)
	}
	if got := squash(a.Text()); got != "New purchase order" {
		t.Errorf("link text = %q, want the label alone", got)
	}
}

// Autofocus is off by default and on when asked (the New sheet's first
// tile, CD-368 (4)).
func TestQuickAction_Autofocus(t *testing.T) {
	plain := uitest.Render(t, components.QuickAction(components.QuickActionProps{Icon: "plus", Label: "New", Href: "/n"}))
	if _, ok := plain.Find("a.newtile").Attr("autofocus"); ok {
		t.Error("a plain quick action has autofocus")
	}
	focused := uitest.Render(t, components.QuickAction(components.QuickActionProps{Icon: "plus", Label: "New", Href: "/n", Autofocus: true}))
	if _, ok := focused.Find("a.newtile").Attr("autofocus"); !ok {
		t.Error("Autofocus: no autofocus attribute")
	}
}

// The New sheet's tiles are QuickActions (CD-368 (4)): one tile per item,
// each the quick action's markup, autofocus on the first only; NewTiles
// without focusFirst puts it on none.
func TestNewTiles_ComposeQuickAction(t *testing.T) {
	items := []ui.NavItem{{Label: "Purchase order", Href: "/doc/a/new", Icon: "shopping-cart"}, {Label: "Expense claim", Href: "/doc/b/new", Icon: "receipt-indian-rupee"}}
	doc := uitest.Render(t, components.NewTiles(items, true))
	tiles := doc.Find(".newtiles > a.newtile")
	if tiles.Length() != 2 {
		t.Fatalf("%d tiles, want 2", tiles.Length())
	}
	for i, it := range items {
		want := uitest.Render(t, components.QuickAction(components.QuickActionProps{Icon: it.Icon, Label: it.Label, Href: it.Href, Autofocus: i == 0}))
		gotHTML, _ := goquery.OuterHtml(tiles.Eq(i))
		wantHTML, _ := goquery.OuterHtml(want.Find("a.newtile"))
		if gotHTML != wantHTML {
			t.Errorf("tile %d = %s, want the QuickAction %s", i, gotHTML, wantHTML)
		}
	}
	if n := uitest.Render(t, components.NewTiles(items, false)).Find("[autofocus]").Length(); n != 0 {
		t.Errorf("focusFirst false: %d tiles with autofocus, want 0", n)
	}
}
