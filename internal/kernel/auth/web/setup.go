package authweb

import (
	"errors"
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// getSetup renders the first-administrator form while no user exists.
func (s *Server) getSetup(w http.ResponseWriter, r *http.Request) {
	done, err := s.a.SetupDone(r.Context())
	if err == nil && done {
		err = auth.ErrNotFound
	}
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	render(w, r, http.StatusOK, s.v.Setup(SetupPage{CSRF: CSRFToken(r.Context())}))
}

// postSetup checks the CSRF token itself (the route is Public) and creates
// the first Administrator.
func (s *Server) postSetup(w http.ResponseWriter, r *http.Request) {
	if !s.validCSRF(r) {
		s.WriteError(w, r, auth.ErrCSRF)
		return
	}
	ctx := r.Context()
	email, name := r.PostFormValue("email"), r.PostFormValue("name")
	_, err := s.a.CreateFirstAdmin(ctx, email, name, r.PostFormValue("password"))
	if errors.Is(err, auth.ErrUsersExist) {
		err = auth.ErrSetupDone
	}
	if err != nil {
		status, msg, ok := userError(err)
		if !ok || errors.Is(err, auth.ErrSetupDone) {
			s.WriteError(w, r, err)
			return
		}
		render(w, r, status, s.v.Setup(SetupPage{CSRF: CSRFToken(ctx), Error: msg, Email: email, Name: name}))
		return
	}
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}
