package webtest_test

import (
	"image"
	_ "image/jpeg"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print/web/webtest"
	"github.com/vardhan/internaltools/internal/testkit"
)

func fetch(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("GET %s: read body: %v", url, err)
	}
	return res, string(b)
}

// Supporting test, no case ID: the e2e server serves the real print and
// files handlers over the seeded documents.
func TestServer_ServesReferencePO(t *testing.T) {
	srv := webtest.NewServer(t, testkit.Principal(t, "ravi@vardhan.test"))

	res, body := fetch(t, srv.URL+"/print/test.PurchaseOrder/1")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /print/test.PurchaseOrder/1: got %d %q, want 200", res.StatusCode, body)
	}
	if got, want := res.Header.Get("Content-Security-Policy"), "default-src 'self'; img-src 'self' data:"; got != want {
		t.Errorf("GET /print/test.PurchaseOrder/1: Content-Security-Policy %q, want print's %q", got, want)
	}
	for _, want := range []string{"PO/26-27/00001", `href="/print/assets/print.css"`} {
		if !strings.Contains(body, want) {
			t.Errorf("reference PO print lacks %q:\n%s", want, body)
		}
	}

	res, body = fetch(t, srv.URL+"/files/1")
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" {
		t.Errorf("GET /files/1: got %d %q, want 200 image/png", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if !strings.HasPrefix(body, "\x89PNG") {
		t.Errorf("GET /files/1: body is not a PNG")
	}

	res, body = fetch(t, srv.URL+"/print/test.PurchaseOrder/42")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /print/test.PurchaseOrder/42: got %d %q, want 200", res.StatusCode, body)
	}
	if !strings.Contains(body, "Draft 42") {
		t.Errorf("draft print lacks Draft 42:\n%s", body)
	}
	// One <td class="sr"> per line row: the lines template's rows carry no class.
	if n := strings.Count(body, `<td class="sr">`); n != 60 {
		t.Errorf("draft print has %d line rows, want 60", n)
	}
	if n := strings.Count(body, `<div class="desc">`); n != 20 {
		t.Errorf("draft print has %d two-line descriptions, want 20 (every third line)", n)
	}

	for _, path := range []string{"/files/2", "/files/3", "/files/4"} {
		if res, _ := fetch(t, srv.URL+path); res.StatusCode != http.StatusOK {
			t.Errorf("GET %s (expense attachment): got %d, want 200", path, res.StatusCode)
		}
	}

	// /files/4 is the 64×32 JPEG with an EXIF orientation-6 segment.
	_, body = fetch(t, srv.URL+"/files/4")
	cfg, _, err := image.DecodeConfig(strings.NewReader(body))
	if err != nil || cfg.Width != 64 || cfg.Height != 32 {
		t.Errorf("GET /files/4: DecodeConfig = %dx%d, %v; want 64x32", cfg.Width, cfg.Height, err)
	}
	// Little-endian IFD entry: tag 0x0112 Orientation, SHORT, count 1, value 6.
	for _, want := range []string{"Exif\x00\x00", "\x12\x01\x03\x00\x01\x00\x00\x00\x06\x00"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /files/4: body lacks %q", want)
		}
	}

	if res, body := fetch(t, srv.URL+"/unrelated"); res.StatusCode != http.StatusNotFound || strings.Contains(body, "File not found.") {
		t.Errorf("GET /unrelated: got %d %q, want the app router's 404, not files/web's", res.StatusCode, body)
	}
}
