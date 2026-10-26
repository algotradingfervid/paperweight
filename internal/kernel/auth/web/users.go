package authweb

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// render writes c, answering a render error through WriteError.
func (s *Server) render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	if err := c.Render(r.Context(), w); err != nil {
		s.WriteError(w, r, err)
	}
}

// flashWarnings shows warnings on the next page (CD-80).
func flashWarnings(r *http.Request, warnings []string) {
	for _, text := range warnings {
		web.Flash(r.Context(), "warning", text)
	}
}

// takeWarnings returns the texts of the session's flash messages.
func takeWarnings(r *http.Request) []string {
	var out []string
	for _, n := range web.TakeFlash(r.Context()) {
		out = append(out, n.Text)
	}
	return out
}

// pathUser is the {id} of the URL; ErrNotFound when it is not a number.
func pathUser(r *http.Request) (auth.UserID, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		return 0, auth.ErrNotFound
	}
	return auth.UserID(id), nil
}

func userPath(id auth.UserID) string { return fmt.Sprintf("/users/%d", id) }

// formIDs parses the form field k as a list of IDs; nil when it is absent.
func formIDs(r *http.Request, k string) (*[]int64, error) {
	vs, ok := r.PostForm[k]
	if !ok {
		return nil, nil
	}
	ids := make([]int64, 0, len(vs))
	for _, v := range vs {
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errBadForm
		}
		ids = append(ids, id)
	}
	return &ids, nil
}

// formVersion parses the urlencoded form and returns the page's posted
// version (CD-94). Every write to an existing user requires it: another body
// type, or a missing or non-numeric version, is errBadForm.
func formVersion(r *http.Request) (*int64, error) {
	if !isURLEncoded(r) || r.ParseForm() != nil {
		return nil, errBadForm
	}
	vs := r.PostForm["version"]
	if len(vs) == 0 {
		return nil, errBadForm
	}
	v, err := strconv.ParseInt(vs[0], 10, 64)
	if err != nil {
		return nil, errBadForm
	}
	return &v, nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	users, err := s.a.ListUsers(ctx, p)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.render(w, r, s.v.Users(UsersPage{CSRF: CSRFToken(ctx), Warnings: takeWarnings(r), Users: users}))
}

func (s *Server) showUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	u, version, _, err := s.a.UserForEdit(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page := UserPage{CSRF: CSRFToken(ctx), Warnings: takeWarnings(r), Version: version, User: u}
	if slices.Contains(p.Roles, auth.RoleAdministrator) {
		page.AccessURL = accessPath(id)
	}
	s.render(w, r, s.v.User(page))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	in := auth.NewUser{
		Email: r.PostFormValue("email"), Name: r.PostFormValue("name"),
		Mobile: r.PostFormValue("mobile"), Password: r.PostFormValue("password"),
	}
	// The form is an explicit admin choice: no ticked roles means none, not
	// the service's nil default.
	in.Roles = append([]string{}, r.PostForm["roles"]...)
	id, warnings, err := s.a.CreateUser(ctx, p, in)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	flashWarnings(r, warnings)
	redirect(w, r, userPath(id))
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	name, mobile := r.PostFormValue("name"), r.PostFormValue("mobile")
	patch := auth.UserPatch{Name: &name, Mobile: &mobile, Version: version}
	if vs, ok := r.PostForm["email"]; ok {
		patch.Email = &vs[0]
	}
	if vs, ok := r.PostForm["enabled"]; ok {
		var enabled bool
		switch vs[0] {
		case "true":
			enabled = true
		case "false":
		default:
			s.WriteError(w, r, errBadForm)
			return
		}
		patch.Enabled = &enabled
	}
	if roles, ok := r.PostForm["roles"]; ok {
		patch.Roles = &roles
	}
	if patch.Sites, err = formIDs(r, "sites"); err == nil {
		patch.Projects, err = formIDs(r, "projects")
	}
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.saveUser(w, r, p, id, patch)
}

func (s *Server) updateRoles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	roles := r.PostForm["roles"]
	if roles == nil {
		roles = []string{}
	}
	s.saveUser(w, r, p, id, auth.UserPatch{Roles: &roles, Version: version})
}

// saveUser runs UpdateUser and redirects to the user's page.
func (s *Server) saveUser(w http.ResponseWriter, r *http.Request, p auth.Principal, id auth.UserID, patch auth.UserPatch) {
	warnings, err := s.a.UpdateUser(r.Context(), p, id, patch)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	flashWarnings(r, warnings)
	redirect(w, r, userPath(id))
}

func (s *Server) changeRestrictions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	dim := auth.Dimension(r.PostFormValue("dimension"))
	add, err := formIDs(r, "add")
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	remove, err := formIDs(r, "remove")
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	var addIDs, removeIDs []int64
	if add != nil {
		addIDs = *add
	}
	if remove != nil {
		removeIDs = *remove
	}
	if err := s.a.ChangeRestrictions(ctx, p, id, dim, addIDs, removeIDs, version); err != nil {
		s.WriteError(w, r, err)
		return
	}
	u, _, _, err := s.a.UserForEdit(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	web.Flash(ctx, "success", "Site and project access for "+u.Name+" saved.")
	redirect(w, r, accessPath(id))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	if err := s.a.DeleteUser(ctx, p, id, version); err != nil {
		s.WriteError(w, r, err)
		return
	}
	redirect(w, r, "/users")
}
