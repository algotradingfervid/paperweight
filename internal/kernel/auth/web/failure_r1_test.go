package authweb_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// Shell review H2, M7 (plan 2026-11-03 R1): a plain POST hitting expiry or a
// stale token reaches the sign-in page, never a bare page, and sign-in says why.

const notSaved = "What you entered on that page wasn't saved."

// nextOf is next on the way to sign in shown in body: the form's hidden input,
// or the link's query when there was no session to hold a form's token. It fails
// when body offers neither.
func nextOf(t *testing.T, body string) string {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	form := doc.Find(`form[action="/signin"]`)
	if link := doc.Find(`main a[href^="/signin"]`); link.Length() == 1 {
		u, err := url.Parse(link.AttrOr("href", ""))
		if err != nil {
			t.Fatal(err)
		}
		return u.Query().Get("next")
	}
	if form.Find("input[name=email]").Length() != 1 || form.Find("input[name=password]").Length() != 1 {
		t.Fatalf("not the sign-in page: %q", body)
	}
	if v := form.Find("input[name=csrf_token]").AttrOr("value", ""); v == "" {
		t.Fatalf("the sign-in form has no token: %q", body)
	}
	return form.Find("input[name=next]").AttrOr("value", "")
}

func wantReason(t *testing.T, what, body, reason string) {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Find("main").Text(); !strings.Contains(got, reason) {
		t.Errorf("%s: page %q lacks %q", what, got, reason)
	}
}

// Every Referer is either the local page the POST came from or refused.
func TestGuest_PlainPostReachesSignIn(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	host := h.HTTP.URL
	for _, tc := range []struct {
		name, referer, next string
	}{
		{"its page", host + "/po/1/edit?tab=lines", "/po/1/edit?tab=lines"},
		{"no referer", "", ""},
		{"another host", "https://evil.example/po/1", ""},
		{"same path on another host", "http://evil.example" + strings.TrimPrefix(host, "http://127.0.0.1") + "/po/1", ""},
		{"protocol-relative path", host + "//evil.example/po/1", ""},
		{"backslash path stays a local path", host + `/\evil.example/po/1`, "/%5Cevil.example/po/1"},
		{"a scheme with no host", "javascript:alert(1)", ""},
		{"not a URL", "http://[bad", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := signedIn(t, h, t10Priya)
			w.Clock.Advance(12*time.Hour + time.Second)
			r := c.WithHeader("Referer", tc.referer).Post("/po/1/save", url.Values{"name": {"x"}})
			wantStatus(t, "POST after expiry", r, http.StatusUnauthorized)
			if got := nextOf(t, r.Body); got != tc.next {
				t.Errorf("next = %q, want %q", got, tc.next)
			}
			wantReason(t, "expired POST", r.Body, auth.ErrSessionExpired.Msg)
			wantReason(t, "expired POST", r.Body, notSaved)
			if st.Saves() != 0 {
				t.Fatalf("reached the handler %d times", st.Saves())
			}
		})
	}
}

// A guest who was never signed in is told to sign in, not that a session ended.
func TestGuest_PlainPostNeverSignedIn(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	r := h.Client().NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "guest POST", r, http.StatusUnauthorized)
	nextOf(t, r.Body)
	wantReason(t, "guest POST", r.Body, auth.ErrSignInRequired.Msg)
	if strings.Contains(r.Body, auth.ErrSessionExpired.Msg) {
		t.Errorf("a guest is told their session expired: %q", r.Body)
	}
}

// A signed-out htmx POST keeps CD-382's answer: 401 and HX-Redirect, no page.
func TestGuest_HTMXPostStaysARedirect(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	w.Clock.Advance(12*time.Hour + time.Second)
	r := c.HTMX().WithHeader("HX-Current-URL", h.HTTP.URL+"/po/list").Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "htmx POST after expiry", r, http.StatusUnauthorized)
	if got := r.Header.Get("HX-Redirect"); got != "/signin?next=%2Fpo%2Flist" {
		t.Errorf("HX-Redirect = %q", got)
	}
	if r.Body != "" {
		t.Errorf("htmx body = %q, want none", r.Body)
	}
}

