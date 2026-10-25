package ui

import (
	"bytes"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// IsHTMX reports whether r is an htmx request that wants a fragment: a
// history restore needs the full page.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
}

// Render writes status and fragment for an htmx request (when fragment is not
// nil), else full. It always sets Vary: HX-Request so caches keep them apart.
// It renders before writing the headers: the session is saved when they are
// written, so a flash the page takes (CD-80) must be cleared by then. A render
// error gives the plain 500 page and drops the partial HTML (CD-160).
func Render(w http.ResponseWriter, r *http.Request, status int, full, fragment templ.Component) {
	c := full
	if fragment != nil && IsHTMX(r) {
		c = fragment
	}
	w.Header().Add("Vary", "HX-Request")
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "ui: write", "path", r.URL.Path, "err", err)
	}
}
