package ui_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui"
)

// probe writes the problem status Render put in the context.
func probe(name string) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, name+"="+strconv.Itoa(ui.ProblemFrom(ctx)))
		return err
	})
}

// Render marks only a full-page GET answered 403 or 404 as a problem page
// (UI v2 §5.14, E6): a 200, another refusal, a POST or an htmx fragment is
// never one, and a history restore (which wants the full page) is.
func TestRender_ProblemOnlyForFullPageGET(t *testing.T) {
	for _, tc := range []struct {
		name, method string
		status       int
		hdr          map[string]string
		fragment     bool
		want         string
	}{
		{"404 page", http.MethodGet, http.StatusNotFound, nil, true, "full=404"},
		{"403 page", http.MethodGet, http.StatusForbidden, nil, true, "full=403"},
		{"200 page", http.MethodGet, http.StatusOK, nil, true, "full=0"},
		{"409 page", http.MethodGet, http.StatusConflict, nil, true, "full=0"},
		{"422 page", http.MethodGet, http.StatusUnprocessableEntity, nil, true, "full=0"},
		{"401 page", http.MethodGet, http.StatusUnauthorized, nil, true, "full=0"},
		{"410 page", http.MethodGet, http.StatusGone, nil, true, "full=0"},
		{"500 page", http.MethodGet, http.StatusInternalServerError, nil, true, "full=0"},
		{"404 POST", http.MethodPost, http.StatusNotFound, nil, true, "full=0"},
		{"404 htmx fragment", http.MethodGet, http.StatusNotFound, map[string]string{"HX-Request": "true"}, true, "fragment=0"},
		{"404 htmx, no fragment", http.MethodGet, http.StatusNotFound, map[string]string{"HX-Request": "true"}, false, "full=0"},
		{"404 history restore", http.MethodGet, http.StatusNotFound, map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"}, true, "full=404"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/x", nil)
			for k, v := range tc.hdr {
				req.Header.Set(k, v)
			}
			var frag templ.Component
			if tc.fragment {
				frag = probe("fragment")
			}
			rec := httptest.NewRecorder()
			ui.Render(rec, req, tc.status, probe("full"), frag)
			if got := rec.Body.String(); got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
			if rec.Code != tc.status {
				t.Errorf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
	if got := ui.ProblemFrom(context.Background()); got != 0 {
		t.Errorf("ProblemFrom(empty ctx) = %d, want 0", got)
	}
}
