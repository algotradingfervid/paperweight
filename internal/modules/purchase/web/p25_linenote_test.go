package purchaseweb

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P25 (CD-330 (2), UX §5.16): on the request page a line's own note
// shows first, then the module's note. PR-A's row 1 (UltraTech or ACC only),
// held by Priya's draft, reads its note, the figures and the holder; row 2
// has no note of its own and reads only the module's note.
func TestPUX022_RequestPageKeepsLineNote(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t)
	draft := w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "25")
	page := w.get(t, w.env.Priya, p15dDoc(purchase.RequestDocType, pr.ID))
	if page.status != 200 {
		t.Fatalf("Priya GET PR-A = %d: %s", page.status, page.text())
	}
	want := []string{
		"UltraTech or ACC only · Requested 40 Bags · Ordered 0 Bags · Pending to order 15 Bags · On " + p15dDisplay(draft) + " (Priya Shah)",
		"Requested 25 Kilograms · Ordered 0 Kilograms · Pending to order 25 Kilograms",
	}
	if got := page.notes(); !slices.Equal(got, want) {
		t.Errorf("line notes = %q, want %q", got, want)
	}
}
