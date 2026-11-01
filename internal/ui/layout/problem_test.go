package layout_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A full-page GET answered 403 or 404 through ui.Render shows the problem
// page in place of the plain heading (UI v2 §5.14, A-30); a 200 page, another
// error page and the htmx fragment never do.
func TestPage_ProblemOnlyOn403And404(t *testing.T) {
	msg := doctype.ErrNotFound.Error()
	do := func(status int, htmx bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/doc/fixture.Order/42", nil)
		req = req.WithContext(ui.WithShell(req.Context(), shell()))
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		o := layout.PageOpts{Title: msg, Error: true}
		rec := httptest.NewRecorder()
		ui.Render(rec, req, status, layout.Page(o), layout.Main(o))
		return rec
	}
	for _, tc := range []struct {
		status  int
		heading string
	}{
		{http.StatusNotFound, "We can't open this page"},
		{http.StatusForbidden, "You don't have access to this"},
	} {
		doc := uitest.Doc(t, do(tc.status, false))
		main := doc.Find("main#main")
		if main.Find("section.problem").Length() != 1 {
			t.Fatalf("%d: no problem page in main", tc.status)
		}
		if h := main.Find("h1"); h.Length() != 1 || strings.TrimSpace(h.Text()) != tc.heading || h.AttrOr("id", "") != "page-title" {
			t.Errorf("%d: h1s = %d %q, want only the problem heading %q", tc.status, h.Length(), h.Text(), tc.heading)
		}
		if got := main.Find(".problem-message").Text(); got != msg {
			t.Errorf("%d: message = %q, want %q", tc.status, got, msg)
		}
		if got := doc.Find("title").Text(); !strings.HasPrefix(got, "Error: "+msg) {
			t.Errorf("%d: title = %q, want the Error-prefixed kernel text", tc.status, got)
		}
	}
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"200 page":     do(http.StatusOK, false),
		"409 page":     do(http.StatusConflict, false),
		"404 fragment": do(http.StatusNotFound, true),
		"403 fragment": do(http.StatusForbidden, true),
	} {
		doc := uitest.Doc(t, rec)
		if n := doc.Find(".problem").Length(); n != 0 {
			t.Errorf("%s: %d problem pages, want none", name, n)
		}
		if h := doc.Find("main#main h1#page-title"); strings.TrimSpace(h.Text()) != msg {
			t.Errorf("%s: h1 = %q, want the plain title %q", name, h.Text(), msg)
		}
	}
}
