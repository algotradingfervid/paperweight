package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UI v2 C4b (§5.6): a card's Action is a ghost button in its head, whatever
// variant it is given, and keeps its label, accessible name and target.
func TestCardWith_GhostAction(t *testing.T) {
	for _, v := range []components.Variant{"", components.Primary} {
		doc := uitest.Render(t, components.CardWith(components.CardProps{
			Title:  "Supplier",
			Action: &components.ButtonProps{Label: "Change", AriaLabel: "Change Supplier", Href: "/x?return=check", Variant: v},
		}))
		a := doc.Find(".card-head > a")
		if a.Length() != 1 {
			t.Fatalf("variant %q: %d head links, want 1", v, a.Length())
		}
		if a.AttrOr("class", "") != "btn tap btn-ghost btn--ghost" {
			t.Errorf("variant %q: class = %q, want the ghost button", v, a.AttrOr("class", ""))
		}
		if a.AttrOr("href", "") != "/x?return=check" || text(a) != "Change" || a.AttrOr("aria-label", "") != "Change Supplier" {
			t.Errorf("variant %q: action = %q href %q aria-label %q", v, text(a), a.AttrOr("href", ""), a.AttrOr("aria-label", ""))
		}
	}
	// A card with only an action still has its head.
	if n := uitest.Render(t, components.CardWith(components.CardProps{Action: &components.ButtonProps{Label: "Open", Href: "/y"}})).Find(".card-head > a").Length(); n != 1 {
		t.Errorf("untitled card: %d head actions, want 1", n)
	}
	// With both set Change wins and the Action isn't drawn; with Change nil
	// the Action is.
	action := &components.ButtonProps{Label: "Open", Href: "/action"}
	for _, c := range []struct {
		name   string
		change *components.Link
		want   string
	}{
		{"both set", &components.Link{Label: "Change", Href: "/change"}, "/change"},
		{"Change nil", nil, "/action"},
	} {
		a := uitest.Render(t, components.CardWith(components.CardProps{Title: "Supplier", Change: c.change, Action: action})).Find(".card-head > a")
		if a.Length() != 1 || a.AttrOr("href", "") != c.want {
			t.Errorf("%s: %d head links, href %q, want only %s", c.name, a.Length(), a.AttrOr("href", ""), c.want)
		}
	}
}
