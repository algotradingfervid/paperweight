// Package web serves attachment uploads, removals and downloads.
package web

import (
	"log/slog"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MultipartOverhead is how many bytes beyond the file itself an upload's
// multipart framing may take.
const MultipartOverhead int64 = 64 << 10

// Config is what the handlers need. The signed-in user comes from
// auth.PrincipalFrom (CD-12). Logger nil means slog.Default().
type Config struct {
	Store  *files.Store
	DB     *db.DB
	Logger *slog.Logger
}

type handlers struct{ cfg Config }

// Routes serves POST /attachments/{doctype}/{id} (multipart field "file") and
// POST /files/{id}/remove.
func Routes(cfg Config) chi.Router {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	h := &handlers{cfg: cfg}
	r := chi.NewRouter()
	r.Post("/attachments/{doctype}/{id}", h.upload)
	r.Post("/files/{id}/remove", h.remove)
	return r
}

// parseID reads a positive int64 from s.
func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil && id > 0
}
