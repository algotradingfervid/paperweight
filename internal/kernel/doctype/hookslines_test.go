package doctype

import "testing"

// M0-28 (CD-286, supporting, no case ID): a DocType with Hooks but no
// LinesTable isn't a document, so Register refuses it.
func TestRegistry_HooksNeedLinesTable(t *testing.T) {
	dt := testOrder()
	dt.LinesTable = ""
	got := mustPanic(t, "Hooks without LinesTable", func() { newRegistry().Register(dt) })
	if want := `doctype: DocType "t.Order": has Hooks but no LinesTable; a document needs a LinesTable`; got != want {
		t.Errorf("panic = %q, want %q", got, want)
	}

	mustNotPanic(t, "master t.Party without Hooks", func() { newRegistry().Register(testParty()) })
}
