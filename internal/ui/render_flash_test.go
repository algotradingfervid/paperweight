package ui_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
)

// A page that takes the flash while rendering clears it for good: scs saves
// the session when the headers are written, so Render must render before it
// writes them (CD-80).
func TestRender_FlashShownOnce(t *testing.T) {
	r := web.NewRouter(web.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Sessions: scs.New()})
	r.Post("/act", func(w http.ResponseWriter, r *http.Request) {
		web.Flash(r.Context(), "success", "Approved.")
		http.Redirect(w, r, "/page", http.StatusSeeOther)
	})
	page := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		for _, n := range web.TakeFlash(ctx) {
			if _, err := io.WriteString(w, "<p>"+n.Text+"</p>"); err != nil {
				return err
			}
		}
		_, err := io.WriteString(w, "<p>page</p>")
		return err
	})
	r.Get("/page", func(w http.ResponseWriter, r *http.Request) {
		ui.Render(w, r, http.StatusOK, page, nil)
	})

	var cookies []*http.Cookie
	do := func(method string) string {
		req := httptest.NewRequest(method, map[string]string{http.MethodPost: "/act", http.MethodGet: "/page"}[method], nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if got := rec.Result().Cookies(); len(got) > 0 {
			cookies = got
		}
		return rec.Body.String()
	}

	do(http.MethodPost)
	if body := do(http.MethodGet); !strings.Contains(body, "Approved.") {
		t.Fatalf("first page lacks the flash: %q", body)
	}
	if body := do(http.MethodGet); strings.Contains(body, "Approved.") {
		t.Errorf("second page still shows the flash: %q", body)
	}
}
