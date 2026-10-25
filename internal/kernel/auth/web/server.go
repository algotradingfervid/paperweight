// Package authweb is AUTH's HTTP layer: the session middleware, the route
// guards, CSRF tokens, the route inventory and AUTH's handlers.
package authweb

import (
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// Config is the web configuration.
type Config struct {
	Secure     bool // served over HTTPS
	TrustProxy bool // take the client IP from X-Forwarded-For
}

// Server holds what AUTH's middleware, guards and handlers need.
type Server struct {
	a   *auth.Auth
	st  *auth.SessionStore
	sm  *scs.SessionManager
	v   Views
	cfg Config
}

// New returns a Server. platform/web takes sm and Server.Middleware (CD-14).
func New(a *auth.Auth, st *auth.SessionStore, sm *scs.SessionManager, v Views, cfg Config) *Server {
	return &Server{a: a, st: st, sm: sm, v: v, cfg: cfg}
}
