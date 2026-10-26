package authweb

import (
	"bytes"
	"net/http"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
)

// accessPath is a user's "Site and project access" page.
func accessPath(id auth.UserID) string { return userPath(id) + "/access" }

// getAccess shows one user's site and project restrictions with Remove and
// Add forms (Task 20).
func (s *Server) getAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	u, version, values, err := s.a.UserAccess(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page := authscreens.AccessPage{
		User: u, Version: version, Post: userPath(id) + "/restrictions", Values: values,
		PickerURL: map[auth.Dimension]string{},
	}
	for _, d := range []auth.Dimension{auth.DimProject, auth.DimSite} {
		dt, _ := auth.MasterFor(d)
		page.PickerURL[d] = s.cfg.PickerURL(dt)
	}
	s.renderPage(w, r, authscreens.Access(page))
}

// renderPage renders c into a buffer before writing, so the flash the layout
// takes is saved with the session and a render error is a 500 (CD-160).
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		s.WriteError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = buf.WriteTo(w)
}
