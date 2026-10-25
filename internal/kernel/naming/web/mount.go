// Package namingweb is the Settings → Numbering screen (CD-49): every
// series with its next number (NAM-051), the series and counter forms, and
// the template preview (NAM-062). It is built from internal/ui components.
package namingweb

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/clock"
)

// msgForbidden is naming's refusal text (CD-66), which the guard shows.
const msgForbidden = "You don't have permission to change document numbering. Ask an Administrator."

const basePath = "/settings/numbering"

// Deps are what the screen needs.
type Deps struct {
	Svc   naming.Service
	Admin naming.Admin
	Clock clock.Clock
}

// Guard wraps h so only Administrators reach it, refusing others with msg.
// WP3 passes AUTH's Srv.Admin.
type Guard func(msg string, h http.Handler) http.Handler

// Mount registers the screen's routes on r, each behind admin.
func Mount(r chi.Router, d Deps, admin Guard) {
	h := &handlers{d: d}
	route := func(method, path string, fn http.HandlerFunc) {
		r.Method(method, path, admin(msgForbidden, fn))
	}
	route(http.MethodGet, basePath, h.list)
	route(http.MethodGet, basePath+"/preview", h.preview)
	route(http.MethodGet, basePath+"/{doctype}", h.edit)
	route(http.MethodPost, basePath+"/{doctype}/template", h.saveTemplate)
	route(http.MethodPost, basePath+"/{doctype}/counter", h.saveCounter)
}
