package authweb

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Role and matrix administration (plan Task 17). The rename, delete and
// rights forms post the role's version; the write finds the role by ID and
// refuses a stale version (CD-94, CD-146 wording).

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

// matrixPage is dt's matrix: the applicable rights, its scope options
// (CD-11) and every role's rights.
func matrixPage(csrf string, dt *doctype.DocType, roles []auth.RoleInfo, rights map[int64]map[auth.Right]auth.Scope) MatrixPage {
	m := MatrixPage{CSRF: csrf, DocType: dt.Name, Label: dt.Label, Scopes: auth.ScopeOptions(dt)}
	for _, rt := range auth.AllRights {
		if auth.Applies(dt, rt) {
			m.Rights = append(m.Rights, rt)
		}
	}
	for _, role := range roles {
		m.Roles = append(m.Roles, MatrixRow{Role: role, Rights: rights[role.ID]})
	}
	return m
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	roles, matrix, err := s.a.RightsMatrix(ctx, p)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	csrf := CSRFToken(ctx)
	page := RolesPage{CSRF: csrf, Warnings: takeWarnings(r), Roles: roles}
	for _, dt := range doctype.All() {
		page.Matrices = append(page.Matrices, matrixPage(csrf, dt, roles, matrix[dt.Name]))
	}
	s.render(w, r, s.v.Roles(page))
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

// setRights saves one role's row: each field is <right>=own|team|all, and an
// absent or empty field is none. A key that is not a right is refused by
// SetRightsID before any write.
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
	dt := chi.URLParam(r, "doctype")
	if err := s.a.SetRightsID(ctx, p, id, dt, rights, version); err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderMatrix(w, r, p, dt)
}

// resetRights reseeds one DocType for every role with Config.Seed. It names
// no role, so it posts no version.
func (s *Server) resetRights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := parseForm(r); err != nil {
		s.WriteError(w, r, err)
		return
	}
	dt := chi.URLParam(r, "doctype")
	if err := s.a.ResetRights(ctx, p, dt, s.cfg.Seed); err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderMatrix(w, r, p, dt)
}

// renderMatrix answers a saved matrix change with name's matrix page.
func (s *Server) renderMatrix(w http.ResponseWriter, r *http.Request, p auth.Principal, name string) {
	dt, ok := doctype.Lookup(name)
	if !ok {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	roles, matrix, err := s.a.RightsMatrix(r.Context(), p)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.render(w, r, s.v.Matrix(matrixPage(CSRFToken(r.Context()), dt, roles, matrix[name])))
}
