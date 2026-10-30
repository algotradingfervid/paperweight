package authweb

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

// CD-160: a page that fails to render is a 500, with nothing of it sent.
func TestRenderPage_RenderErrorIs500(t *testing.T) {
	failing := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, _ = io.WriteString(w, "half a page")
		return errors.New("render failed")
	})
	rec := httptest.NewRecorder()
	(&Server{}).renderPage(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK, failing)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "half a page") {
		t.Errorf("body %q holds the failed page", body)
	}
}
