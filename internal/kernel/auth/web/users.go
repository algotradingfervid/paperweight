package authweb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// flashWarnings shows warnings on the next page (CD-80).
func flashWarnings(r *http.Request, warnings []string) {
	for _, text := range warnings {
		web.Flash(r.Context(), "warning", text)
	}
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
	s.renderPage(w, r, http.StatusOK, s.v.Users(UsersPage{CSRF: CSRFToken(ctx), Users: users}))
}

// userPage builds user id's page: an Administrator's has the forms, and
// anyone else's is only their own. It refuses what UserForEdit refuses.
func (s *Server) userPage(ctx context.Context, p auth.Principal, id auth.UserID) (UserPage, error) {
	page := UserPage{CSRF: CSRFToken(ctx), Self: p.ID == id}
	if !slices.Contains(p.Roles, auth.RoleAdministrator) {
		u, version, _, err := s.a.UserForEdit(ctx, p, id)
		page.User, page.Version = u, version
		return page, err
	}
	v, err := s.a.UserAdmin(ctx, p, id)
	if err != nil {
		return page, err
	}
	page.User, page.Version, page.Roles, page.Held, page.Locked = v.User, v.Version, v.Roles, v.Held, v.Locked
	page.Admin, page.AccessURL = true, accessPath(id)
	return page, nil
}

// logReread logs a failed read of user id's name after a save that worked;
// the page then gets the result without the name.
func logReread(r *http.Request, id auth.UserID, err error) {
	slog.ErrorContext(r.Context(), "read the user after a save", "user", id, "path", r.URL.Path, "err", err)
}

// writeUserRefusal redraws user id's page with a write's refusal: the
// ConflictBanner on a stale version (CD-381), else the error summary. The forms
// keep the posted version (CD-94), and held, when set, stays ticked. A bad form,
// a refusal of the right (403) or an internal error keeps its plain answer.
func (s *Server) writeUserRefusal(w http.ResponseWriter, r *http.Request, id auth.UserID, version int64, held []string, err error) {
	status, msg, ok := userError(err)
	if !ok || errors.Is(err, errBadForm) || status == http.StatusForbidden {
		s.WriteError(w, r, err)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	page, loadErr := s.userPage(r.Context(), p, id)
	if loadErr != nil {
		s.WriteError(w, r, loadErr)
		return
	}
	page.Version, page.Error, page.Stale = version, msg, auth.IsChanged(err)
	if held != nil {
		page.Held = held
	}
	s.renderPage(w, r, status, s.v.User(page))
}

// getNewUser shows the add-a-user form with Employee ticked (D-AUTH-22).
func (s *Server) getNewUser(w http.ResponseWriter, r *http.Request) {
	s.renderNewUser(w, r, http.StatusOK, NewUserPage{Held: []string{auth.RoleEmployee}})
}

// renderNewUser shows page with every role on offer.
func (s *Server) renderNewUser(w http.ResponseWriter, r *http.Request, status int, page NewUserPage) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	roles, _, err := s.a.RightsMatrix(ctx, p)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	for _, role := range roles {
		page.Roles = append(page.Roles, role.Name)
	}
	page.CSRF = CSRFToken(ctx)
	s.renderPage(w, r, status, s.v.NewUser(page))
}

// passwordProblems are the password policy's refusals (auth.CheckPolicy) for
// a password an administrator sets.
var passwordProblems = []*auth.Problem{auth.ErrPasswordTooShort, auth.ErrPasswordTooLong, auth.ErrPasswordCommon, auth.ErrPasswordHasName}

func isPasswordProblem(err error) bool {
	return slices.ContainsFunc(passwordProblems, func(p *auth.Problem) bool { return errors.Is(err, p) })
}

// newUserField is the form field CreateUser's refusal err belongs to.
func newUserField(err error) string {
	switch {
	case errors.Is(err, auth.ErrNameRequired):
		return "name"
	case isPasswordProblem(err):
		return "password"
	}
	return "email"
}

