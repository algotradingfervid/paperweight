package components_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/icons"
)

// r12Icon is icon name as drawn.
func r12Icon(t *testing.T, name string) string {
	t.Helper()
	var b bytes.Buffer
	if err := icons.Icon(name).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func r12Doc(t *testing.T, c templ.Component) *goquery.Document {
	t.Helper()
	var b bytes.Buffer
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(&b)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// r12Action is the action named aria inside area: an accessible name, an
// icon and either a visible word or none.
func r12Action(t *testing.T, area *goquery.Selection, aria, icon, word, class string) {
	t.Helper()
	el := area.Find(`[aria-label="` + aria + `"]`)
	if el.Length() != 1 {
		t.Fatalf("%d controls named %q, want 1", el.Length(), aria)
	}
	html, _ := el.Html()
	if !strings.Contains(html, r12Icon(t, icon)) {
		t.Errorf("%q has no %s icon: %s", aria, icon, html)
	}
	if got := strings.TrimSpace(el.Text()); got != word {
		t.Errorf("%q shows %q, want %q", aria, got, word)
	}
	if !el.HasClass(class) {
		t.Errorf("%q class = %q, want %s", aria, el.AttrOr("class", ""), class)
	}
}

// The move group is drawn only around a move button: a single line, with
// neither, has none, and a line with one has it holding just that one.
func TestR12_LineMovesOnlyWithAMove(t *testing.T) {
	for _, tc := range []struct {
		name     string
		up, down string
		want     []string
	}{
		{"neither", "", "", nil},
		{"up only", "/l/2/move?dir=up", "", []string{"Move up line 2"}},
		{"down only", "", "/l/2/move?dir=down", []string{"Move down line 2"}},
		{"both", "/l/2/move?dir=up", "/l/2/move?dir=down", []string{"Move up line 2", "Move down line 2"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			card := components.LineCard{No: 2, Item: "Cement", Amount: "₹1.00", ChangeURL: "/l/2", RemoveURL: "/l/2/remove", MoveUpURL: tc.up, MoveDownURL: tc.down}
			doc := r12Doc(t, components.LineList(components.LineListProps{Cards: []components.LineCard{card}, Editable: true, Total: 1}))
			groups := doc.Find(".lines-actions .lines-moves")
			if want := min(len(tc.want), 1); groups.Length() != want {
				t.Fatalf("%d move groups, want %d", groups.Length(), want)
			}
			var got []string
			groups.Find("[aria-label]").Each(func(_ int, s *goquery.Selection) { got = append(got, s.AttrOr("aria-label", "")) })
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("move group holds %q, want %q", got, tc.want)
			}
		})
	}
}

// §5.11: on a card and on both grids, Change has the pencil, Remove the
// trash in the danger text colour, and Move up and Move down are icon-only,
// each with its name for screen readers.
func TestR12_LineActions(t *testing.T) {
	card := components.LineCard{
		No: 2, Item: "Cement", Amount: "₹1.00",
		ChangeURL: "/l/2", RemoveURL: "/l/2/remove", MoveUpURL: "/l/2/move?dir=up", MoveDownURL: "/l/2/move?dir=down", Version: "3",
	}
	row := components.LineGridRow{
		No: 2, Amount: "₹1.00", Cells: []components.GridCell{{Label: "Item", Input: templ.NopComponent, Text: "Cement"}, {Label: "Qty", Input: templ.NopComponent, Text: "1"}, {Label: "Rate", Input: templ.NopComponent, Text: "1"}},
		PostURL: "/l/2", ChangeURL: "/l/2", RemoveURL: "/l/2/remove", MoveUpURL: "/l/2/move?dir=up", MoveDownURL: "/l/2/move?dir=down", Version: "3",
	}
	for _, tc := range []struct {
		name, area string
		list       components.LineListProps
		change     bool
	}{
		{"card", ".lines-actions", components.LineListProps{Cards: []components.LineCard{card}, Editable: true, Total: 1}, true},
		{"grid", ".line-editor .linegrid-actions", components.LineListProps{Grid: []components.LineGridRow{row}, Editable: true, Total: 1}, false},
		{"display grid", ".line-editor .linegrid-actions", components.LineListProps{Grid: []components.LineGridRow{row}, Display: true, Total: 1}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			area := r12Doc(t, components.LineList(tc.list)).Find(tc.area)
			if area.Length() != 1 {
				t.Fatalf("%d %s, want 1", area.Length(), tc.area)
			}
			if tc.change {
				r12Action(t, area, "Change line 2", "pencil", "Change", "btn--ghost")
			}
			r12Action(t, area, "Remove line 2", "trash-2", "Remove", "btn--danger-quiet")
			r12Action(t, area, "Move up line 2", "arrow-up", "", "btn--ghost")
			r12Action(t, area, "Move down line 2", "arrow-down", "", "btn--ghost")
		})
	}
}
