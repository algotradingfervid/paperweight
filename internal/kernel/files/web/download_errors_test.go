package web_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// Review fix: ServeContent's own error replies (416 here) keep the download's
// security headers; its serveError drops Cache-Control.
func TestServe_UnsatisfiableRangeKeepsHeaders(t *testing.T) {
	f := newFixture(t, nil)
	m := attach(t, f, f.deepak, f.exp, "bill.pdf", filestest.PDF(4096))

	req := getReq(http.MethodGet, fileURL(m.ID), &f.deepak)
	req.Header.Set("Range", "bytes=99999999-")
	rec := serve(f, req)

	if rec.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("Range bytes=99999999-: status = %d, want 416 (body %q)", rec.Code, rec.Body.String())
	}
	wantSecurityHeaders(t, "Range bytes=99999999-", rec, "text/plain; charset=utf-8")
}
