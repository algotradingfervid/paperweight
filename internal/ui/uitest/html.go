// Package uitest holds the HTML test helpers: render a component or call a
// handler, then query the result with goquery.
package uitest

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
)

// Render renders c with a background context and parses the HTML.
func Render(t testing.TB, c templ.Component) *goquery.Document {
	t.Helper()
	return RenderCtx(t, context.Background(), c)
}

// RenderCtx renders c with ctx and parses the HTML.
func RenderCtx(t testing.TB, ctx context.Context, c templ.Component) *goquery.Document {
	t.Helper()
	var buf bytes.Buffer
	if err := c.Render(ctx, &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	doc, err := goquery.NewDocumentFromReader(&buf)
	if err != nil {
		t.Fatalf("parse HTML: %v", err)
	}
	return doc
}

// Do sends a request to h and returns the recorded response. A non-nil form
// is sent form-encoded; hdr holds header name and value pairs.
func Do(t testing.TB, h http.Handler, method, target string, form url.Values, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	if len(hdr)%2 != 0 {
		t.Fatalf("uitest.Do: odd number of header arguments %q", hdr)
	}
	var r *http.Request
	if form != nil {
		r = httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		r = httptest.NewRequest(method, target, nil)
	}
	for i := 0; i < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

// Doc parses the body of rec.
func Doc(t testing.TB, rec *httptest.ResponseRecorder) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("parse HTML: %v", err)
	}
	return doc
}
