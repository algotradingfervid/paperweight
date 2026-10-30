package authweb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
)

// getCopyRights shows the page that copies another role's rights to role
// {id}. It is the confirm step (design-system §4 rule 9, CD-200).
func (s *Server) getCopyRights(w http.ResponseWriter, r *http.Request) {
	id, err := pathRole(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page, err := s.copyRightsPage(r.Context(), id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, http.StatusOK, authscreens.CopyRights(page))
}

// copyRightsPage builds role id's page. It refuses a missing role and the
// Administrator role as the copy itself does.
func (s *Server) copyRightsPage(ctx context.Context, id int64) (authscreens.CopyRightsPage, error) {
	p, _ := auth.PrincipalFrom(ctx)
	roles, _, err := s.a.RightsMatrix(ctx, p)
	if err != nil {
		return authscreens.CopyRightsPage{}, err
	}
	i := slices.IndexFunc(roles, func(r auth.RoleInfo) bool { return r.ID == id })
	if i < 0 {
		return authscreens.CopyRightsPage{}, auth.ErrNotFound
	}
	if roles[i].IsAdmin {
		return authscreens.CopyRightsPage{}, auth.ErrAdminRoleFixed
	}
	page := authscreens.CopyRightsPage{
		Role: roles[i].Name, Post: fmt.Sprintf("/roles/%d/copy-rights", id), Version: roles[i].Version,
	}
	for _, r := range roles {
		if !r.IsAdmin && r.ID != id {
			page.Sources = append(page.Sources, r)
		}
	}
	return page, nil
}

// postCopyRights copies the picked role's rights. A source that can't be used
// keeps the page with the message on the field; the page keeps the posted
// version, so a change made since it was opened still stops the copy (CD-94).
func (s *Server) postCopyRights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, version, err := roleForm(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	if source, perr := strconv.ParseInt(r.PostFormValue("source_id"), 10, 64); perr != nil {
		err = auth.ErrCopySourceRequired
	} else {
		err = s.a.CopyRightsID(ctx, p, id, source, version)
	}
	var problem *auth.Problem
	switch {
	case err == nil:
		redirect(w, r, "/roles")
	case errors.As(err, &problem) && problem.Code == http.StatusUnprocessableEntity:
		page, err := s.copyRightsPage(ctx, id)
		if err != nil {
			s.WriteError(w, r, err)
			return
		}
		page.Version, page.Error = version, problem.Msg
		s.renderPage(w, r, problem.Code, authscreens.CopyRights(page))
	default:
		s.WriteError(w, r, err)
	}
}
