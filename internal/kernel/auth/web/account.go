package authweb

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// getPassword renders the change-password form.
func (s *Server) getPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	page := PasswordPage{CSRF: CSRFToken(ctx), Forced: userState(ctx).MustChangePassword}
	render(w, r, http.StatusOK, s.v.Password(page))
}

// postPassword changes the user's own password; the current session stays
// signed in and the user's other sessions end.
func (s *Server) postPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	err := s.a.ChangeOwnPassword(ctx, p.ID, r.PostFormValue("current"), r.PostFormValue("next"),
		r.PostFormValue("confirm"), auth.HashToken(s.sm.Token(ctx)))
	if err != nil {
		status, msg, ok := userError(err)
		if !ok {
			s.WriteError(w, r, err)
			return
		}
		page := PasswordPage{CSRF: CSRFToken(ctx), Error: msg, Forced: userState(ctx).MustChangePassword}
		render(w, r, status, s.v.Password(page))
		return
	}
	redirect(w, r, "/")
}

// postResetPassword sets another user's temporary password (Administrator).
func (s *Server) postResetPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.a.ResetPassword(ctx, p, auth.UserID(id), r.PostFormValue("password")); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, fmt.Sprintf("/users/%d", id))
}
