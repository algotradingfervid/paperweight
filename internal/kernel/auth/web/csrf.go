package authweb

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

type csrfKey struct{}

// csrfState lets CSRFToken reach the session and see whether the response has
// started (scs saves the session at the first Write or WriteHeader).
type csrfState struct {
	sm      *scs.SessionManager
	started bool
}

// startWriter marks the response started at its first Write or WriteHeader.
type startWriter struct {
	http.ResponseWriter
	cs *csrfState
}

func (w *startWriter) WriteHeader(code int) {
	w.cs.started = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *startWriter) Write(b []byte) (int, error) {
	w.cs.started = true
	return w.ResponseWriter.Write(b)
}

func (w *startWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// CSRFToken returns the session's CSRF token (CD-15): the UI sends it as the
// X-CSRF-Token header (hx-headers) and the csrf_token form field. A session
// with values gets its token from the middleware; in a new guest session the
// first call creates and stores it, so call it before the response is
// written. Created any later, the token could not be saved: CSRFToken logs an
// error and returns "", so the form is visibly broken rather than stale.
func CSRFToken(ctx context.Context) string {
	cs, ok := ctx.Value(csrfKey{}).(*csrfState)
	if !ok {
		return ""
	}
	tok := cs.sm.GetString(ctx, auth.SessionKeyCSRF)
	if tok == "" {
		if cs.started {
			slog.ErrorContext(ctx, "CSRFToken called after the response started; the token could not be saved")
			return ""
		}
		tok = newCSRFToken()
		cs.sm.Put(ctx, auth.SessionKeyCSRF, tok)
	}
	return tok
}

func newCSRFToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error
	return base64.RawURLEncoding.EncodeToString(b)
}

// validCSRF reports whether r carries the session's stored token, in the
// X-CSRF-Token header or else the csrf_token form field.
func (s *Server) validCSRF(r *http.Request) bool {
	want := s.sm.GetString(r.Context(), auth.SessionKeyCSRF)
	got := r.Header.Get("X-CSRF-Token")
	if got == "" {
		got = r.PostFormValue("csrf_token")
	}
	return want != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
