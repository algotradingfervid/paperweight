package modkitweb_test

import (
	"net/http"
	"strings"
	"testing"
)

// No case ID: a stale save is the conflict banner (CD-365), a warning with
// DOC's text and "Reload Business settings" to the page; a bad date (422)
// stays the danger error summary with no warning.
func TestSettingsPage_ConflictBanner(t *testing.T) {
	s := newScreen(t)
	wantStatus(t, "first tab's save", s.post(adminEmail, form("1", "31-03-2026", terms)), http.StatusSeeOther)
	s.get(adminEmail) // shows the save's flash, so the 409 page has only its own banner
	stale := s.post(adminEmail, form("1", "30-06-2026", terms))
	wantStatus(t, "stale save", stale, http.StatusConflict)
	b := stale.doc.Find("main .banner")
	if b.Length() != 1 || !b.HasClass("banner--warning") {
		t.Fatalf("409 banners = %d (class %q); want one warning", b.Length(), b.AttrOr("class", ""))
	}
	if got := norm(b.Text()); !strings.Contains(got, msgStale) {
		t.Errorf("409 banner %q lacks %q", got, msgStale)
	}
	a := b.Find("a.btn")
	if got := norm(a.Text()); a.Length() != 1 || got != "Reload "+pageTitle || a.AttrOr("href", "") != settingsPath {
		t.Errorf("Reload button = %q -> %q (%d); want %q -> %s", got, a.AttrOr("href", ""), a.Length(), "Reload "+pageTitle, settingsPath)
	}

	bad := s.post(adminEmail, form("2", "31-13-2026", terms))
	wantStatus(t, "bad date", bad, http.StatusUnprocessableEntity)
	if n := bad.doc.Find("main .banner--warning").Length(); n != 0 {
		t.Errorf("422 has %d warning banners; want none", n)
	}
	if n := bad.doc.Find("main .error-summary").Length(); n != 1 {
		t.Errorf("422 has %d error summaries; want 1", n)
	}
}
