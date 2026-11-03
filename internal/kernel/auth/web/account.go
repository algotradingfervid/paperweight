package authweb

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/web"
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

// getResetPassword shows the form that sets user {id}'s temporary password.
// A reset ends the target's sessions, so the viewer's own URL leads to Change
// password instead.
func (s *Server) getResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	if p, _ := auth.PrincipalFrom(r.Context()); p.ID == id {
		redirect(w, r, "/account/password")
		return
	}
	s.renderResetPassword(w, r, http.StatusOK, id, "")
}

// renderResetPassword shows user id's reset form with errText on its field.
func (s *Server) renderResetPassword(w http.ResponseWriter, r *http.Request, status int, id auth.UserID, errText string) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	u, _, _, err := s.a.UserForEdit(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, status, s.v.ResetPassword(ResetPasswordPage{CSRF: CSRFToken(ctx), Error: errText, User: u}))
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
		if status, msg, ok := userError(err); ok && isPasswordProblem(err) {
			s.renderResetPassword(w, r, status, auth.UserID(id), msg)
			return
		}
		s.WriteError(w, r, err)
		return
	}
	if u, _, _, err := s.a.UserForEdit(ctx, p, auth.UserID(id)); err != nil {
		logReread(r, auth.UserID(id), err)
		web.Flash(ctx, "success", "Password reset. They choose a new one when they next sign in.")
	} else {
		web.Flash(ctx, "success", "Password reset for "+u.Name+". They choose a new one when they next sign in.")
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
	u, _, _, err := s.a.UserForEdit(ctx, p, auth.UserID(id))
	switch {
	case err != nil:
		logReread(r, auth.UserID(id), err)
		web.Flash(ctx, "success", "The account is unlocked.")
	case u.Enabled:
		web.Flash(ctx, "success", u.Name+" can sign in again.")
	default:
		web.Flash(ctx, "success", u.Name+" is unlocked, but still disabled. Enable the account to let them sign in.")
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
