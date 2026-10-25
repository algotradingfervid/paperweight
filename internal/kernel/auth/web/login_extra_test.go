package authweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// A guest's GET /signin token is accepted by POST /signin; a wrong or missing
// token is refused with ErrCSRF and signs nobody in.
func TestLogin_GuestTokenAcceptedOthersRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)

	for _, tok := range []string{"wrong-token", ""} {
		c := h.Client()
		page := c.Get("/signin")
		wantStatus(t, "GET /signin", page, http.StatusOK)
		r := c.NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}, "csrf_token": {tok}})
		wantStatus(t, "POST /signin with token "+tok, r, http.StatusForbidden)
		if n := t11SignedInSessions(t, w); n != 0 {
			t.Fatalf("token %q: sessions rows with a user = %d, want 0", tok, n)
		}
		if body := r.Body; !strings.Contains(body, auth.ErrCSRF.Msg) {
			t.Errorf("token %q: body %q, want %q", tok, body, auth.ErrCSRF.Msg)
		}
	}

	c := h.Client()
	page := c.Get("/signin")
	wantStatus(t, "GET /signin", page, http.StatusOK)
	r := c.NoCSRF().Post("/signin", url.Values{
		"email": {t10Priya}, "password": {t10Password}, "csrf_token": {t11FormCSRF(t, "GET /signin", page)},
	})
	wantRedirect(t, "POST /signin with the page's token", r, http.StatusSeeOther, "/")
	if n := t11SignedInSessions(t, w); n != 1 {
		t.Errorf("sessions rows with a user = %d, want 1", n)
	}
}

// Sign-in rotates the CSRF token: the guest page's token is refused on a
// signed-in POST, and the session stays signed in.
func TestLogin_GuestTokenRefusedAfterSignIn(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	w.SetPassword(t, t10Priya, t10Password)

	c := h.Client()
	page := c.Get("/signin")
	wantStatus(t, "GET /signin", page, http.StatusOK)
	guest := t11FormCSRF(t, "GET /signin", page)
	r := c.NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}, "csrf_token": {guest}})
	wantRedirect(t, "POST /signin", r, http.StatusSeeOther, "/")

	out := c.NoCSRF().Post("/signout", url.Values{"csrf_token": {guest}})
	wantStatus(t, "POST /signout with the guest token", out, http.StatusForbidden)
	if !strings.Contains(out.Body, auth.ErrCSRF.Msg) {
		t.Errorf("body %q, want %q", out.Body, auth.ErrCSRF.Msg)
	}
	if n := t11SignedInSessions(t, w); n != 1 {
		t.Errorf("sessions rows with a user = %d, want 1", n)
	}
}