func TestCSRF_PlainPostReachesSignIn(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	for _, tc := range []struct {
		name, referer, next string
	}{
		{"its page", h.HTTP.URL + "/po/1/edit", "/po/1/edit"},
		{"another host", "https://evil.example/po/1/edit", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := c.NoCSRF().WithHeader("Referer", tc.referer).Post("/po/1/save", url.Values{"name": {"x"}})
			wantStatus(t, "plain POST with no token", r, http.StatusForbidden)
			if got := nextOf(t, r.Body); got != tc.next {
				t.Errorf("next = %q, want %q", got, tc.next)
			}
			wantReason(t, "stale token", r.Body, auth.ErrCSRFSignIn.Msg)
			wantReason(t, "stale token", r.Body, notSaved)
			if st.Saves() != 0 {
				t.Fatalf("reached the handler %d times", st.Saves())
			}
		})
	}
}

// AUTH-046: an htmx POST keeps its 403 and text; the page behind it is still there.
func TestCSRF_HTMXKeepsItsAnswer(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	r := signedIn(t, h, t10Priya).HTMX().NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "htmx POST with no token", r, http.StatusForbidden)
	if !strings.Contains(r.Header.Get("HX-Trigger"), auth.ErrCSRF.Msg) {
		t.Errorf("HX-Trigger = %q, want %q", r.Header.Get("HX-Trigger"), auth.ErrCSRF.Msg)
	}
	if strings.Contains(r.Body, "/signin") {
		t.Errorf("htmx body = %q, want no sign-in page", r.Body)
	}
}

// A stale sign-in page gets a fresh one with the email and next kept.
func TestLogin_StaleTokenGetsTheForm(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	r := h.Client().NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}, "next": {"/po/list"}})
	wantStatus(t, "POST /signin with a stale token", r, http.StatusForbidden)
	if got := nextOf(t, r.Body); got != "/po/list" {
		t.Errorf("next = %q", got)
	}
	wantReason(t, "stale sign-in", r.Body, auth.ErrCSRF.Msg)
	if !strings.Contains(r.Body, "/signin?next=%2Fpo%2Flist") {
		t.Errorf("the link keeps no next: %q", r.Body)
	}

	// With a session, the form itself, the email kept.
	c := h.Client()
	wantStatus(t, "GET /signin", c.Get("/signin"), http.StatusOK)
	r = c.NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}, "next": {"/po/list"}})
	wantStatus(t, "POST /signin with a stale token", r, http.StatusForbidden)
	if !strings.Contains(r.Body, t10Priya) {
		t.Errorf("the email isn't kept: %q", r.Body)
	}
}

// AUTH-047: a refused POST with no session writes no session row, on /signin or
// on a guarded route, signed out or expired.
func TestSignIn_RefusedPostWritesNoRow(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	before := sessionRows(t, w)
	for range 3 {
		h.Client().NoCSRF().Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}})
		h.Client().NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}})
	}
	if got := sessionRows(t, w); got != before {
		t.Errorf("session rows = %d, want %d", got, before)
	}
}

