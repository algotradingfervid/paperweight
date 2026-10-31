package purchaseweb

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// audits is the number of the PO's audit entries.
func (w *p18Web) audits(t *testing.T, id int64) int {
	t.Helper()
	var n int
	if err := w.env.DB.Read(t.Context(), func(tx *db.Tx) error {
		es, err := audit.ForDoc(t.Context(), tx, purchase.DocType, id)
		n = len(es)
		return err
	}); err != nil {
		t.Fatalf("audit of PO %d: %v", id, err)
	}
	return n
}

// p18NoteLong is DOC's generic length wording for the note (AD-13 ruling (1)).
const p18NoteLong = "Note can be at most 200 characters."

// PO-080 handlers, AD-13 rulings (1) and (2) on P18: a 201-character note
// redraws through renderExpected with 422 and the length text, nothing
// written; 200 characters redirects to the PO. A clear posting version 0 on a
// PO with no record redirects to the PO, writes no row and no audit entry,
// and never redraws.
func TestPurchaseWeb_ExpectedRulings(t *testing.T) {
	w := newP18Web(t)
	env := w.env
	po := purchasetest.NewOrder(t, env)

	t.Run("clear with no record", func(t *testing.T) {
		audits := w.audits(t, po.ID)
		a := w.post(t, env.Arjun, clearPath(po.ID), url.Values{"version": {"0"}})
		if a.status != http.StatusSeeOther || a.location != poPage(po.ID) {
			t.Errorf("clear with version 0 and no record = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, poPage(po.ID))
		}
		if len(w.renders) != 0 {
			t.Errorf("renderExpected called for a no-op clear: %+v", w.renders)
		}
		if n := w.rows(t); n != 0 {
			t.Errorf("rows after the no-op clear = %d, want 0", n)
		}
		if n := w.audits(t, po.ID); n != audits {
			t.Errorf("PO audit entries after the no-op clear = %d, want %d (no entry)", n, audits)
		}
	})

	t.Run("note over 200", func(t *testing.T) {
		a := w.post(t, env.Arjun, expectedPath(po.ID), setForm("0", "05-11-2026", strings.Repeat("a", 201)))
		if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text, p18NoteLong) {
			t.Errorf("set with a 201-character note = %d %q, want 422 with %q", a.status, a.text, p18NoteLong)
		}
		if len(w.renders) != 1 || w.renders[0].status != http.StatusUnprocessableEntity || !slices.Equal(w.renders[0].msgs, []string{p18NoteLong}) {
			t.Errorf("renderExpected calls = %+v, want one 422 with %q", w.renders, p18NoteLong)
		}
		if n := w.rows(t); n != 0 {
			t.Errorf("rows after the refused note = %d, want 0", n)
		}
	})

	t.Run("note of 200", func(t *testing.T) {
		a := w.post(t, env.Arjun, expectedPath(po.ID), setForm("0", "05-11-2026", strings.Repeat("a", 200)))
		if a.status != http.StatusSeeOther || a.location != poPage(po.ID) {
			t.Errorf("set with a 200-character note = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, poPage(po.ID))
		}
		if _, v, ok := w.row(t, po.ID); !ok || v != 1 {
			t.Errorf("row after the 200-character note = v%d %v, want v1", v, ok)
		}
	})
}
