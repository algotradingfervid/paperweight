package grnweb_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// MOD-GRN S3d (UX doc §5.7 Focus and keyboard, CD-358): only the htmx 200
// answer to a bill details save draws the card heading with Card's
// FocusHeading marker (tabindex="-1", data-focus-heading), which app.js
// focuses after the swap. A plain GET, ?bill=edit, a 422 and a 409 draw the
// heading without it.
func TestGRNBill_FocusHeadingAfterSave(t *testing.T) {
	w := newS1World(t)
	g := w.s3bGRN(w.Suresh, w.s3bPO(), doctype.StateApproved)

	focus := func(what string, r s1Page, wantStatus int, want bool) {
		t.Helper()
		if r.status != wantStatus {
			t.Fatalf("%s = %d %q, want %d", what, r.status, r.text(), wantStatus)
		}
		h := r.s3bCard().Find("h2.card-title")
		if h.Length() != 1 || s2Norm(h.Text()) != "Bill details" {
			t.Fatalf("%s: %d card headings %q, want one Bill details", what, h.Length(), s2Norm(h.Text()))
		}
		_, marker := h.Attr("data-focus-heading")
		tab, hasTab := h.Attr("tabindex")
		if marker != want || hasTab != want || (want && tab != "-1") {
			t.Errorf("%s: heading data-focus-heading %v, tabindex %q (present %v); want marker and tabindex=-1 %v", what, marker, tab, hasTab, want)
		}
	}

	focus("GET, no bill", w.get(w.Suresh, s3Doc(g.ID)), http.StatusOK, false)
	focus("htmx 422", w.s3bSave(w.Suresh, g.ID, s3bForm(0, "", "23-10-2026", ""), true), http.StatusUnprocessableEntity, false)
	focus("htmx save", w.s3bSave(w.Suresh, g.ID, s3bForm(0, s3bInvoice, "23-10-2026", ""), true), http.StatusOK, true)
	focus("GET, saved", w.get(w.Suresh, s3Doc(g.ID)), http.StatusOK, false)
	focus("GET, ?bill=edit", w.get(w.Suresh, s3Doc(g.ID)+"?bill=edit"), http.StatusOK, false)
	focus("htmx 409", w.s3bSave(w.Suresh, g.ID, s3bForm(0, "INV-NEW", "23-10-2026", ""), true), http.StatusConflict, false)
}
