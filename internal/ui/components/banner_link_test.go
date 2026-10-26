package components_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A banner's Link is its own sentence: the page text keeps a space between
// the message and the link text, so it never reads "…again.Reload".
func TestBanner_LinkSpacedFromMessage(t *testing.T) {
	const msg = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	doc := uitest.Render(t, components.Banner(components.BannerProps{
		Kind: components.Danger, Message: msg,
		Link: &components.Link{Label: "Reload", Href: "/doc/po/42/edit/details"},
	}))
	a := doc.Find(`.banner a[href="/doc/po/42/edit/details"]`)
	if a.Length() != 1 || text(a) != "Reload" {
		t.Fatalf("banner has %d Reload links, want 1", a.Length())
	}
	raw := doc.Find(".banner").Text()
	if !strings.Contains(raw, msg) {
		t.Fatalf("banner text %q lacks the message", raw)
	}
	if strings.Contains(raw, msg+"Reload") {
		t.Errorf("banner text runs the message into the link: %q", raw)
	}
	if !strings.Contains(text(doc.Find(".banner")), msg+" Reload") {
		t.Errorf("banner text = %q, want %q", text(doc.Find(".banner")), msg+" Reload")
	}
}
