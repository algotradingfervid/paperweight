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
var errBadForm = &auth.Problem{Code: http.StatusBadRequest, Msg: "The form could not be read. Reload the page and try again."}

func (g *Guarded) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if g.Req.Kind == KindPublic {
		g.next.ServeHTTP(w, r)
		return
	}
	s, ctx := g.s, r.Context()
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		guest(w, r)
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
		s.WriteError(w, r, auth.ErrCSRF)
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

// guest answers a request with no principal (CD-17, CD-63).
func guest(w http.ResponseWriter, r *http.Request) {
	msg := auth.ErrSignInRequired.Msg
	if isExpired(r.Context()) {
		msg = auth.ErrSessionExpired.Msg
	}
	loc := "/signin?next=" + url.QueryEscape(r.URL.RequestURI())
	switch {
	case isHTMX(r):
		w.Header().Set("HX-Redirect", loc)
		web.Error(w, r, http.StatusUnauthorized, msg)
	case r.Method != http.MethodGet || strings.HasPrefix(r.URL.Path, "/api/"):
		web.Error(w, r, http.StatusUnauthorized, msg)
	default:
		http.Redirect(w, r, loc, http.StatusSeeOther)
	}
}

// redirect sends a 303 to loc, or HX-Redirect to an htmx request.
func redirect(w http.ResponseWriter, r *http.Request, loc string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", loc)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, loc, http.StatusSeeOther)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions || m == http.MethodTrace
}

func isURLEncoded(r *http.Request) bool {
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return mt == "application/x-www-form-urlencoded"
}
