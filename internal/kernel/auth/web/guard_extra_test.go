package authweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func sessionRows(t *testing.T, w *authtest.World) int {
	t.Helper()
	var n int
	err := w.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), "SELECT count(*) FROM sessions").Scan(&n)
	})
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	return n
}

func TestGuard_CSRFBeforeRight(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	deepak := signedIn(t, h, t10Deepak)

	r := deepak.NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "deepak POST without token", r, http.StatusForbidden)
	if !strings.Contains(r.Body, auth.ErrCSRF.Msg) || strings.Contains(r.Body, "permission") {
		t.Fatalf("body = %q, want %q and not the denial", r.Body, auth.ErrCSRF.Msg)
	}
	if st.Saves() != 0 {
		t.Fatalf("reached the handler %d times", st.Saves())
	}
}

func TestGuard_ForcedChangeRedirect(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w, webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.Method(http.MethodPost, "/account/password", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write([]byte("CHANGED"))
		})))
	}))
	c := signedIn(t, h, t10Priya)
	err := w.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), "UPDATE users SET must_change_password = 1 WHERE email = ?", t10Priya)
		return err
	})
	if err != nil {
		t.Fatalf("set must_change_password: %v", err)
	}

	wantRedirect(t, "plain GET /po/list", c.Get("/po/list"), http.StatusSeeOther, "/account/password")

	hx := c.HTMX().Get("/po/list")
	wantStatus(t, "HTMX GET /po/list", hx, http.StatusOK)
	if got := hx.Header.Get("HX-Redirect"); got != "/account/password" {
		t.Fatalf("HX-Redirect = %q, want /account/password", got)
	}
	if strings.Contains(hx.Body, "PO LIST") {
		t.Fatalf("HTMX body leaks PO LIST: %q", hx.Body)
	}

	r := c.Post("/account/password", url.Values{"password": {"x"}})
	wantStatus(t, "POST /account/password with the session token", r, http.StatusOK)
	if !strings.Contains(r.Body, "CHANGED") {
		t.Fatalf("POST /account/password body = %q, want CHANGED", r.Body)
	}
}

func TestGuard_AdminDeniedShowsMsg(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	r := c.Get("/users")
	wantStatus(t, "priya GET /users", r, http.StatusForbidden)
	if !strings.Contains(r.Body, t10AdminMsg) || strings.Contains(r.Body, "USERS") {
		t.Fatalf("body = %q, want %q and no data", r.Body, t10AdminMsg)
	}
}

func TestCSRF_WrongFormToken(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)

	r := c.NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}, "csrf_token": {"wrong"}})
	wantStatus(t, "POST with a wrong csrf_token", r, http.StatusForbidden)
	if !strings.Contains(r.Body, auth.ErrCSRF.Msg) {
		t.Fatalf("body = %q, want %q", r.Body, auth.ErrCSRF.Msg)
	}
	if st.Saves() != 0 {
		t.Fatalf("reached the handler %d times", st.Saves())
	}
}

func TestGuard_MalformedBodyRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	tok := csrfOf(t, c)

	req, err := http.NewRequest(http.MethodPost, h.HTTP.URL+"/po/1/save", strings.NewReader("created_by=3&%zz"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-CSRF-Token", tok)
	req.AddCookie(c.Cookie())
	resp, err := h.HTTP.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if st.Saves() != 0 {
		t.Fatalf("reached the handler %d times", st.Saves())
	}
}

func TestCSRF_LazyToken(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		r.Method(http.MethodGet, "/signin", authweb.Public(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			_, _ = rw.Write([]byte("FORM " + authweb.CSRFToken(r.Context()))) //nolint:gosec // test stub, base64url token
		})))
	}))
	c := h.Client()

	wantStatus(t, "GET /static/app.css", c.Get("/static/app.css"), http.StatusOK)
	wantStatus(t, "guest GET /", c.Get("/"), http.StatusSeeOther)
	if n := sessionRows(t, w); n != 0 {
		t.Fatalf("sessions rows after requests without a form = %d, want 0", n)
	}
	if c.Cookie() != nil {
		t.Fatalf("session cookie set without a form: %v", c.Cookie())
	}

	r := c.Get("/signin")
	wantStatus(t, "GET /signin", r, http.StatusOK)
	if strings.TrimSpace(strings.TrimPrefix(r.Body, "FORM ")) == "" {
		t.Fatalf("GET /signin rendered no token: %q", r.Body)
	}
	if n := sessionRows(t, w); n != 1 {
		t.Fatalf("sessions rows after a form = %d, want 1", n)
	}
	again := c.Get("/signin")
	if again.Body != r.Body {
		t.Fatalf("second form token %q, want the stored %q", again.Body, r.Body)
	}
}

