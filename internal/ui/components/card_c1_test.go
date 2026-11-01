package components_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// Card v2 (UI v2 §5.5 item 1): an icon tile beside the heading, hidden from
// screen readers, and the one-line description under it.
func TestCardWith_IconAndDescription(t *testing.T) {
	doc := uitest.Render(t, components.CardWith(components.CardProps{
		Title: "Vendor and delivery", Icon: "truck", Description: "The vendor gets the order; goods come to the site.",
	}))
	tile := doc.Find(".card-head .card-icon")
	if tile.Length() != 1 || tile.AttrOr("aria-hidden", "") != "true" || tile.Find("svg.icon").Length() != 1 {
		t.Errorf("icon tile = %d (aria-hidden %q, svg %d), want one hidden tile with the icon",
			tile.Length(), tile.AttrOr("aria-hidden", ""), tile.Find("svg.icon").Length())
	}
	if got := text(doc.Find(".card-head h2.card-title")); got != "Vendor and delivery" {
		t.Errorf("heading = %q", got)
	}
	if got := text(doc.Find(".card-head .card-desc")); got != "The vendor gets the order; goods come to the site." {
		t.Errorf("description = %q", got)
	}
	if _, ok := doc.Find("h2").Attr("tabindex"); ok {
		t.Error("heading has tabindex without FocusHeading")
	}
	if doc.Find("[data-focus-heading]").Length() != 0 {
		t.Error("heading marked for focus without FocusHeading")
	}
}

// Without an icon or a description a card renders as before, so every
// existing caller of Card is unchanged.
func TestCardWith_PlainMatchesCard(t *testing.T) {
	change := &components.Link{Label: "Change", Href: "/x"}
	got := c1HTML(t, components.CardWith(components.CardProps{Title: "Supplier", Change: change}))
	const want = `<section class="card card-border"><div class="card-body"><div class="card-head"><h2 class="card-title">Supplier</h2><a href="/x">Change <span class="visually-hidden">Supplier</span></a></div></div></section>`
	for name, c := range map[string]string{"CardWith": got, "Card": c1HTML(t, components.Card("Supplier", change))} {
		if c != want {
			t.Errorf("%s = %s\nwant (base Card) %s", name, c, want)
		}
	}
	if n := uitest.Render(t, components.Card("Supplier", change)).Find(".card-icon, .card-desc").Length(); n != 0 {
		t.Errorf("plain card has %d icon or description elements", n)
	}
}

// CD-358: FocusHeading gives the heading tabindex="-1" and the marker the
// shared script focuses after the swap.
func TestCardWith_FocusHeading(t *testing.T) {
	for _, p := range []components.CardProps{
		{Title: "Bill details", FocusHeading: true},
		{Title: "Bill details", Icon: "receipt-indian-rupee", Description: "As printed on the bill.", FocusHeading: true},
	} {
		h := uitest.Render(t, components.CardWith(p)).Find("h2.card-title")
		if h.AttrOr("tabindex", "") != "-1" {
			t.Errorf("%+v: tabindex = %q, want -1", p, h.AttrOr("tabindex", ""))
		}
		if _, ok := h.Attr("data-focus-heading"); !ok {
			t.Errorf("%+v: heading has no data-focus-heading", p)
		}
		if _, ok := h.Attr("autofocus"); ok {
			t.Errorf("%+v: heading has autofocus; the script focuses it after a swap only", p)
		}
	}
}

// c1HTML is c rendered as raw HTML.
func c1HTML(t *testing.T, c templ.Component) string {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}
