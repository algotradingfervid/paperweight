package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 §5.14: an empty list shows an icon in a circle, one plain line and
// at most one action, the primary next step.
func TestEmptyState_IconLineAndAction(t *testing.T) {
	doc := uitest.Render(t, components.EmptyState("No sample orders yet.", &components.Link{Label: "New sample order", Href: "/doc/so/new"}))
	box := doc.Find(".empty-state")
	if box.Find(".empty-state-icon svg").Length() != 1 {
		t.Error("no icon in .empty-state-icon")
	}
	if got := box.Find(".empty-state-icon").AttrOr("aria-hidden", ""); got != "true" {
		t.Errorf("icon aria-hidden = %q, want true", got)
	}
	if n := box.Find("p").Length(); n != 1 || text(box.Find("p")) != "No sample orders yet." {
		t.Errorf("%d lines, text %q; want one: No sample orders yet.", n, text(box.Find("p")))
	}
	a := box.Find("a")
	if a.Length() != 1 || text(a) != "New sample order" || a.AttrOr("href", "") != "/doc/so/new" || !a.HasClass("btn-primary") {
		t.Errorf("action = %d %q %q %q; want one primary New sample order to /doc/so/new", a.Length(), text(a), a.AttrOr("href", ""), a.AttrOr("class", ""))
	}

	doc = uitest.Render(t, components.EmptyState("No comments yet.", nil))
	if n := doc.Find(".empty-state a").Length(); n != 0 {
		t.Errorf("%d actions with no Link, want none", n)
	}
}

// CD-366: an empty list with no search or filter is the icon, the heading
// and the plain line, with no action: New is in the page head and the
// bottom nav.
func TestEmptyList_IconHeadingLine(t *testing.T) {
	doc := uitest.Render(t, components.EmptyList("No sample orders yet", "Sample orders you add show up here."))
	box := doc.Find(".empty-state")
	if box.Find(".empty-state-icon svg").Length() != 1 || box.Find(".empty-state-icon").AttrOr("aria-hidden", "") != "true" {
		t.Error("no aria-hidden icon in .empty-state-icon")
	}
	if got := text(box.Find("p.empty-state-text")); got != "No sample orders yet" {
		t.Errorf("heading = %q", got)
	}
	if got := text(box.Find("p.empty-state-line")); got != "Sample orders you add show up here." {
		t.Errorf("line = %q", got)
	}
	if n := box.Find("a, button").Length(); n != 0 {
		t.Errorf("%d actions, want none", n)
	}
}

// UI v2 §5.14 filtered-empty: the search icon, the message, an optional
// plain line and Clear filters, which is not a second primary.
func TestNoMatch_MessageLineAndClear(t *testing.T) {
	doc := uitest.Render(t, components.NoMatch(`No sample orders match "shree tmt"`, "Try fewer words, or clear the filters.", "/doc/so?sort=amount.desc"))
	box := doc.Find(".empty-state")
	if box.Find(".empty-state-icon svg circle").Length() != 1 {
		t.Error("no search icon (its circle) in .empty-state-icon")
	}
	if got := text(box.Find("p.empty-state-text")); got != `No sample orders match "shree tmt"` {
		t.Errorf("message = %q", got)
	}
	if got := text(box.Find("p.empty-state-line")); got != "Try fewer words, or clear the filters." {
		t.Errorf("line = %q", got)
	}
	a := box.Find("a")
	if a.Length() != 1 || text(a) != "Clear filters" || a.AttrOr("href", "") != "/doc/so?sort=amount.desc" || a.HasClass("btn-primary") {
		t.Errorf("action = %d %q %q %q; want one non-primary Clear filters", a.Length(), text(a), a.AttrOr("href", ""), a.AttrOr("class", ""))
	}

	doc = uitest.Render(t, components.NoMatch("No pending lines match these filters.", "", "/purchase/pending"))
	if n := doc.Find(".empty-state p").Length(); n != 1 {
		t.Errorf("%d paragraphs with no line, want only the message", n)
	}
}
