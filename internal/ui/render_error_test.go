package ui_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui"
)

// A component that fails part-way gives the plain 500 page, never the HTML it
// wrote before failing (CD-160).
func TestRender_ErrorDropsPartialHTML(t *testing.T) {
	broken := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		if _, err := io.WriteString(w, "<p>partial</p>"); err != nil {
			return err
		}
		return errors.New("boom")
	})
	rec := httptest.NewRecorder()
	ui.Render(rec, httptest.NewRequest(http.MethodGet, "/x", nil), http.StatusOK, broken, nil)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "partial") || !strings.Contains(body, `role="alert"`) {
		t.Errorf("body = %q, want the error page without the partial HTML", body)
	}
	if got := rec.Header().Get("Vary"); got != "HX-Request" {
		t.Errorf("Vary = %q, want HX-Request", got)
	}
}
