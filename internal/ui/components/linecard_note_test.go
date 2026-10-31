package components_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// m04Note is a line note with markup characters, which must show as text.
const m04Note = "Pending 3 of 5 <kg> & more"

// m04SetNote sets the additive Note string field of *v (hooks spec §3:
// components.LineCard and LineGridRow gain Note). It goes through
// reflection so this file compiles before the field exists; a missing
// field fails the test instead.
func m04SetNote(t *testing.T, v any, note string) {
	t.Helper()
	f := reflect.ValueOf(v).Elem().FieldByName("Note")
	if !f.IsValid() || f.Kind() != reflect.String {
		t.Fatalf("%T has no Note string field (hooks spec §3)", reflect.ValueOf(v).Elem().Interface())
	}
	f.SetString(note)
}

// m04Shows is the innermost elements of sel whose text contains text: the
// element showing it, whatever words a component puts beside it.
func m04Shows(sel *goquery.Selection, text string) *goquery.Selection {
	return sel.Find("*").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.Contains(m04Norm(s.Text()), text) && s.Children().FilterFunction(func(_ int, c *goquery.Selection) bool {
			return strings.Contains(m04Norm(c.Text()), text)
		}).Length() == 0
	})
}

func m04Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// M0-4: a LineCard's Note shows on that card, as text, and only there; a
// card without a Note renders as before.
func TestLineCard_Note(t *testing.T) {
	cards := []components.LineCard{
		{No: 1, Item: "Cement OPC 53", Code: "CEMENT-OPC", QtyRate: "10 × ₹350.00", Amount: "₹3,500.00"},
		{No: 2, Item: "Wire 18G", Code: "WIRE-18G", QtyRate: "5 × ₹100.00", Amount: "₹500.00"},
	}
	plain := uitest.Render(t, components.LineList(components.LineListProps{Cards: cards, Total: 2}))
	m04SetNote(t, &cards[1], m04Note)
	doc := uitest.Render(t, components.LineList(components.LineListProps{Cards: cards, Total: 2}))

	li := doc.Find("li.lines-card")
	if li.Length() != 2 {
		t.Fatalf("%d cards, want 2", li.Length())
	}
	if m04Shows(li.Eq(1), m04Note).Length() != 1 {
		t.Errorf("card 2 = %q, want an element reading the note %q", m04Norm(li.Eq(1).Text()), m04Note)
	}
	if strings.Contains(li.Eq(0).Text(), "Pending") {
		t.Errorf("card 1 shows card 2's note")
	}
	if doc.Find("kg").Length() != 0 {
		t.Error("the note's markup characters were rendered as markup")
	}
	before, _ := plain.Find("li.lines-card").Eq(0).Html()
	after, _ := li.Eq(0).Html()
	if before != after {
		t.Errorf("a card without a Note changed:\nbefore %s\nafter  %s", before, after)
	}
}

// M0-4: a LineGridRow's Note shows in that row's part of the desktop grid,
// as text, and not in another row's.
func TestLineGridRow_Note(t *testing.T) {
	rows := gridRows(3)
	m04SetNote(t, &rows[1], m04Note)
	doc := uitest.Render(t, components.LineGrid(rows, "₹2,25,000.00"))

	note := m04Shows(doc.Selection, m04Note)
	if note.Length() != 1 {
		t.Fatalf("the note shows %d times, want once", note.Length())
	}
	if !inRow(doc, note, 2) {
		t.Error("the note is not in row 2's part of the grid")
	}
	if inRow(doc, note, 1) || inRow(doc, note, 3) {
		t.Error("the note is in another row's part of the grid")
	}
	if doc.Find("kg").Length() != 0 {
		t.Error("the note's markup characters were rendered as markup")
	}
}
