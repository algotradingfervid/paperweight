// Package web serves the print pages and their stylesheets.
package web

import (
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Config is what the print handler needs. The signed-in user comes from
// auth.PrincipalFrom (CD-12) and the DocType label from Info.DocLabel (CD-05).
// Registry nil means print.Default.
type Config struct {
	DB         *db.DB
	Auth       auth.Service
	Info       doctype.InfoSource
	Letterhead print.Letterhead
	Registry   *print.Registry
}

type handlers struct{ cfg Config }

// Routes serves GET /print/{doctype}/{id}, GET /print/assets/print.css and
// GET /print/assets/brand.css (CD-99).
func Routes(cfg Config) chi.Router {
	if cfg.Registry == nil {
		cfg.Registry = print.Default
	}
	h := &handlers{cfg: cfg}
	r := chi.NewRouter()
	r.Get("/print/assets/print.css", css(print.CSS))
	r.Get("/print/assets/brand.css", css(brandCSS))
	r.Get("/print/{doctype}/{id}", h.print)
	return r
}
