package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// modkit's routes are mounted behind the signed-in guard (M0-6). They answer
// 501 until M0-7 (settings) and M0-14 (comments) build them; those tasks
// change the expectations here.
func TestApp_ModkitRoutesMounted(t *testing.T) {
	w := newWorld(t)
	w.canonical()

	guest := w.browser()
	if r := guest.get("/settings/business"); r.status != http.StatusSeeOther || !strings.HasPrefix(r.location(), "/signin") {
		t.Errorf("guest GET /settings/business = %d Location %q, want 303 to /signin", r.status, r.location())
	}

	b := w.signedIn(adminEmail)
	if r := b.get("/settings/business"); r.status != http.StatusNotImplemented {
		t.Errorf("GET /settings/business = %d, want 501", r.status)
	}
	for _, path := range []string{"/settings/business", "/modules/comments/purchase.Order/1"} {
		if r := b.post(path, url.Values{}); r.status != http.StatusNotImplemented {
			t.Errorf("POST %s = %d, want 501", path, r.status)
		}
	}
}
