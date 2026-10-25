package authweb

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// render writes c with status. Anything put into the session must happen
// before this call: scs saves the session at the first write.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "render", "path", r.URL.Path, "err", err)
	}
}

// userError returns err's status and text when it is a user-facing error
// below 500 (CD-44).
func userError(err error) (int, string, bool) {
	var se interface{ Status() int }
	if errors.As(err, &se) && se.Status() < http.StatusInternalServerError {
		return se.Status(), err.Error(), true
	}
	return 0, "", false
}

// getSignIn renders the sign-in form; CSRFToken creates the guest session
// and its token.
func (s *Server) getSignIn(w http.ResponseWriter, r *http.Request) {
	page := LoginPage{CSRF: CSRFToken(r.Context()), Next: r.URL.Query().Get("next")}
	render(w, r, http.StatusOK, s.v.Login(page))
}

// postSignIn checks the CSRF token itself (the route is Public), signs in,
// renews the session token and rotates the CSRF token (Review Focus 1).
func (s *Server) postSignIn(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		s.WriteError(w, r, auth.ErrCSRF)
		return
	}
	ctx := r.Context()
	next := r.PostFormValue("next")
	res, err := s.a.SignIn(ctx, auth.SignInInput{
		Email: r.PostFormValue("email"), Password: r.PostFormValue("password"),
		IP: ClientIP(r, s.cfg.TrustProxy), UserAgent: r.UserAgent(),
	})
	if err != nil {
		status, msg, ok := userError(err)
		if !ok {
			s.WriteError(w, r, err)
			return
		}
		// The email is not re-filled (AUTH-004).
		render(w, r, status, s.v.Login(LoginPage{CSRF: CSRFToken(ctx), Error: msg, Next: next}))
		return
	}
	if err := s.sm.RenewToken(ctx); err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.sm.Put(ctx, auth.SessionKeyUser, int64(res.User))
	s.sm.Put(ctx, auth.SessionKeyCSRF, newCSRFToken())
	loc := SafeNext(next)
	if res.MustChangePassword {
		loc = "/account/password"
	}
	http.Redirect(w, r, loc, http.StatusSeeOther) //nolint:gosec // loc is SafeNext (local paths only) or a constant
}

// postSignOut destroys the session (so no empty session is saved) and logs
// the sign-out.
func (s *Server) postSignOut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.sm.Destroy(ctx); err != nil {
		s.WriteError(w, r, err)
		return
	}
	if err := s.a.LogSignOut(ctx, p.ID, ClientIP(r, s.cfg.TrustProxy), r.UserAgent()); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/signin")
}
