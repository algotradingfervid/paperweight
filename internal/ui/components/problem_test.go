package components_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const notOwnerText = "You can only change Purchase Orders you created." // an AUTH 403 text

// ProblemPage shows its status's icon and heading, the kernel text exactly,
// the next step and the ways back (UI v2 §5.14, A-30).
func TestProblemPage_404And403(t *testing.T) {
	for _, tc := range []struct {
		status             int
		msg, icon, heading string
	}{
		{http.StatusNotFound, doctype.ErrNotFound.Error(), "file-question", "We can't open this page"},
		{http.StatusForbidden, notOwnerText, "shield-alert", "You don't have access to this"},
	} {
		doc := uitest.Render(t, components.ProblemPage(tc.status, tc.msg))
		p := doc.Find("section.problem")
		if p.Length() != 1 {
			t.Fatalf("%d: %d problem sections, want 1", tc.status, p.Length())
		}
		if got, want := p.Find(".problem-icon svg").Length(), 1; got != want {
			t.Errorf("%d: %d icons, want %d", tc.status, got, want)
		}
		if got, _ := p.Find(".problem-icon svg").Html(); got != iconHTML(t, tc.icon) {
			t.Errorf("%d: icon isn't %s", tc.status, tc.icon)
		}
		if _, hidden := p.Find(".problem-icon").Attr("aria-hidden"); !hidden {
			t.Errorf("%d: the icon circle isn't hidden from screen readers", tc.status)
		}
		h := p.Find("h1#page-title")
		if text(h) != tc.heading || h.AttrOr("tabindex", "") != "-1" {
			t.Errorf("%d: heading = %q (tabindex %q), want %q", tc.status, text(h), h.AttrOr("tabindex", ""), tc.heading)
		}
		if p.AttrOr("aria-labelledby", "") != "page-title" {
			t.Errorf("%d: section labelled by %q", tc.status, p.AttrOr("aria-labelledby", ""))
		}
		if got := p.Find(".problem-message").Text(); got != tc.msg {
			t.Errorf("%d: message = %q, want the kernel text %q", tc.status, got, tc.msg)
		}
		if got, want := text(p.Find(".problem-next")), "If someone sent you this link, ask them to check it, or ask your administrator for access."; got != want {
			t.Errorf("%d: next step = %q", tc.status, got)
		}
		links := p.Find(".problem-actions a")
		if links.Length() != 2 {
			t.Fatalf("%d: %d links, want Go to Home and Search", tc.status, links.Length())
		}
		home, search := links.Eq(0), links.Eq(1)
		if text(home) != "Go to Home" || home.AttrOr("href", "") != "/" || !home.HasClass("btn--primary") {
			t.Errorf("%d: first link = %q -> %q (class %q), want the primary Go to Home -> /", tc.status, text(home), home.AttrOr("href", ""), home.AttrOr("class", ""))
		}
		if text(search) != "Search" || search.AttrOr("href", "") != "/search" || !search.HasClass("btn--secondary") {
			t.Errorf("%d: second link = %q -> %q (class %q), want the secondary Search -> /search", tc.status, text(search), search.AttrOr("href", ""), search.AttrOr("class", ""))
		}
	}
}
