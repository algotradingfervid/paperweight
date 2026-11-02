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

	"github.com/vardhan/internaltools/internal/ui/icons"
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

// CheckTile is one summary card a check page should show: its title and the
// icon name in its tile.
type CheckTile struct{ Title, Icon string }

// WantCheckTiles fails unless the cards with a Change button in doc are want,
// in order, each with exactly its icon.
func WantCheckTiles(t testing.TB, doc *goquery.Document, want ...CheckTile) {
	t.Helper()
	cards := doc.Find("section.card:has(.card-head > a.btn-ghost)")
	if cards.Length() != len(want) {
		t.Fatalf("%d cards with a Change button, want %d", cards.Length(), len(want))
	}
	for i, w := range want {
		card := cards.Eq(i)
		if got := strings.Join(strings.Fields(card.Find(".card-title").First().Text()), " "); got != w.Title {
			t.Errorf("card %d title = %q, want %q", i, got, w.Title)
		}
		tile, _ := card.Find(".card-head .card-icon svg").Html()
		if icon, _ := Render(t, icons.Icon(w.Icon)).Find("svg").Html(); tile != icon {
			t.Errorf("%s tile = %q, want the %s icon", w.Title, tile, w.Icon)
		}
	}
}