func (s *Server) showUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	id, err := pathUser(r)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	page, err := s.userPage(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	s.renderPage(w, r, http.StatusOK, s.v.User(page))
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	in := auth.NewUser{
		Email: r.PostFormValue("email"), Name: r.PostFormValue("name"),
		Mobile: r.PostFormValue("mobile"), Password: r.PostFormValue("password"), MustChange: true,
	}
	// The form is an explicit admin choice: no ticked roles means none, not
	// the service's nil default.
	in.Roles = append([]string{}, r.PostForm["roles"]...)
	id, warnings, err := s.a.CreateUser(ctx, p, in)
	if err != nil {
		status, msg, ok := userError(err)
		if !ok || errors.Is(err, errBadForm) {
			s.WriteError(w, r, err)
			return
		}
		s.renderNewUser(w, r, status, NewUserPage{
			Error: msg, ErrorField: newUserField(err), Email: in.Email, Name: in.Name, Mobile: in.Mobile, Held: in.Roles,
		})
		return
	}
	web.Flash(ctx, "success", strings.TrimSpace(in.Name)+" was added.")
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
	patch := auth.UserPatch{Version: version}
	// The Disable and Enable forms post neither: an absent field stays as it is.
	if vs, ok := r.PostForm["name"]; ok {
		patch.Name = &vs[0]
	}
	if vs, ok := r.PostForm["mobile"]; ok {
		patch.Mobile = &vs[0]
	}
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
	warnings, err := s.a.UpdateUser(ctx, p, id, auth.UserPatch{Roles: &roles, Version: version})
	if err != nil {
		s.writeUserRefusal(w, r, id, *version, roles, err)
		return
	}
	if u, _, _, err := s.a.UserForEdit(ctx, p, id); err != nil {
		logReread(r, id, err)
		web.Flash(ctx, "success", "Roles saved.")
	} else {
		web.Flash(ctx, "success", "Roles for "+u.Name+" saved.")
	}
	flashWarnings(r, warnings)
	redirect(w, r, userPath(id))
}

// saveUser runs UpdateUser and redirects to the user's page, which says what
// a Disable or Enable did.
func (s *Server) saveUser(w http.ResponseWriter, r *http.Request, p auth.Principal, id auth.UserID, patch auth.UserPatch) {
	ctx := r.Context()
	warnings, err := s.a.UpdateUser(ctx, p, id, patch)
	if err != nil {
		s.writeUserRefusal(w, r, id, *patch.Version, nil, err)
		return
	}
	if patch.Enabled != nil {
		v, err := s.a.UserAdmin(ctx, p, id)
		switch {
		case err != nil && !*patch.Enabled:
			logReread(r, id, err)
			web.Flash(ctx, "success", "The user is disabled and signed out everywhere.")
		case err != nil:
			logReread(r, id, err)
			web.Flash(ctx, "success", "The user is enabled.")
		case !*patch.Enabled:
			web.Flash(ctx, "success", v.User.Name+" is disabled and signed out everywhere.")
		case v.Locked:
			web.Flash(ctx, "success", v.User.Name+" is enabled, but still locked after too many failed sign-ins. Unlock the account to let them sign in.")
		default:
			web.Flash(ctx, "success", v.User.Name+" can sign in again.")
		}
	}
	flashWarnings(r, warnings)
	redirect(w, r, userPath(id))
}

// changeRestrictions adds and removes one dimension's values and redirects
// to the access page. An Add form with nothing picked gets that page back
// with a field error (CD-194).
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
	if problem := emptyAddError(r, dim); problem != nil {
		s.writeEmptyAdd(w, r, id, dim, *version, problem)
		return
	}
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
	u, _, _, err := s.a.UserForEdit(ctx, p, id)
	if err != nil {
		s.WriteError(w, r, err)
		return
	}
	if err := s.a.DeleteUser(ctx, p, id, version); err != nil {
		s.writeUserRefusal(w, r, id, *version, nil, err)
		return
	}
	web.Flash(ctx, "success", u.Name+" was deleted.")
	redirect(w, r, "/users")
}
