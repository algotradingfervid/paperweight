package layout_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The loading bar is the first element in <body>, hidden from assistive
// tech, and <body> names it as every request's indicator (UI v2 §5.14).
func TestPage_LoadbarIsTheBodyIndicator(t *testing.T) {
	for _, o := range []layout.PageOpts{{Title: "Purchase orders"}, {Title: "Sign in", Bare: true}} {
		doc := render(t, shell(), o)
		if got, _ := doc.Find("body").Attr("hx-indicator"); got != "#loadbar" {
			t.Errorf("bare=%v: body hx-indicator = %q, want #loadbar", o.Bare, got)
		}
		first := doc.Find("body").Children().First()
		if id, _ := first.Attr("id"); id != "loadbar" {
			t.Fatalf("bare=%v: body's first element is #%s, want #loadbar", o.Bare, id)
		}
		if got, _ := first.Attr("aria-hidden"); got != "true" {
			t.Errorf("bare=%v: #loadbar aria-hidden = %q, want true", o.Bare, got)
		}
		if !first.HasClass("loadbar") {
			t.Errorf("bare=%v: #loadbar lacks its loadbar class", o.Bare)
		}
	}
}

// The network banner goes in its own polite live region
// (never over a field), and its two texts come from the page's templates.
func TestPage_NetworkBannerTemplates(t *testing.T) {
	for _, o := range []layout.PageOpts{{Title: "Purchase orders"}, {Title: "Sign in", Bare: true}} {
		doc := render(t, shell(), o)
		region := doc.Find("#network")
		if region.Length() != 1 {
			t.Fatalf("bare=%v: %d #network regions, want 1", o.Bare, region.Length())
		}
		if got, _ := region.Attr("aria-live"); got != "polite" {
			t.Errorf("bare=%v: #network aria-live = %q, want polite", o.Bare, got)
		}
		if _, ok := region.Attr("role"); ok {
			t.Errorf("bare=%v: #network has a role; it is announced politely", o.Bare)
		}
		if region.Closest("#alerts").Length() != 0 {
			t.Errorf("bare=%v: #network sits inside #alerts (role=alert is assertive)", o.Bare)
		}
		wifi, _ := uitest.Render(t, icons.Icon("wifi-off")).Find("svg").Html()
		for _, c := range []struct{ id, want, button, attr string }{
			{"network-offline", "Error: Couldn't reach the server. Check your connection. Nothing you typed is lost.", "Try again", "data-retry"},
			{"network-server", "Error: The server had a problem. Try again in a minute.", "Try again", "data-retry"},
			{"network-unsure", "Error: Your change may have been saved. Reload to check before trying again.", "Reload", "data-reload"},
		} {
			id, want := c.id, c.want
			tpl := doc.Find("template#" + id)
			if tpl.Length() != 1 {
				t.Fatalf("bare=%v: %d template#%s, want 1", o.Bare, tpl.Length(), id)
			}
			if got := strings.Join(strings.Fields(tpl.Find(".banner-body > p").Text()), " "); got != want {
				t.Errorf("bare=%v: %s text = %q, want %q", o.Bare, id, got, want)
			}
			if tpl.Find(".banner.banner--danger").Length() != 1 {
				t.Errorf("bare=%v: %s holds no danger banner", o.Bare, id)
			}
			if tpl.Find("[role]").Length() != 0 {
				t.Errorf("bare=%v: %s has a role; the polite region announces it", o.Bare, id)
			}
			if btn := tpl.Find("button[type=button][" + c.attr + "]"); btn.Length() != 1 || strings.TrimSpace(btn.Text()) != c.button {
				t.Errorf("bare=%v: %s has %d %s buttons, want 1", o.Bare, id, btn.Length(), c.button)
			}
			if h, _ := tpl.Find(".banner > svg").Html(); h != wifi {
				t.Errorf("bare=%v: %s banner icon is not wifi-off", o.Bare, id)
			}
		}
	}
}
