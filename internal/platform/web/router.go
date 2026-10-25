// Package web is the HTTP platform: the router and its middleware, error
// responses, flash messages and the content-hashed static files. It imports no
// kernel package (CD-90).
package web

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Options configures NewRouter; Sessions and Auth may each be nil, in which
// case that step is skipped. Leave Sessions nil when AUTH's middleware is
// wired as Auth: AUTH loads and saves the session itself and calls
// WithSessions, so setting both saves each session twice and every request
// fails (CD-150). Sessions is for a router without AUTH's middleware.
type Options struct {
	Logger         *slog.Logger
	Sessions       *scs.SessionManager
	Auth           func(http.Handler) http.Handler
	TrustedOrigins []string
}

// NewRouter returns a router with the middleware in this order: request ID,
// access log, recover (500 page), security headers, cross-origin protection,
// session load, then opts.Auth.
func NewRouter(opts Options) chi.Router {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	cop := http.NewCrossOriginProtection()
	for _, origin := range opts.TrustedOrigins {
		if err := cop.AddTrustedOrigin(origin); err != nil {
			panic(fmt.Sprintf("web: trusted origin %q is not valid (%v); use the form scheme://host[:port]", origin, err))
		}
	}
	cop.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Error(w, r, http.StatusForbidden, crossOriginText)
	}))

	r := chi.NewRouter()
	r.Use(middleware.RequestID, accessLog(logger), recoverer, securityHeaders, cop.Handler)
	if opts.Sessions != nil {
		r.Use(loadSession(opts.Sessions))
	}
	if opts.Auth != nil {
		r.Use(opts.Auth)
	}
	return r
}
