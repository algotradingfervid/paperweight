package authweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// csrfOf reads c's session token from the harness's GET /_test/csrf (Self).
func csrfOf(t *testing.T, c *webtest.Client) string {
	t.Helper()
	r := c.Get("/_test/csrf")
	wantStatus(t, "GET /_test/csrf", r, http.StatusOK)
	tok := strings.TrimSpace(r.Body)
	if tok == "" {
		t.Fatal("GET /_test/csrf returned an empty token")
	}
	return tok
}

// AUTH-046
func TestCSRF_RequiredOnUnsafeMethods(t *testing.T) {
	w := authtest.NewWorld(t)
	w.Grant(t, "PO Writer", authtest.DTOrder, auth.RightWrite, auth.ScopeAll)
	w.AddRole(t, t10Ravi, "PO Writer")
	h, st := t10Harness(t, w)

	c := signedIn(t, h, t10Ravi)
	other := h.Client()
	other.Login(t10Ravi, t10Password) // a second session of the same user
	own, theirs := csrfOf(t, c), csrfOf(t, other)
	if own == theirs {
		t.Fatalf("two sessions share CSRF token %q", own)
	}

	form := url.Values{"name": {"x"}}

	// (a) no token
	r := c.HTMX().NoCSRF().Post("/po/1/save", form)
	wantStatus(t, "(a) no X-CSRF-Token", r, http.StatusForbidden)
	if !strings.Contains(messageOf(r), "Your form has expired. Reload the page and try again.") {
		t.Fatalf("(a) body %q, HX-Trigger %q, want the CSRF message", r.Body, r.Header.Get("HX-Trigger"))
	}
	if st.Saves() != 0 {
		t.Fatalf("(a) reached the handler %d times", st.Saves())
	}

	// (b) another session's token
	r = c.HTMX().NoCSRF().WithHeader("X-CSRF-Token", theirs).Post("/po/1/save", form)
	wantStatus(t, "(b) other session's token", r, http.StatusForbidden)
	if !strings.Contains(messageOf(r), "Your form has expired. Reload the page and try again.") {
		t.Fatalf("(b) body %q, HX-Trigger %q, want the CSRF message", r.Body, r.Header.Get("HX-Trigger"))
	}
	if st.Saves() != 0 {
		t.Fatalf("(b) reached the handler %d times", st.Saves())
	}

	// (c) the session's own token
	r = c.HTMX().NoCSRF().WithHeader("X-CSRF-Token", own).Post("/po/1/save", form)
	wantStatus(t, "(c) own token", r, http.StatusOK)
	if !strings.Contains(r.Body, "SAVED") {
		t.Fatalf("(c) body %q, want SAVED", r.Body)
	}
	if st.Saves() != 1 {
		t.Fatalf("handler calls = %d, want 1", st.Saves())
	}
}
