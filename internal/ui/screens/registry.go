// Package screens is the registry of every screen the browser sweep checks,
// with the widths and the page scripts it uses. It does not import chromedp.
package screens

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Screen is one page state the sweep visits.
type Screen struct {
	Name  string
	Route string // chi pattern it covers, e.g. "/doc/{doctype}/{id}"
	URL   string
	As    string                          // fixture email, "" = signed out
	Setup func(ctx context.Context) error // optional; ctx is a chromedp context
}

// Widths are the viewport widths every screen is checked at.
var Widths = []int64{320, 360, 390, 768, 1024, 1440}

// MobileBelow is the first width that is not emulated as a phone.
const MobileBelow = 768

// DefaultSkip holds the route prefixes the sweep does not cover.
var DefaultSkip = []string{"/static", "/print", "/api", "/healthz"}

var registry []Screen

// Register adds screens. A Name that is already registered panics.
func Register(s ...Screen) {
	for _, n := range s {
		if slices.ContainsFunc(registry, func(o Screen) bool { return o.Name == n.Name }) {
			panic("screens: duplicate screen name " + n.Name)
		}
		registry = append(registry, n)
	}
}

// All returns the registered screens sorted by Name.
func All() []Screen {
	out := slices.Clone(registry)
	slices.SortFunc(out, func(a, b Screen) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// Uncovered returns every GET pattern of r that starts with no prefix in skip
// and that no registered Screen.Route equals.
func Uncovered(r chi.Routes, skip []string) []string {
	var out []string
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method != http.MethodGet {
			return nil
		}
		if slices.ContainsFunc(skip, func(p string) bool { return strings.HasPrefix(route, p) }) {
			return nil
		}
		if !slices.ContainsFunc(registry, func(s Screen) bool { return s.Route == route }) {
			out = append(out, route)
		}
		return nil
	})
	return out
}