func TestSession_ExpiredPost(t *testing.T) {
	w := authtest.NewWorld(t)
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Priya)
	w.Clock.Advance(12*time.Hour + time.Second)

	r := c.Post("/po/1/save", url.Values{"name": {"x"}})
	wantStatus(t, "POST after expiry", r, http.StatusUnauthorized)
	if !strings.Contains(r.Body, auth.ErrSessionExpired.Msg) {
		t.Fatalf("body = %q, want %q", r.Body, auth.ErrSessionExpired.Msg)
	}
	if st.Saves() != 0 {
		t.Fatalf("reached the handler %d times", st.Saves())
	}
}

// dropCSRF removes the session's CSRF token before the guard runs.
func dropCSRF(h **webtest.Harness) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			(*h).SM.Remove(r.Context(), auth.SessionKeyCSRF)
			next.ServeHTTP(rw, r)
		})
	}
}

func TestCSRF_NoStoredTokenRefused(t *testing.T) {
	w := authtest.NewWorld(t)
	var h *webtest.Harness
	reached := 0
	h, _ = t10Harness(t, w, webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.With(dropCSRF(&h)).Method(http.MethodPost, "/_x/nocsrf", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			reached++
			_, _ = rw.Write([]byte("OK"))
		})))
	}))
	c := signedIn(t, h, t10Priya)

	r := c.NoCSRF().Post("/_x/nocsrf", url.Values{"name": {"x"}})
	wantStatus(t, "POST with no token to a session with no stored token", r, http.StatusForbidden)
	if reached != 0 {
		t.Fatalf("reached the handler %d times", reached)
	}
}

func TestCSRF_SignedInTokenCreatedUpFront(t *testing.T) {
	w := authtest.NewWorld(t)
	var h *webtest.Harness
	h, st := t10Harness(t, w, webtest.WithRoutes(func(r chi.Router, s *authweb.Server) {
		r.With(dropCSRF(&h)).Method(http.MethodGet, "/_x/drop", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
			_, _ = rw.Write([]byte("DROPPED"))
		})))
		r.Method(http.MethodGet, "/_x/late", s.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			_, _ = rw.Write([]byte("FORM "))
			_, _ = rw.Write([]byte(authweb.CSRFToken(r.Context())))
		})))
	}))
	w.Grant(t, "PO Writer", authtest.DTOrder, auth.RightWrite, auth.ScopeAll)
	w.AddRole(t, t10Priya, "PO Writer")
	c := signedIn(t, h, t10Priya)
	wantStatus(t, "GET /_x/drop", c.Get("/_x/drop"), http.StatusOK)

	late := c.Get("/_x/late")
	tok := strings.TrimPrefix(late.Body, "FORM ")
	if tok == "" {
		t.Fatalf("GET /_x/late rendered no token: %q", late.Body)
	}
	r := c.NoCSRF().Post("/po/1/save", url.Values{"name": {"x"}, "csrf_token": {tok}})
	wantStatus(t, "POST with the token rendered after the response started", r, http.StatusOK)
	if st.Saves() != 1 {
		t.Fatalf("saves = %d, want 1", st.Saves())
	}
}

func TestCSRF_LateGuestTokenEmpty(t *testing.T) {
	w := authtest.NewWorld(t)
	h, _ := t10Harness(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		r.Method(http.MethodGet, "/_x/lateform", authweb.Public(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
			_, _ = rw.Write([]byte("FORM "))
			_, _ = rw.Write([]byte(authweb.CSRFToken(r.Context())))
		})))
	}))
	r := h.Client().Get("/_x/lateform")
	wantStatus(t, "GET /_x/lateform", r, http.StatusOK)
	if r.Body != "FORM " {
		t.Fatalf("body = %q, want no token once the response started", r.Body)
	}
	if n := sessionRows(t, w); n != 0 {
		t.Fatalf("sessions rows = %d, want 0", n)
	}
}
