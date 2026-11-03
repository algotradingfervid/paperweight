package authweb

import (
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// Kind is what a guard requires.
type Kind int

// Guard kinds.
const (
	KindPublic Kind = iota
	KindSelf
	KindRight
	KindAdmin
	KindCheckedInHandler // the handler calls Can or Scope itself (CD-46)
)

// Requirement is a route's declared permission.
type Requirement struct {
	Kind    Kind
	DocType string
	Right   auth.Right
	Msg     string // KindAdmin: the 403 text
}

// Guarded is a handler behind a declared Requirement; CheckRoutes looks for it.
type Guarded struct {
	Req  Requirement
	next http.Handler
	s    *Server
}

// Public lets everyone through.
func Public(h http.Handler) http.Handler {
	return &Guarded{Req: Requirement{Kind: KindPublic}, next: h}
}

// Self requires a signed-in user.
func (s *Server) Self(h http.Handler) http.Handler {
	return &Guarded{Req: Requirement{Kind: KindSelf}, next: h, s: s}
}

// Right requires right r on doctype.
func (s *Server) Right(doctype string, r auth.Right, h http.Handler) http.Handler {
	return &Guarded{Req: Requirement{Kind: KindRight, DocType: doctype, Right: r}, next: h, s: s}
}

// Admin requires the Administrator role; others get 403 with msg.
func (s *Server) Admin(msg string, h http.Handler) http.Handler {
	return &Guarded{Req: Requirement{Kind: KindAdmin, Msg: msg}, next: h, s: s}
}

// CheckedInHandler requires a signed-in user and declares that h checks the
// right itself and answers through WriteError (CD-46). Only for routes under
// CheckedInHandlerPrefixes.
func (s *Server) CheckedInHandler(h http.Handler) http.Handler {
	return &Guarded{Req: Requirement{Kind: KindCheckedInHandler}, next: h, s: s}
}

// ownerFields are set by the server, never by a form (C9).
var ownerFields = []string{"created_by", "created_at", "updated_by", "updated_at"}

// errBadForm answers a form body that does not parse.
var errBadForm = auth.ErrBadForm

func (g *Guarded) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.Req.Kind == KindPublic {
		g.next.ServeHTTP(w, r)
		return
	}
	s, ctx := g.s, r.Context()
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		s.guest(w, r)
		return
	}
	unsafe := !safeMethod(r.Method)
	if unsafe && isURLEncoded(r) {
		if err := r.ParseForm(); err != nil {
			s.WriteError(w, r, errBadForm)
			return
		}
		for _, k := range ownerFields {
			r.PostForm.Del(k)
			r.Form.Del(k)
		}
	}
	if unsafe && !s.validCSRF(r) {
		if isHTMX(r) || isAPI(r) {
			s.WriteError(w, r, auth.ErrCSRF)
			return
		}
		if r.URL.Path == "/signout" {
			// Still CSRF-protected, so still signed in; the page says so.
			s.renderPage(w, r, http.StatusForbidden, s.v.Message(MessagePage{CSRF: CSRFToken(ctx), Title: "Sign out", Message: auth.ErrSignOutRefused.Msg, SignOut: true}))
			return
		}
		s.signInPage(w, r, http.StatusForbidden, auth.ErrCSRFSignIn.Msg, "", localPage(r, r.Referer()))
		return
	}
	if userState(ctx).MustChangePassword && r.URL.Path != "/account/password" && r.URL.Path != "/signout" {
		redirect(w, r, "/account/password")
		return
	}
	switch g.Req.Kind {
	case KindRight:
		if err := s.a.Can(ctx, p, g.Req.DocType, g.Req.Right, nil); err != nil {
			s.WriteError(w, r, err)
			return
		}
	case KindAdmin:
		admin, err := s.a.HasRole(ctx, p.ID, auth.RoleAdministrator)
		if err != nil {
			s.WriteError(w, r, err)
			return
		}
		if !admin {
			web.Error(w, r, http.StatusForbidden, g.Req.Msg)
			return
		}
	}
	g.next.ServeHTTP(w, r)
}

// msgNotSaved goes with a plain POST's refusal: an htmx form keeps what was
// typed, but a plain POST's page is gone.
const msgNotSaved = "What you entered on that page wasn't saved."

