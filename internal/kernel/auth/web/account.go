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

// postUnlock clears a user's sign-in lockout (Administrator).
func (s *Server) postUnlock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.a.Unlock(ctx, p, auth.UserID(id)); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, fmt.Sprintf("/users/%d", id))
}

// getSessions lists the signed-in user's sessions.
func (s *Server) getSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	page := SessionsPage{CSRF: CSRFToken(ctx)}
	var err error
	page.Sessions, err = s.st.ListForUser(ctx, p.ID, auth.HashToken(s.sm.Token(ctx)))
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	render(w, r, http.StatusOK, s.v.Sessions(page))
}

// postRevokeSession ends one of the signed-in user's sessions; another
// user's session id is not found.
func (s *Server) postRevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.st.RevokeForUser(ctx, p.ID, id); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/account/sessions")
}

// postRevokeOthers ends the signed-in user's other sessions; the current one
// stays.
func (s *Server) postRevokeOthers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.st.RevokeOthers(ctx, p.ID, auth.HashToken(s.sm.Token(ctx))); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/account/sessions")
}
