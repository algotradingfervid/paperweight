package ui

import (
	"bytes"
	"embed"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:generate go run ./cmd/buildassets

//go:embed static
var staticFS embed.FS

// Static serves the embedded static/ files by their hashed names with the
// immutable cache header. WP3 mounts it with
// http.StripPrefix("/static/ui/", ui.Static()). Unknown names are 404.
func Static() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" || strings.Contains(name, "/") {
			http.NotFound(w, r)
			return
		}
		b, err := staticFS.ReadFile("static/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		ctype := mime.TypeByExtension(path.Ext(name))
		if path.Ext(name) == ".woff2" {
			ctype = "font/woff2"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(b))
	})
}
