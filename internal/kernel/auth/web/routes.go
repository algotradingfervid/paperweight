package authweb

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

// PublicPaths may be Public or unguarded; a trailing * matches any suffix.
var PublicPaths = []string{"/signin", "/setup", "/static/*", "/healthz"}

// CheckedInHandlerPrefixes are the only paths a CheckedInHandler may guard
// (CD-46, CD-87).
var CheckedInHandlerPrefixes = []string{"/doc/"}

// Mount registers AUTH's routes on r.
func (s *Server) Mount(chi.Router) {}

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
