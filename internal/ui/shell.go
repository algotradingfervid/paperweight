package ui

import (
	"context"
	"net/http"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

// Shell is what the app shell shows around every page.
type Shell struct {
	AppName, User, CSRF, Path string
	Nav                       []NavItem
	InboxCount                int
	SignedIn                  bool
}

// NavItem is one navigation link.
type NavItem struct{ Label, Href, Icon, Group string }

type shellKey struct{}

// WithShell returns ctx carrying s.
func WithShell(ctx context.Context, s Shell) context.Context {
	return context.WithValue(ctx, shellKey{}, s)
}

// ShellFrom returns the Shell in ctx, or a zero Shell named after the brand
// (CD-99) when there is none.
func ShellFrom(ctx context.Context) Shell {
	if s, ok := ctx.Value(shellKey{}).(Shell); ok {
		return s
	}
	return Shell{AppName: brand.Load().AppName}
}

// ShellMiddleware puts src(r) in every request's context.
func ShellMiddleware(src func(*http.Request) Shell) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(WithShell(r.Context(), src(r))))
		})
	}
}
