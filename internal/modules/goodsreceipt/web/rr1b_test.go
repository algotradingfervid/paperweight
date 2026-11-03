package grnweb_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// RR1b fix 2: once the bill form is redrawn over a refused post (the user
// is at the form, typing), the page no longer offers Add bill details,
// whichever way the redraw comes. With htmx, the 422 card removes the
// page's bar out of band, as a first save does; without JavaScript, the page
// drawn from the kept post has no Add bill details primary, and the next
// load (the kept post shown once) has it again. A refused change of stored
// details has no Add bill details to remove.
func TestGRNWeb_BillRedrawNoAddPrimary(t *testing.T) {
	w := newS1World(t)
	g := w.s3bGRN(w.Suresh, w.s3bPO(), doctype.StateApproved)
	oob := `[hx-swap-oob^="delete:"]`

	r := w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", "6,250.00"), true)
	if r.status != http.StatusUnprocessableEntity || r.doc.Find(oob).Length() != 1 {
		t.Errorf("htmx refused first save = %d with %d bar deletes, want 422 and one", r.status, r.doc.Find(oob).Length())
	}

	w.keep = true
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", "6,250.00"), false)
	if r.status != http.StatusSeeOther {
		t.Fatalf("no-JS refused first save = %d, want 303", r.status)
	}
	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "the page over the kept post")
	if r.s3bCard().Find(".error-summary").Length() != 1 {
		t.Fatalf("the page over the kept post has no error summary: %q", r.s3bText())
	}
	if label, _, n := r.r11Bar(); n != 0 {
		t.Errorf("the page over the kept post: bar primary %q, want none", label)
	}
	r = s3OK(t, w.get(w.Suresh, s3Doc(g.ID)), "the next load")
	if label, _, n := r.r11Bar(); n != 1 || label != "Add bill details" {
		t.Errorf("the next load: bar primary %d %q, want Add bill details", n, label)
	}
	w.keep = false

	w.s3bMustSave(w.Suresh, g.ID, 0, s3bInvoice, "6,250.00")
	r = w.s3bSave(w.Suresh, g.ID, s3bForm(1, "", "23-10-2026", "7,000.00"), true)
	if r.status != http.StatusUnprocessableEntity || r.doc.Find(oob).Length() != 0 {
		t.Errorf("htmx refused change = %d with %d bar deletes, want 422 and none", r.status, r.doc.Find(oob).Length())
	}
}