// expiredCookie tells the sign-in page a session ended (shell M7). It is a
// cookie because a revoked session replayed must write no session row, and it
// holds no text: the page says auth's own message. It lives only as long as a
// redirect needs, and a sign-in clears it.
const (
	expiredCookie = "signin_expired"
	expiredMaxAge = 10 * 60 // seconds
)

// setExpired sets expiredCookie, or clears it with maxAge below 0.
func (s *Server) setExpired(w http.ResponseWriter, maxAge int) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure follows the session cookie's setting, and the cookie holds no secret.
		Name: expiredCookie, Value: "1", Path: "/signin", MaxAge: maxAge,
		HttpOnly: true, Secure: s.sm.Cookie.Secure, SameSite: http.SameSiteLaxMode,
	})
}

// guest answers a request with no principal (CD-17, CD-63). A plain POST gets
// the sign-in page itself, with its status; a request that goes on to sign-in
// leaves the expiry for that page to say.
func (s *Server) guest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	msg := auth.ErrSignInRequired.Msg
	if isExpired(ctx) {
		msg = auth.ErrSessionExpired.Msg
	}
	loc := "/signin?next=" + url.QueryEscape(r.URL.RequestURI())
	signIn := isHTMX(r) || (r.Method == http.MethodGet && !isAPI(r))
	if signIn && isExpired(ctx) {
		s.setExpired(w, expiredMaxAge)
	}
	switch {
	case isHTMX(r):
		w.Header().Set("HX-Redirect", htmxSignIn(r))
		web.Error(w, r, http.StatusUnauthorized, msg)
	case isAPI(r):
		web.Error(w, r, http.StatusUnauthorized, msg)
	case r.Method != http.MethodGet:
		s.signInPage(w, r, http.StatusUnauthorized, msg, "", localPage(r, r.Referer()))
	default:
		http.Redirect(w, r, loc, http.StatusSeeOther)
	}
}

// signInPage answers a browser's plain POST refused for its session or its
// form token: the way to sign in with the reason, returning to next by GET, so
// nothing replays (CD-382). With a session it is the sign-in form. With none,
// a form would need a token and so a new session row on every such POST
// (AUTH-047), so it is a page linking to the form.
func (s *Server) signInPage(w http.ResponseWriter, r *http.Request, status int, reason, email, next string) {
	reasons := []string{reason, msgNotSaved}
	if r.URL.Path == "/signin" {
		reasons = reasons[:1] // no other page's entries were lost
	}
	token := s.sm.GetString(r.Context(), auth.SessionKeyCSRF)
	if token != "" {
		render(w, r, status, s.v.Login(LoginPage{CSRF: token, Warnings: reasons, Email: email, Next: next}))
		return
	}
	link := "/signin"
	if next != "" {
		link += "?next=" + url.QueryEscape(next)
	}
	render(w, r, status, s.v.Message(MessagePage{Title: "Sign in", Warnings: reasons, SignIn: link}))
}

// htmxSignIn is the sign-in URL for an htmx request. Its own URL is a fragment
// or a POST, not a page to land on, so next is the page it came from, when
// HX-Current-URL names a local page of this host.
func htmxSignIn(r *http.Request) string {
	next := localPage(r, r.Header.Get("HX-Current-URL"))
	if next == "" {
		return "/signin"
	}
	return "/signin?next=" + url.QueryEscape(next)
}

// localPage is raw's path and query when raw is a URL on r's host that passes
// SafeNext, else "".
func localPage(r *http.Request, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host != r.Host {
		return ""
	}
	next := u.RequestURI()
	if SafeNext(next) != next {
		return ""
	}
	return next
}

// redirect sends a 303 to loc, or HX-Redirect to an htmx request.
func redirect(w http.ResponseWriter, r *http.Request, loc string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", loc)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, loc, http.StatusSeeOther) //nolint:gosec // G710: every caller passes a fixed local path built from ids and an escaped DocType.
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func isAPI(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/api/") }

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions || m == http.MethodTrace
}

func isURLEncoded(r *http.Request) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mt == "application/x-www-form-urlencoded"
}
