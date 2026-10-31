package modkitweb

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what modkit's screens need; internal/app fills them.
type Deps struct {
	DB    *db.DB
	Auth  auth.Service
	Audit audit.Writer
	Clock clock.Clock
}

// handlers carries Deps to each task's handler file (settings.go, and
// stubs_m014.go until M0-14 replaces it).
type handlers struct{ d Deps }

// Routes registers modkit's routes.
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Get("/settings/business", h.settingsGet)
		r.Post("/settings/business", h.settingsPost)
		r.Post("/modules/comments/{doctype}/{id}", h.addComment)
	}
}

// notBuilt answers a route whose task hasn't built it yet.
func notBuilt(w http.ResponseWriter) {
	http.Error(w, http.StatusText(http.StatusNotImplemented), http.StatusNotImplemented)
}
