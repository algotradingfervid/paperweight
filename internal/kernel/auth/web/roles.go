package authweb

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// Role and rights administration (plan Task 17, design-system §8.6). The
// rename, delete and rights forms post the role's version; the write finds the
// role by ID and refuses a stale version (CD-94, CD-146 wording).

// What the page after a save says.
const (
	msgReadAdded   = "Read was added, because every right needs it."
	msgRightsSaved = "Rights saved."
)

// rightsMatrix is RightsMatrix's matrix: DocType, then role ID.
type rightsMatrix = map[string]map[int64]map[auth.Right]auth.Scope

// parseForm parses the urlencoded form; another body type is errBadForm.
func parseForm(r *http.Request) error {
	if !isURLEncoded(r) || r.ParseForm() != nil {
		return errBadForm
	}
	return nil
}

// pathRole is the role {id} of the URL; ErrNotFound when it is not a number.
func pathRole(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, auth.ErrNotFound
	}
	return id, nil
}

// roleForm is the role {id} and the posted version.
func roleForm(r *http.Request) (id, version int64, err error) {
	if id, err = pathRole(r); err != nil {
		return 0, 0, err
	}
	v, err := formVersion(r)
	if err != nil {
		return 0, 0, err
	}
	return id, *v, nil
}

// loadRole is role id with the whole rights matrix, read together, so a page's
// rights and the version it posts match; ErrNotFound when there is no such role.
func (s *Server) loadRole(ctx context.Context, id int64) (auth.RoleInfo, rightsMatrix, error) {
	p, _ := auth.PrincipalFrom(ctx)
	roles, matrix, err := s.a.RightsMatrix(ctx, p)
	if err != nil {
		return auth.RoleInfo{}, nil, err
	}
	i := slices.IndexFunc(roles, func(r auth.RoleInfo) bool { return r.ID == id })
	if i < 0 {
		return auth.RoleInfo{}, nil, auth.ErrNotFound
	}
	return roles[i], matrix, nil
}

// docTypesByLabel is every DocType in label order.
func docTypesByLabel() []*doctype.DocType {
	all := slices.Clone(doctype.All())
	slices.SortStableFunc(all, func(a, b *doctype.DocType) int { return strings.Compare(a.Label, b.Label) })
	return all
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	roles, _, err := s.a.RightsMatrix(ctx, p)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, http.StatusOK, authscreens.Roles(authscreens.RolesPage{Roles: roles}))
}

