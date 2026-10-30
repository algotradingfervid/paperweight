// Package webtest runs authweb behind an httptest server for tests.
package webtest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
)

// loginPath is the harness-only sign-in route behind Client.Login. It sits
// outside Router, so CheckRoutes never sees it.
const loginPath = "/_test/login"

// csrfEchoHeader carries the session's stored CSRF token on every harness
// response, so a Client can post without fetching it.
const csrfEchoHeader = "X-Test-Csrf"

// tokenWriter sets csrfEchoHeader from the session just before the response is
// written; it sits inside LoadAndSave, so the session is still in memory.
type tokenWriter struct {
	http.ResponseWriter
	r        *http.Request
	sm       *scs.SessionManager
	written  bool
	reported string
}

func (tw *tokenWriter) setHeader() {
	if tw.written {
		return
	}
	tw.written = true
	tw.reported = tw.sm.GetString(tw.r.Context(), auth.SessionKeyCSRF)
	if tw.reported != "" {
		tw.Header().Set(csrfEchoHeader, tw.reported)
	}
}

func (tw *tokenWriter) WriteHeader(code int) {
	tw.setHeader()
	tw.ResponseWriter.WriteHeader(code)
}

func (tw *tokenWriter) Write(b []byte) (int, error) {
	tw.setHeader()
	return tw.ResponseWriter.Write(b)
}

func (tw *tokenWriter) Unwrap() http.ResponseWriter { return tw.ResponseWriter }

// withTokenHeader also fails t when the handler changed the stored CSRF token
// after the response started: scs had saved the session by then, so the
// change was lost.
func withTokenHeader(t testing.TB, sm *scs.SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tw := &tokenWriter{ResponseWriter: w, r: r, sm: sm}
			next.ServeHTTP(tw, r)
			if !tw.written {
				tw.setHeader()
				return
			}
			if tok := sm.GetString(r.Context(), auth.SessionKeyCSRF); tok != tw.reported {
				t.Errorf("%s %s: CSRF token changed after the response started (saved %q, now %q)", r.Method, r.URL.Path, tw.reported, tok)
			}
		})
	}
}

// Harness is an authweb.Server over a World, served by an httptest server.
type Harness struct {
	W      *authtest.World
	Srv    *authweb.Server
	Router chi.Router
	HTTP   *httptest.Server
	SM     *scs.SessionManager

	t          testing.TB
	cookieName string
}

type options struct {
	tls, trustProxy, probe bool
	routes                 []func(chi.Router, *authweb.Server)
}

// Option configures New.
type Option func(*options)

// WithTLS serves over HTTPS (Secure cookie named __Host-session).
func WithTLS() Option { return func(o *options) { o.tls = true } }

// WithTrustProxy sets Config.TrustProxy.
func WithTrustProxy() Option { return func(o *options) { o.trustProxy = true } }

// WithRoutes adds routes to the router after Srv.Mount.
func WithRoutes(fn func(chi.Router, *authweb.Server)) Option {
	return func(o *options) { o.routes = append(o.routes, fn) }
}

// New builds the harness: Srv.Middleware, the CSRF token response header,
// Srv.Mount, GET /_test/csrf (Self, the session's CSRF token as the body),
// GET /static/app.css (unguarded) and the WithRoutes routes.
func New(t testing.TB, w *authtest.World, opts ...Option) *Harness {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	cfg := auth.DefaultSessionConfig(o.tls)
	st := auth.NewSessionStore(w.DB, w.Clock, cfg)
	sm := auth.NewSessionManager(st, cfg)
	srv := authweb.New(w.Auth, st, sm, views.Default{}, authweb.Config{Secure: o.tls, TrustProxy: o.trustProxy, PickerURL: pickerURL})

	r := chi.NewRouter()
	r.Use(srv.Middleware, withTokenHeader(t, sm))
	srv.Mount(r)
	r.Method(http.MethodGet, "/_test/csrf", srv.Self(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		_, _ = rw.Write([]byte(authweb.CSRFToken(r.Context())))
	})))
	r.Get("/static/app.css", func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "text/css")
		_, _ = rw.Write([]byte("body{}"))
	})
	for _, fn := range o.routes {
		fn(r, srv)
	}
	if o.probe {
		mountProbe(t, r, srv, w)
	}

	login := srv.Middleware(withTokenHeader(t, sm)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		res, err := w.Auth.SignIn(r.Context(), auth.SignInInput{
			Email: r.PostFormValue("email"), Password: r.PostFormValue("password"),
			IP: authweb.ClientIP(r, o.trustProxy), UserAgent: r.UserAgent(),
		})
		if err == nil {
			err = sm.RenewToken(r.Context())
		}
		if err != nil {
			srv.WriteError(rw, r, err)
			return
		}
		sm.Put(r.Context(), auth.SessionKeyUser, int64(res.User))
		sm.Remove(r.Context(), auth.SessionKeyCSRF) // a new token with the new session token
		authweb.CSRFToken(r.Context())
		_, _ = rw.Write([]byte("SIGNED IN"))
	})))
	mux := http.NewServeMux()
	mux.Handle("POST "+loginPath, login)
	mux.Handle("/", r)

	var hs *httptest.Server
	if o.tls {
		hs = httptest.NewTLSServer(mux)
	} else {
		hs = httptest.NewServer(mux)
	}
	t.Cleanup(hs.Close)
	return &Harness{W: w, Srv: srv, Router: r, HTTP: hs, SM: sm, t: t, cookieName: sm.Cookie.Name}
}
