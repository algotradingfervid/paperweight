package purchaseweb

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15d fix round 2 (CD-258 Q8 (a), CD-327 (1); UX §5.16): Request date is
// hidden only as an input, on a new request or a draft; a submitted
// request's Details card shows it.

// PUX-022's Details card shows a submitted request's Request date in
// dd-mm-yyyy (UX §5.16).
func TestPUX022_RequestDateShownSubmitted(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t, purchasetest.RequestHeader(map[string]string{"request_date": "20-10-2026"}))
	page := w.get(t, w.env.Priya, p15dDoc(purchase.RequestDocType, pr.ID))
	if page.status != 200 {
		t.Fatalf("Priya GET PR-A = %d: %s", page.status, page.text())
	}
	if card := p15dCard(page, "Details"); !strings.Contains(card, "Request date20-10-2026") {
		t.Errorf("Details card %q lacks Request date 20-10-2026", card)
	}
}

// PUX-021's draft Details step still has no Request date input (CD-258 Q8 (a)).
func TestPUX021_RequestDateHiddenDraftStep(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t, purchasetest.StopAtDraft())
	page := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/details")
	if page.status != 200 {
		t.Fatalf("Suresh GET details = %d: %s", page.status, page.text())
	}
	if page.doc.Find(`[name="request_date"]`).Length() != 0 || strings.Contains(page.text(), "Request date") {
		t.Errorf("draft Details step shows Request date: %s", page.text())
	}
}

// requestHide with a nil View (a new record, CD-327 (1)) hides Request date
// without dereferencing the View, and hides nothing else.
func TestPUX021_RequestHideNilView(t *testing.T) {
	var m module
	if !m.requestHide(docscreen.Ctx{}, doctype.Field{Name: "request_date"}) {
		t.Error("requestHide(nil View, request_date) = false, want hidden on a new record")
	}
	if m.requestHide(docscreen.Ctx{}, doctype.Field{Name: "purpose"}) {
		t.Error("requestHide(nil View, purpose) = true, want shown")
	}
}
