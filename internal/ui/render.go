package ui

import (
	"bytes"
	"context"
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

type problemKey struct{}

// ProblemFrom is the status of the 403 or 404 page being rendered, or 0: only
// Render sets it, for a full-page GET answered with one of them, so the
// layout shows the problem page there and nowhere else (UI v2 §5.14).
func ProblemFrom(ctx context.Context) int {
	s, _ := ctx.Value(problemKey{}).(int)
	return s
}

// Render writes status and fragment for an htmx request (when fragment is not
// nil), else full. It always sets Vary: HX-Request so caches keep them apart.
// It renders before writing the headers: the session is saved when they are
// written, so a flash the page takes (CD-80) must be cleared by then. A render
// error gives the plain 500 page and drops the partial HTML (CD-160). A
// full-page GET answered 403 or 404 carries its status for ProblemFrom; htmx
// requests keep the CD-348 notice instead.
func Render(w http.ResponseWriter, r *http.Request, status int, full, fragment templ.Component) {
	c, ctx := full, r.Context()
	switch {
	case fragment != nil && IsHTMX(r):
		c = fragment
	case r.Method == http.MethodGet && !IsHTMX(r) && (status == http.StatusForbidden || status == http.StatusNotFound):
		ctx = context.WithValue(ctx, problemKey{}, status)
	}
	w.Header().Add("Vary", "HX-Request")
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if _, err := buf.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "ui: write", "path", r.URL.Path, "err", err)
	}
}
