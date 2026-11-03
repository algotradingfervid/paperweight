package components_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// R7b: a card's id names its position (Pos), never the number it shows
// (No); the first card Show all reveals is line-reveal; a card without a
// Pos (the gallery's, a module's own list) has no id, so two lists on one
// page never share one.
func TestLineCardID_Position(t *testing.T) {
	for _, c := range []struct {
		card components.LineCard
		want string
	}{
		{components.LineCard{No: 7, Pos: 3}, "line-3"},
		{components.LineCard{No: 21, Pos: 21, Reveal: true}, "line-reveal"},
		{components.LineCard{No: 4}, ""},
	} {
		if got := components.LineCardID(c.card); got != c.want {
			t.Errorf("LineCardID(%+v) = %q, want %q", c.card, got, c.want)
		}
		li := uitest.Render(t, components.LineList(components.LineListProps{Cards: []components.LineCard{c.card}, Total: 1})).Find("li.lines-card")
		id, hasID := li.Attr("id")
		tab, hasTab := li.Attr("tabindex")
		if c.want == "" && (hasID || hasTab) {
			t.Errorf("%+v: card has id %q tabindex %q, want neither", c.card, id, tab)
		}
		if c.want != "" && (id != c.want || tab != "-1") {
			t.Errorf("%+v: card id %q tabindex %q, want %q and -1", c.card, id, tab, c.want)
		}
	}
}
