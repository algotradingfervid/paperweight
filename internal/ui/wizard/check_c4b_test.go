package wizard_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// UI v2 C4b (§5.6): a check card has the icon tile it is given beside its
// heading and a "Change" ghost button to the step, named "Change <title>".
func TestCheckSection_IconTileAndGhostChange(t *testing.T) {
	for _, icon := range []string{"handshake", "list-checks"} {
		d := uitest.Render(t, templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
			return wizard.CheckSection("Party", icon, wizard.Complete, "/doc/po/42/edit/party?return=check").Render(templ.WithChildren(ctx, templ.Raw(`<p class="summary-x">Acme</p>`)), w)
		}))
		head := d.Find("section.card .card-head")
		tile := head.Find(".card-lead .card-icon svg")
		got, _ := tile.Html()
		want, _ := uitest.Render(t, icons.Icon(icon)).Find("svg").Html()
		if tile.Length() != 1 || got != want {
			t.Errorf("%s: tile = %d svg, inner %q, want the %s icon", icon, tile.Length(), got, icon)
		}
		if h := strings.TrimSpace(head.Find(".card-lead h2.card-title").Text()); h != "Party" {
			t.Errorf("%s: heading beside the tile = %q", icon, h)
		}
		a := head.Children().Filter("a")
		if a.Length() != 1 {
			t.Fatalf("%s: %d header links, want the one Change button", icon, a.Length())
		}
		if !a.HasClass("btn") || !a.HasClass("btn-ghost") {
			t.Errorf("%s: Change classes = %q, want a ghost button", icon, a.AttrOr("class", ""))
		}
		if a.AttrOr("href", "") != "/doc/po/42/edit/party?return=check" || strings.TrimSpace(a.Text()) != "Change" || a.AttrOr("aria-label", "") != "Change Party" {
			t.Errorf("%s: Change = %q href %q aria-label %q", icon, a.Text(), a.AttrOr("href", ""), a.AttrOr("aria-label", ""))
		}
	}
}

// An empty icon panics: every check card has its tile.
func TestCheckSection_EmptyIconPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(r.(string), "Party") {
			t.Errorf("recover = %v, want a panic naming the section", r)
		}
	}()
	wizard.CheckSection("Party", "", wizard.Complete, "/x")
}
