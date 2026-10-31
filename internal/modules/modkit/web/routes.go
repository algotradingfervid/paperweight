package modkitweb

import (
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what modkit's screens need; internal/app fills them.
type Deps struct {
	DB    *db.DB
	Auth  auth.Service
	Audit audit.Writer
	Clock clock.Clock
	Info  doctype.InfoSource // resolves a commented record for its read check
	// Sessions keep a comment refused without htmx for the document page
	// (TakeCommentDraft); the router runs inside its LoadAndSave.
	Sessions *scs.SessionManager
}

// handlers carries Deps to each task's handler file (settings.go,
// comments.go).
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
