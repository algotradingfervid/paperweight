package ui

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// IsHTMX reports whether r is an htmx request that wants a fragment: a
// history restore needs the full page.
func IsHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-History-Restore-Request") != "true"
}

// Render writes status and fragment for an htmx request (when fragment is not
// nil), else full. It always sets Vary: HX-Request so caches keep them apart.
func Render(w http.ResponseWriter, r *http.Request, status int, full, fragment templ.Component) {
	w.Header().Add("Vary", "HX-Request")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	c := full
	if fragment != nil && IsHTMX(r) {
		c = fragment
	}
	if err := c.Render(r.Context(), w); err != nil {
		slog.ErrorContext(r.Context(), "ui: render", "path", r.URL.Path, "err", err)
	}
}
