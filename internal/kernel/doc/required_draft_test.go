package doc

import "testing"

// DOC-043, draft half (CD-178): a draft save drops the blank line, keeps the
// line with only a description, and reports no Required errors.
func TestDoc_Lines_BlankDroppedDraft(t *testing.T) {
	lines := []map[string]string{
		validLine(),
		{"item_id": "", "qty": "", "rate": "", "description": ""},
		{"item_id": "", "qty": "", "rate": "", "description": "loading charges"},
	}
	n, errs, _ := validate(t, testDT(), ActionInput{Values: validHeader(), Lines: lines}, false)
	if len(n.Lines) != 2 {
		t.Fatalf("normalized lines = %d, want 2", len(n.Lines))
	}
	if got := n.Lines[1].Values["description"]; got != "loading charges" {
		t.Errorf("line 2 description = %#v, want %q", got, "loading charges")
	}
	eqErrs(t, errs, nil)
}