// A stale token never signs anyone out, and the page says so: still signed in,
// with a sign-out form carrying the live token, which works.
func TestSignOut_StaleTokenStaysSignedIn(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	before := t11SignedInSessions(t, w)
	r := c.NoCSRF().Post("/signout", url.Values{"csrf_token": {"stale"}})
	wantStatus(t, "POST /signout with a stale token", r, http.StatusForbidden)
	wantReason(t, "stale sign-out", r.Body, auth.ErrSignOutRefused.Msg)
	if strings.Contains(r.Body, "thing to fix") || strings.Contains(r.Body, "entries are kept") {
		t.Errorf("the page shows an error summary for a sign-out: %q", r.Body)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	form := doc.Find(`main form[action="/signout"]`)
	token := form.Find("input[name=csrf_token]").AttrOr("value", "")
	if form.Length() != 1 || token == "" || token == "stale" {
		t.Fatalf("no fresh sign-out form: %q", r.Body)
	}
	if doc.Find(`form[action="/signin"]`).Length() != 0 {
		t.Errorf("a signed-in user is offered sign-in: %q", r.Body)
	}
	if got := t11SignedInSessions(t, w); got != before {
		t.Errorf("sessions = %d, want %d: a stale token signed someone out", got, before)
	}
	wantStatus(t, "the session lives", c.Get("/po/list"), http.StatusOK)
	wantRedirect(t, "sign-out with the fresh token", c.NoCSRF().Post("/signout", url.Values{"csrf_token": {token}}), http.StatusSeeOther, "/signin")
	if got := t11SignedInSessions(t, w); got != before-1 {
		t.Errorf("sessions after a real sign-out = %d, want %d", got, before-1)
	}
}

// M7: GET and htmx both land on sign-in with the reason, which stays until a
// sign-in (two requests of one page can both hit expiry) and never shows for
// someone who wasn't signed in.
func TestSignIn_ShowsWhyAfterExpiry(t *testing.T) {
	for _, tc := range []struct {
		name string
		hit  func(c *webtest.Client) webtest.Response
		want int
	}{
		{"GET", func(c *webtest.Client) webtest.Response { return c.Get("/po/list") }, http.StatusSeeOther},
		{"htmx", func(c *webtest.Client) webtest.Response { return c.HTMX().Get("/po/list") }, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := authtest.NewWorld(t)
			h, _ := t10Harness(t, w)
			w.SetPassword(t, t10Priya, t10Password)
			c := signedIn(t, h, t10Priya)
			w.Clock.Advance(12*time.Hour + time.Second)
			wantStatus(t, "request after expiry", tc.hit(c), tc.want)
			for range 2 {
				wantReason(t, "sign-in after expiry", c.Get("/signin").Body, auth.ErrSessionExpired.Msg)
			}
			if r := c.Post("/signin", url.Values{"email": {t10Priya}, "password": {t10Password}}); r.Status != http.StatusSeeOther {
				t.Fatalf("sign-in: status %d", r.Status)
			}
			if body := c.Get("/signin").Body; strings.Contains(body, auth.ErrSessionExpired.Msg) {
				t.Errorf("the reason outlived the sign-in: %q", body)
			}
		})
	}

	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := h.Client()
	c.Get("/po/list")
	if body := c.Get("/signin").Body; strings.Contains(body, auth.ErrSessionExpired.Msg) || strings.Contains(body, auth.ErrSignInRequired.Msg) {
		t.Errorf("a first visit gets a reason: %q", body)
	}
}

// expiredSet is the Set-Cookie line of the expiry mark, "" with none.
func expiredSet(r webtest.Response) string {
	for _, c := range r.Header.Values("Set-Cookie") {
		if strings.HasPrefix(c, "signin_expired=") {
			return c
		}
	}
	return ""
}

// The mark holds no text, reaches only the sign-in page, and is short-lived;
// only a request that goes on to sign-in sets it.
func TestSignIn_ExpiryMark(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	w.Clock.Advance(12*time.Hour + time.Second)

	const want = "signin_expired=1; Path=/signin; Max-Age=600; HttpOnly; SameSite=Lax"
	if got := expiredSet(c.Get("/po/list")); got != want {
		t.Errorf("GET: Set-Cookie = %q, want %q", got, want)
	}
	for _, tc := range []struct {
		name string
		r    func() webtest.Response
	}{
		{"plain POST", func() webtest.Response { return c.Post("/po/1/save", url.Values{"name": {"x"}}) }},
		{"API GET", func() webtest.Response { return c.Get("/api/po/1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := expiredSet(tc.r()); got != "" {
				t.Errorf("Set-Cookie = %q, want none: this request doesn't go on to sign-in", got)
			}
		})
	}
}
