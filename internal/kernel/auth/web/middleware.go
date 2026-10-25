package authweb

import (
	"context"
	"errors"
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/web"
)

type (
	expiredKey   struct{}
	userStateKey struct{}
)

// Middleware records the request meta, loads and saves the session, lets
// web.Flash and CSRFToken reach it and loads the principal.
func (s *Server) Middleware(next http.Handler) http.Handler {
	principal := s.loadPrincipal(next)
	load := s.sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal.ServeHTTP(w, r.WithContext(web.WithSessions(r.Context(), s.sm)))
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var hash string
		if c, err := r.Cookie(s.sm.Cookie.Name); err == nil && c.Value != "" {
			hash = auth.HashToken(c.Value)
		}
		ctx := auth.WithRequestMeta(r.Context(), hash, ClientIP(r, s.cfg.TrustProxy), r.UserAgent())
		load.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loadPrincipal stores the session user's Principal and UserState in the
// context. A missing or disabled user destroys the session; that, or a
// presented cookie whose session was not found, marks the request expired.
func (s *Server) loadPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		presented := auth.PresentedHash(ctx) != ""
		// A stored session always holds a value, so none means the presented
		// session was not found (expired or revoked). Invariant: no handler may
		// save a session with zero values, or it would read as expired here.
		expired := presented && len(s.sm.Keys(ctx)) == 0
		if uid, ok := s.sm.Get(ctx, auth.SessionKeyUser).(int64); ok {
			p, st, err := s.a.LoadPrincipal(ctx, auth.UserID(uid))
			switch {
			case errors.Is(err, auth.ErrNotFound) || (err == nil && !st.Enabled):
				if err := s.sm.Destroy(ctx); err != nil {
					s.WriteError(w, r, err)
					return
				}
				expired = true
			case err != nil:
				s.WriteError(w, r, err)
				return
			default:
				ctx = auth.WithPrincipal(ctx, p)
				ctx = context.WithValue(ctx, userStateKey{}, st)
			}
		}
		// A session with values gets its token now, before anything can start
		// the response; only a new guest session creates it lazily (CSRFToken).
		if len(s.sm.Keys(ctx)) > 0 && s.sm.GetString(ctx, auth.SessionKeyCSRF) == "" {
			s.sm.Put(ctx, auth.SessionKeyCSRF, newCSRFToken())
		}
		cs := &csrfState{sm: s.sm}
		ctx = context.WithValue(ctx, csrfKey{}, cs)
		if expired {
			ctx = context.WithValue(ctx, expiredKey{}, true)
		}
		next.ServeHTTP(&startWriter{ResponseWriter: w, cs: cs}, r.WithContext(ctx))
	})
}

func isExpired(ctx context.Context) bool {
	e, _ := ctx.Value(expiredKey{}).(bool)
	return e
}

func userState(ctx context.Context) auth.UserState {
	st, _ := ctx.Value(userStateKey{}).(auth.UserState)
	return st
}
