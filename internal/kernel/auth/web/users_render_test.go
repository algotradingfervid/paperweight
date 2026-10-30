package authweb_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	authweb "github.com/vardhan/internaltools/internal/kernel/auth/web"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/views"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

// failingUserViews are the default views with user pages that fail part-way.
type failingUserViews struct{ views.Default }

func halfAPage() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "half a page")
		return errors.New("render failed")
	})
}

func (failingUserViews) Users(authweb.UsersPage) templ.Component { return halfAPage() }
func (failingUserViews) User(authweb.UserPage) templ.Component   { return halfAPage() }

// CD-160: a user page that fails to render is a 500, with nothing of it sent.
func TestUsers_RenderErrorIs500(t *testing.T) {
	w := authtest.NewWorld(t)
	var bare http.Handler
	h := webtest.New(t, w, webtest.WithRoutes(func(r chi.Router, _ *authweb.Server) {
		r.Get("/bare/*", func(rw http.ResponseWriter, req *http.Request) {
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, nil))
			req.URL.Path = strings.TrimPrefix(req.URL.Path, "/bare")
			bare.ServeHTTP(rw, req)
		})
	}))
	srv := authweb.New(w.Auth, auth.NewSessionStore(w.DB, w.Clock, auth.DefaultSessionConfig(false)), h.SM,
		failingUserViews{}, authweb.Config{})
	mux := chi.NewRouter()
	srv.Mount(mux)
	bare = mux

	admin := signedIn(t, h, t13Admin)
	for _, path := range []string{"/users", t13UserPath(w, t13Suresh, "")} {
		r := admin.Get("/bare" + path)
		wantStatus(t, "GET "+path, r, http.StatusInternalServerError)
		if strings.Contains(r.Body, "half a page") {
			t.Errorf("GET %s: body %q holds the failed page", path, r.Body)
		}
	}
}
