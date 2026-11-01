package authweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// No case ID: a stale rights save is the conflict banner (CD-365), a warning
// with DOC's text and "Reload Expense Claim rights" to the page; a refused
// save of another kind keeps its error page, with no warning.
func TestRightsSave_ConflictBanner(t *testing.T) {
	w, _, admin := p6World(t)
	opened := t17RoleVersion(t, w, p6Stores)
	item := t17RightsPath(t, w, p6Stores, authtest.DTItem)
	wantRedirect(t, "another save", admin.Post(item, t13With(url.Values{"read": {"all"}}, opened)), http.StatusSeeOther, item)
	admin.Get(item) // shows the save's flash

	path := t17RightsPath(t, w, p6Stores, authtest.DTClaim)
	r := admin.Post(path, t13With(url.Values{"read": {"all"}}, opened))
	wantStatus(t, "stale save", r, http.StatusConflict)
	doc := p1Doc(t, "stale save", r)
	// The page's first banner is the slot's; the reset disclosure holds its own warning.
	b := doc.Find("main .banner").First()
	if !b.HasClass("banner--warning") || doc.Find("main .banner--danger").Length() != 0 {
		t.Fatalf("409 first banner class %q, %d danger banners; want a warning and no danger: %s", b.AttrOr("class", ""), doc.Find("main .banner--danger").Length(), p1Text(b))
	}
	if got := p1Text(b); !strings.Contains(got, t17xStoresChanged) {
		t.Errorf("409 banner %q lacks %q", got, t17xStoresChanged)
	}
	a := b.Find("a.btn")
	if got := p1Text(a); a.Length() != 1 || got != "Reload Expense Claim rights" || a.AttrOr("href", "") != path {
		t.Errorf("Reload button = %q -> %q (%d); want \"Reload Expense Claim rights\" -> %s", got, a.AttrOr("href", ""), a.Length(), path)
	}

	current := t17RoleVersion(t, w, p6Stores)
	bad := admin.Post(path, t13With(url.Values{"bogus": {"all"}}, current))
	wantStatus(t, "unknown right", bad, http.StatusBadRequest)
	doc = p1Doc(t, "unknown right", bad)
	if got := p1Text(doc.Find(`[role="alert"]`)); got != p1BadForm {
		t.Errorf("unknown right: alert %q, want %q", got, p1BadForm)
	}
	if n := doc.Find(".banner--warning").Length(); n != 0 {
		t.Errorf("non-409 refusal has %d warning banners; want none", n)
	}
}