// getRole shows role {id}: a card per DocType, or one line for Administrator.
func (s *Server) getRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathRole(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	role, matrix, err := s.loadRole(r.Context(), id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page := authscreens.RolePage{Role: role}
	for _, dt := range docTypesByLabel() {
		page.Cards = append(page.Cards, authscreens.RoleCard{DocType: dt, Rights: matrix[dt.Name][id]})
	}
	s.renderPage(w, r, http.StatusOK, authscreens.Role(page))
}

// rightsPage is role id's rights page on the DocType name. It refuses a missing
// role or DocType, and the Administrator role as a save does.
func (s *Server) rightsPage(ctx context.Context, id int64, name string) (authscreens.RightsPage, error) {
	role, matrix, err := s.loadRole(ctx, id)
	if err != nil {
		return authscreens.RightsPage{}, err
	}
	dt, ok := doctype.Lookup(name)
	if !ok {
		return authscreens.RightsPage{}, auth.ErrNotFound
	}
	if role.IsAdmin {
		return authscreens.RightsPage{}, auth.ErrAdminRoleFixed
	}
	return authscreens.RightsPage{Role: role, DocType: dt, DocTypes: docTypesByLabel(), Rights: matrix[dt.Name][id]}, nil
}

// getRights shows role {id}'s rights on {doctype}.
func (s *Server) getRights(w http.ResponseWriter, r *http.Request) {
	id, err := pathRole(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page, err := s.rightsPage(r.Context(), id, chi.URLParam(r, "doctype"))
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, http.StatusOK, authscreens.Rights(page))
}

func (s *Server) createRole(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	if err := parseForm(r); err != nil {
		s.WriteError(w, r, err)
		return
	}
	if err := s.a.CreateRole(r.Context(), p, r.PostFormValue("name")); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/roles")
}

func (s *Server) renameRole(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	id, version, err := roleForm(r)
	if err == nil {
		err = s.a.RenameRoleID(r.Context(), p, id, r.PostFormValue("name"), version)
	}
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/roles")
}

func (s *Server) deleteRole(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	id, version, err := roleForm(r)
	if err == nil {
		err = s.a.DeleteRoleID(r.Context(), p, id, version)
	}
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/roles")
}

// addRead adds read at the widest scope in rights, because saving any right
// grants it (CD-211), and reports whether it did. SetRightsID still refuses a
// row without read (AUTH-065); a row with no real scope stays as posted for it
// to refuse.
func addRead(rights map[auth.Right]auth.Scope) bool {
	if _, ok := rights[auth.RightRead]; ok {
		return false
	}
	saved := slices.Collect(maps.Values(rights))
	for _, sc := range []auth.Scope{auth.ScopeAll, auth.ScopeTeam, auth.ScopeOwn} {
		if slices.Contains(saved, sc) {
			rights[auth.RightRead] = sc
			return true
		}
	}
	return false
}

// setRights saves one role's row: each field is <right>=own|team|all, and an
// absent or empty field is none. A key that is not a right is refused by
// SetRightsID before any write. The next page says what happened.
func (s *Server) setRights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, version, err := roleForm(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	rights := map[auth.Right]auth.Scope{}
	for k, vs := range r.PostForm {
		if k != "csrf_token" && k != "version" && vs[0] != "" {
			rights[auth.Right(k)] = auth.Scope(vs[0])
		}
	}
	saved := maps.Clone(rights)
	readAdded := addRead(saved)
	dt := chi.URLParam(r, "doctype")
	err = s.a.SetRightsID(ctx, p, id, dt, saved, version)
	var problem *auth.Problem
	switch {
	case err == nil:
		if readAdded {
			web.Flash(ctx, "info", msgReadAdded)
		} else {
			web.Flash(ctx, "success", msgRightsSaved)
		}
		redirect(w, r, authscreens.RightsPath(id, dt))
	case errors.As(err, &problem) && problem.Code == http.StatusConflict:
		page, err := s.rightsPage(ctx, id, dt)
		if err != nil {
			s.WriteError(w, r, err)
			return
		}
		page.Role.Version, page.Rights, page.Error = version, rights, problem.Msg
		s.renderPage(w, r, problem.Code, authscreens.Rights(page))
	default:
		s.WriteError(w, r, err)
	}
}

// resetRights reseeds one DocType for every role with Config.Seed. It names
// no role, so it posts no version. The rights page asks first, as this can't
// be undone (design-system §4 rule 9); role_id says which page to return to.
func (s *Server) resetRights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := parseForm(r); err != nil {
		s.WriteError(w, r, err)
		return
	}
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	if !ok {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	if err := s.a.ResetRights(ctx, p, dt.Name, s.cfg.Seed); err != nil {
		s.WriteError(w, r, err)
		return
	}
	web.Flash(ctx, "success", dt.Label+" rights were reset to defaults for every role.")
	redirect(w, r, s.afterReset(ctx, r.PostFormValue("role_id"), dt.Name))
}

// afterReset is the rights page of the posted role, or /roles when that names
// no role whose rights page opens.
func (s *Server) afterReset(ctx context.Context, roleID, docType string) string {
	id, err := strconv.ParseInt(roleID, 10, 64)
	if err != nil {
		return "/roles"
	}
	role, _, err := s.loadRole(ctx, id)
	if err != nil || role.IsAdmin {
		return "/roles"
	}
	return authscreens.RightsPath(id, docType)
}
