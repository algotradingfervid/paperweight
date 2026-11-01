package layout_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func renderSolo(t *testing.T, s ui.Shell, o layout.PageOpts) *goquery.Document {
	t.Helper()
	ctx := templ.WithChildren(ui.WithShell(context.Background(), s), text("Hello"))
	return uitest.RenderCtx(t, ctx, layout.Solo(o))
}

// Solo is a page with one centred card: the brand block (logo and app name
// from the brand), the heading, the banner and the children, in that order,
// and nothing of the signed-in shell (UI v2 §5.3, N3).
func TestSolo_Card(t *testing.T) {
	doc := renderSolo(t, shell(), layout.PageOpts{Title: "Sign in", BannerSlot: text("BANNER")})
	if got, want := doc.Find("title").Text(), "Sign in · Internal tools"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	card := doc.Find("main .solo .solo__card")
	if card.Length() != 1 {
		t.Fatalf("%d .solo__card in main, want 1", card.Length())
	}
	img := card.Find(".solo__brand img.brand-logo")
	for attr, want := range map[string]string{
		"src": brand.AppLogoURL, "width": strconv.Itoa(brand.AppLogoWidth),
		"height": strconv.Itoa(brand.AppLogoHeight), "alt": "",
	} {
		if got, ok := img.Attr(attr); !ok || got != want {
			t.Errorf("solo logo %s = %q, want %q", attr, got, want)
		}
	}
	if got := strings.TrimSpace(card.Find(".solo__brand").Text()); got != "Internal tools" {
		t.Errorf("brand block text = %q, want the shell's app name", got)
	}
	h1 := doc.Find("h1")
	if h1.Length() != 1 || h1.Text() != "Sign in" || h1.Closest(".solo__card").Length() != 1 {
		t.Fatalf("want one h1 Sign in inside the card, got %d %q", h1.Length(), h1.Text())
	}
	if id, _ := h1.Attr("id"); id != "page-title" {
		t.Errorf("h1 id = %q, want page-title", id)
	}
	if ti, _ := h1.Attr("tabindex"); ti != "-1" {
		t.Errorf("h1 tabindex = %q, want -1", ti)
	}
	html, _ := card.Html()
	b, h, banner, body := strings.Index(html, "solo__brand"), strings.Index(html, "<h1"), strings.Index(html, "BANNER"), strings.Index(html, "Hello")
	if b < 0 || b >= h || h >= banner || banner >= body {
		t.Errorf("card order brand %d, h1 %d, banner %d, children %d; want that order", b, h, banner, body)
	}
	if got := strings.TrimSpace(doc.Find(".solo__foot").Text()); got != "Purchase orders, goods receipts and expenses in one place." {
		t.Errorf("foot line = %q", got)
	}
	for _, sel := range []string{"nav", ".bottomnav", "dialog", ".menu-button", ".header-search"} {
		if n := doc.Find(sel).Length(); n != 0 {
			t.Errorf("solo page has %d %s, want none (signed-in shell)", n, sel)
		}
	}
}

func TestSolo_ErrorTitle(t *testing.T) {
	doc := renderSolo(t, ui.Shell{AppName: "X"}, layout.PageOpts{Title: "Sign in", Error: true})
	if got := doc.Find("title").Text(); got != "Error: Sign in · X" {
		t.Errorf("title = %q", got)
	}
}
