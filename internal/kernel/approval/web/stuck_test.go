package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func TestWeb_StuckListedForAdmin(t *testing.T) {
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))
	// Ravi and Meena can still approve it, so the list is empty.
	if txt := page(t, e, e.Anil, "/admin/approvals/stuck").Text(); !strings.Contains(txt, "No approvals are stuck.") {
		t.Errorf("stuck page before anyone is disabled does not show the empty state; text %q", txt)
	}
	e.Disable(t, e.Ravi.ID)
	e.Disable(t, e.Meena.ID)

	doc := page(t, e, e.Anil, "/admin/approvals/stuck")
	if got := cardTitles(doc); len(got) != 1 || got[0] != "Draft 42" {
		t.Fatalf("stuck card titles %q, want [Draft 42]", got)
	}
	txt := doc.Find("section.card").Text()
	for _, s := range []string{"No active approver: needs Purchase Manager or Director.", "Purchase Order", "₹60,000.00", "Priya Shah"} {
		if !strings.Contains(txt, s) {
			t.Errorf("stuck card does not show %q; card text %q", s, txt)
		}
	}
	if n := doc.Find(`section.card a[href="/doc/fixture.Order/42"]`).Length(); n != 1 {
		t.Errorf("stuck card has %d links to /doc/fixture.Order/42, want 1", n)
	}
}

func TestWeb_StuckRefusesNonAdmin(t *testing.T) {
	e := at.New(t)
	const msg = "You don't have permission to see stuck approvals. Ask an Administrator."
	rec := uitest.Do(t, handler(e, e.Ravi), http.MethodGet, "/admin/approvals/stuck", nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status %d, want 403; body %q", rec.Code, rec.Body.String())
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("parse refusal page: %v", err)
	}
	if got := doc.Find(`[role="alert"]`).Text(); !strings.Contains(got, msg) {
		t.Errorf("alert text %q does not contain the ErrNotAdmin message %q", got, msg)
	}
}
