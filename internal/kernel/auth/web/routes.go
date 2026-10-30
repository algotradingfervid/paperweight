package authweb

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// PublicPaths may be Public or unguarded; a trailing * matches any suffix.
var PublicPaths = []string{"/signin", "/setup", "/static/*", "/healthz"}

// CheckedInHandlerPrefixes are the only paths a CheckedInHandler may guard
// (CD-46, CD-87).
var CheckedInHandlerPrefixes = []string{"/doc/"}

// Mount registers AUTH's routes on r.
func (s *Server) Mount(r chi.Router) {
	r.Method(http.MethodGet, "/signin", Public(http.HandlerFunc(s.getSignIn)))
	r.Method(http.MethodPost, "/signin", Public(http.HandlerFunc(s.postSignIn)))
	r.Method(http.MethodPost, "/signout", s.Self(http.HandlerFunc(s.postSignOut)))
	r.Method(http.MethodGet, "/setup", Public(http.HandlerFunc(s.getSetup)))
	r.Method(http.MethodPost, "/setup", Public(http.HandlerFunc(s.postSetup)))
	r.Method(http.MethodGet, "/account/password", s.Self(http.HandlerFunc(s.getPassword)))
	r.Method(http.MethodPost, "/account/password", s.Self(http.HandlerFunc(s.postPassword)))
	r.Method(http.MethodPost, "/users/{id}/password", s.Admin(auth.ErrNotAdminPassword.Msg, http.HandlerFunc(s.postResetPassword)))
	// Unlock and sessions (Task 16).
	r.Method(http.MethodPost, "/users/{id}/unlock", s.Admin(auth.ErrAdminOnlyField.Msg, http.HandlerFunc(s.postUnlock)))
	r.Method(http.MethodGet, "/account/sessions", s.Self(http.HandlerFunc(s.getSessions)))
	r.Method(http.MethodPost, "/account/sessions/{id}/revoke", s.Self(http.HandlerFunc(s.postRevokeSession)))
	r.Method(http.MethodPost, "/account/sessions/revoke-others", s.Self(http.HandlerFunc(s.postRevokeOthers)))
	// User administration (Task 13).
	r.Method(http.MethodGet, "/users", s.Admin(auth.ErrAdminOnlyUsers.Msg, http.HandlerFunc(s.listUsers)))
	r.Method(http.MethodPost, "/users", s.Admin(auth.ErrAdminOnlyUsers.Msg, http.HandlerFunc(s.createUser)))
	r.Method(http.MethodGet, "/users/{id}", s.Self(http.HandlerFunc(s.showUser)))
	r.Method(http.MethodPost, "/users/{id}", s.Self(http.HandlerFunc(s.updateUser)))
	r.Method(http.MethodPost, "/users/{id}/roles", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.updateRoles)))
	r.Method(http.MethodPost, "/users/{id}/restrictions", s.Admin(auth.ErrAdminOnlyAccess.Msg, http.HandlerFunc(s.changeRestrictions)))
	// Site and project access (Task 20).
	r.Method(http.MethodGet, "/users/{id}/access", s.Admin(auth.ErrAdminOnlyAccess.Msg, http.HandlerFunc(s.getAccess)))
	r.Method(http.MethodPost, "/users/{id}/delete", s.Admin(auth.ErrAdminOnlyUsers.Msg, http.HandlerFunc(s.deleteUser)))
	// Roles and the rights matrix (Task 17).
	r.Method(http.MethodGet, "/roles", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.listRoles)))
	r.Method(http.MethodPost, "/roles", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.createRole)))
	r.Method(http.MethodPost, "/roles/{id}/rename", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.renameRole)))
	r.Method(http.MethodPost, "/roles/{id}/delete", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.deleteRole)))
	r.Method(http.MethodPost, "/roles/{id}/rights/{doctype}", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.setRights)))
	r.Method(http.MethodPost, "/rights/{doctype}/reset", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.resetRights)))
	// Who can… (Task 19).
	r.Method(http.MethodGet, "/rights/who-can", s.Admin(auth.ErrAdminOnlyWhoCan.Msg, http.HandlerFunc(s.getWhoCan)))
	// Copy rights from another role (CD-200).
	r.Method(http.MethodGet, "/roles/{id}/copy-rights", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.getCopyRights)))
	r.Method(http.MethodPost, "/roles/{id}/copy-rights", s.Admin(auth.ErrAdminOnlyRoles.Msg, http.HandlerFunc(s.postCopyRights)))
}

// CheckRoutes returns one "METHOD path: problem" line per route that does not
// declare its permission properly.
func CheckRoutes(r chi.Routes) []string {
	var out []string
	_ = chi.Walk(r, func(method, route string, h http.Handler, _ ...func(http.Handler) http.Handler) error {
		g, ok := h.(*Guarded)
		var problem string
		switch {
		case !ok:
			if !isPublicPath(route) {
				problem = "no guard declared"
			}
		case g.Req.Kind == KindPublic && !isPublicPath(route):
			problem = "Public but not in PublicPaths"
		case g.Req.Kind == KindCheckedInHandler && !hasCheckedInPrefix(route):
			problem = "CheckedInHandler outside CheckedInHandlerPrefixes"
		}
		if problem != "" {
			out = append(out, method+" "+route+": "+problem)
		}
		return nil
	})
	return out
}

func isPublicPath(route string) bool {
	for _, p := range PublicPaths {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			if strings.HasPrefix(route, prefix) {
				return true
			}
		} else if route == p {
			return true
		}
	}
	return false
}

func hasCheckedInPrefix(route string) bool {
	for _, p := range CheckedInHandlerPrefixes {
		if strings.HasPrefix(route, p) {
			return true
		}
	}
	return false
}

// SafeNext returns next when it is a local path, else "/".
func SafeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, `/\`) {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return "/"
	}
	return next
}
