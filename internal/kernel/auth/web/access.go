package authweb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
)

// accessPath is a user's "Site and project access" page.
func accessPath(id auth.UserID) string { return fmt.Sprintf("/users/%d/access", id) }

// getAccess shows one user's site and project restrictions with Remove and
// Add forms (Task 20). It refuses what accessPage refuses.
func (s *Server) getAccess(w http.ResponseWriter, r *http.Request) {
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page, err := s.accessPage(r.Context(), id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, http.StatusOK, authscreens.Access(page))
}

// accessPage builds user id's access page. It refuses a non-admin with
// ErrAdminOnlyAccess and an unknown user with ErrNotFound; a Server with no
// Config.PickerURL, or a dimension with no master, is a plain internal error.
func (s *Server) accessPage(ctx context.Context, id auth.UserID) (authscreens.AccessPage, error) {
	p, _ := auth.PrincipalFrom(ctx)
	u, version, values, err := s.a.UserAccess(ctx, p, id)
	if err != nil {
		return authscreens.AccessPage{}, err
	}
	if s.cfg.PickerURL == nil {
		return authscreens.AccessPage{}, errors.New("authweb: Config.PickerURL is not set")
	}
	page := authscreens.AccessPage{
		User: u, UserPath: userPath(id), Version: version, Post: userPath(id) + "/restrictions", Values: values,
		PickerURL: map[auth.Dimension]string{},
	}
	for _, d := range []auth.Dimension{auth.DimProject, auth.DimSite} {
		dt, ok := auth.MasterFor(d)
		if !ok {
			return authscreens.AccessPage{}, fmt.Errorf("authweb: no master for dimension %q", d)
		}
		page.PickerURL[d] = s.cfg.PickerURL(dt)
	}
	return page, nil
}

// emptyAddError is the field error for an Add form posted with nothing
// picked: add present and blank, nothing to remove, on a real dimension
// (CD-194). It is nil for any other form.
func emptyAddError(r *http.Request, dim auth.Dimension) *auth.Problem {
	notBlank := func(v string) bool { return v != "" }
	add := r.PostForm["add"]
	if len(add) == 0 || slices.ContainsFunc(add, notBlank) || slices.ContainsFunc(r.PostForm["remove"], notBlank) {
		return nil
	}
	switch dim {
	case auth.DimSite:
		return auth.ErrChooseSite
	case auth.DimProject:
		return auth.ErrChooseProject
	}
	return nil
}

// writeEmptyAdd re-renders the access page with problem on dim's picker and
// in the summary. Its forms keep the posted version, so a change made since
// the user opened the page still stops the next save (CD-94).
func (s *Server) writeEmptyAdd(w http.ResponseWriter, r *http.Request, id auth.UserID, dim auth.Dimension, version int64, problem *auth.Problem) {
	page, err := s.accessPage(r.Context(), id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page.Version, page.ErrorDim, page.Error = version, dim, problem.Msg
	s.renderPage(w, r, problem.Code, authscreens.Access(page))
}

// renderPage renders c into a buffer before writing, then sends status, so
// the flash the layout takes is saved with the session and a render error is
// a 500 (CD-160).
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		s.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}
