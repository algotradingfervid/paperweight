package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

type conflictErr struct{}

func (conflictErr) Error() string                     { return "Someone else changed this." }
func (conflictErr) Status() int                       { return http.StatusConflict }
func (conflictErr) FieldErrors() []doctype.FieldError { return nil }

// CD-241: a doctype.HTTPError reaching fail is an htmx notice with its status.
func TestFail_HTTPErrorNotice(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/files/1/remove", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	(&handlers{}).fail(rec, req, conflictErr{})
	if rec.Code != http.StatusConflict || rec.Body.Len() != 0 || rec.Header().Get("HX-Reswap") != "none" {
		t.Errorf("status %d body %q HX-Reswap %q, want 409, empty, none", rec.Code, rec.Body.String(), rec.Header().Get("HX-Reswap"))
	}
	const want = `{"notice":{"level":"error","text":"Someone else changed this."}}`
	if got := rec.Header().Get("HX-Trigger"); got != want {
		t.Errorf("HX-Trigger = %q, want %q", got, want)
	}
}
