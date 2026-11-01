package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// ConflictBanner is every 409's banner (CD-362): a warning with DOC's text,
// the keep-your-entries line and a Reload button naming the record, or a
// plain Reload when there is no label.
func TestConflictBanner_Labels(t *testing.T) {
	const msg = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	const href = "/doc/po/42/edit/details"
	for _, tc := range []struct{ label, button string }{
		{"Draft 42", "Reload Draft 42"},
		{"", "Reload"},
	} {
		doc := uitest.Render(t, components.ConflictBanner(msg, tc.label, href))
		b := doc.Find(".banner")
		if b.Length() != 1 || !b.HasClass("banner--warning") || b.AttrOr("role", "") != "alert" {
			t.Fatalf("label %q: %d banners (class %q), want one warning alert", tc.label, b.Length(), b.AttrOr("class", ""))
		}
		if got, want := text(b.Find(".banner-body > p").First()), "Warning: "+msg; got != want {
			t.Errorf("label %q: message = %q, want %q", tc.label, got, want)
		}
		if got, want := text(b.Find(".banner-body > p").Eq(1)), "Your entries stay on this page until you reload, so you can copy them."; got != want {
			t.Errorf("label %q: second line = %q, want %q", tc.label, got, want)
		}
		a := b.Find("a.btn")
		if a.Length() != 1 || a.Find("span").Text() != tc.button || a.AttrOr("href", "") != href || !a.HasClass("btn--secondary") {
			t.Fatalf("label %q: button = %q -> %q (%d), want the secondary %q -> %s", tc.label, text(a), a.AttrOr("href", ""), a.Length(), tc.button, href)
		}
		if a.Find("svg").Length() != 1 {
			t.Fatalf("label %q: button has %d icons, want 1", tc.label, a.Find("svg").Length())
		}
		if got, _ := a.Find("svg").Html(); got != iconHTML(t, "refresh-cw") {
			t.Errorf("label %q: button icon isn't refresh-cw", tc.label)
		}
	}
}
