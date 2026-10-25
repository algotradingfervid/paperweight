package brand_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 160 32"><text x="0" y="24">Test</text></svg>`

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testFS holds valid images under the default names.
func testFS(t *testing.T) fstest.MapFS {
	t.Helper()
	return fstest.MapFS{
		"logo.svg":       {Data: []byte(testSVG)},
		"logo-print.png": {Data: pngBytes(t, 300, 100)},
		"favicon.png":    {Data: pngBytes(t, 32, 32)},
	}
}

// configJSON returns the default keys with overrides applied; an empty override deletes the key.
func configJSON(t *testing.T, overrides map[string]string) []byte {
	t.Helper()
	m := map[string]string{
		"app_name":      "Paperweight",
		"primary":       "#1F4FD1",
		"app_logo":      "logo.svg",
		"document_logo": "logo-print.png",
		"favicon":       "favicon.png",
	}
	for k, v := range overrides {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func wantErr(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want an error containing %q, got nil", parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not contain %q", err, p)
		}
	}
}

func TestParse_Defaults(t *testing.T) {
	b, err := os.ReadFile("brand.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := brand.Parse(b, os.DirFS("assets"))
	if err != nil {
		t.Fatal(err)
	}
	css := brand.CSS(c)
	for _, want := range []string{"--color-primary-600:#1F4FD1", "--color-doc-accent:#1F4FD1"} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS() = %q, want it to contain %q", css, want)
		}
	}
	if got, want := brand.PrintCSS(c), ":root{--doc-accent:#1F4FD1}"; got != want {
		t.Errorf("PrintCSS() = %q, want %q", got, want)
	}
}

func TestParse_DerivesShades(t *testing.T) {
	c, err := brand.Parse(configJSON(t, map[string]string{"primary": "#0f6e56"}), testFS(t))
	if err != nil {
		t.Fatal(err)
	}
	if c.Primary != "#0F6E56" || c.DocumentAccent != "#0F6E56" {
		t.Fatalf("Primary = %s, DocumentAccent = %s, want #0F6E56 for both", c.Primary, c.DocumentAccent)
	}
	// Against white, a darker colour has a higher ratio and a lighter one a lower ratio.
	onWhite := func(hex string) float64 {
		r, err := brand.Contrast(hex, "#FFFFFF")
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	p := onWhite(c.Primary)
	if onWhite(c.Primary700) <= p || onWhite(c.Primary800) <= onWhite(c.Primary700) {
		t.Errorf("700 %s and 800 %s are not progressively darker than %s", c.Primary700, c.Primary800, c.Primary)
	}
	if onWhite(c.Primary50) >= p {
		t.Errorf("50 %s is not lighter than %s", c.Primary50, c.Primary)
	}
	pairs := []struct {
		fg, bg string
		min    float64
	}{
		{"#FFFFFF", c.Primary, 4.5},
		{"#FFFFFF", c.Primary700, 4.5},
		{c.Primary, "#F5F6F8", 4.5},
		{c.Primary, "#FFFFFF", 4.5},
		{c.Primary, "#EDEFF2", 4.5},
		{c.Primary800, c.Primary50, 4.5},
		{c.DocumentAccent, "#FFFFFF", 4.5},
	}
	for _, pr := range pairs {
		r, err := brand.Contrast(pr.fg, pr.bg)
		if err != nil {
			t.Fatal(err)
		}
		if r < pr.min {
			t.Errorf("%s on %s = %.2f, want at least %v", pr.fg, pr.bg, r, pr.min)
		}
	}
}

func TestParse_TooLightPrimary(t *testing.T) {
	_, err := brand.Parse(configJSON(t, map[string]string{"primary": "#8AB4F8"}), testFS(t))
	if err == nil {
		t.Fatal("want an error")
	}
	if !strings.HasPrefix(err.Error(), `brand.json: "primary" #8AB4F8 is too light:`) {
		t.Errorf("error %q has the wrong start", err)
	}
	wantErr(t, err, "needs at least 4.5:1")
}

func TestParse_RejectsLowContrastShades(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string]string
		key       string
	}{
		{"light primary_700", map[string]string{"primary_700": "#8AB4F8"}, `"primary_700" #8AB4F8`},
		{"primary_50 close to primary_800", map[string]string{"primary_800": "#16348A", "primary_50": "#5A78C8"}, `"primary_50"`},
		{"light document_accent", map[string]string{"document_accent": "#8AB4F8"}, `"document_accent" #8AB4F8`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := brand.Parse(configJSON(t, tc.overrides), testFS(t))
			wantErr(t, err, tc.key, "needs at least 4.5:1")
		})
	}
}

func TestParse_RejectsUnknownKeyAndMissingFile(t *testing.T) {
	_, err := brand.Parse([]byte(`{"app_name":"X","primary":"#1F4FD1","app_logo":"logo.svg","document_logo":"logo-print.png","favicon":"favicon.png","secondary":"#000000"}`), testFS(t))
	wantErr(t, err, `"secondary"`)

	_, err = brand.Parse(configJSON(t, map[string]string{"app_logo": "missing.svg"}), testFS(t))
	wantErr(t, err, "app_logo", "missing.svg")
}

func TestParse_RejectsWrongTypeAndLargeFile(t *testing.T) {
	files := testFS(t)
	files["logo.gif"] = &fstest.MapFile{Data: []byte("GIF89a")}
	_, err := brand.Parse(configJSON(t, map[string]string{"app_logo": "logo.gif"}), files)
	wantErr(t, err, "app_logo", "logo.gif")

	files["big.svg"] = &fstest.MapFile{Data: append([]byte(testSVG), bytes.Repeat([]byte(" "), 200*1024)...)}
	_, err = brand.Parse(configJSON(t, map[string]string{"app_logo": "big.svg"}), files)
	wantErr(t, err, "app_logo", "big.svg", "200 KB")
}

func TestParse_LogoSizes(t *testing.T) {
	// The generated sizes are the default files' intrinsic sizes.
	if brand.AppLogoWidth != 160 || brand.AppLogoHeight != 32 {
		t.Errorf("AppLogo = %d × %d, want 160 × 32", brand.AppLogoWidth, brand.AppLogoHeight)
	}
	if brand.DocumentLogoWidth != 600 || brand.DocumentLogoHeight != 200 {
		t.Errorf("DocumentLogo = %d × %d, want 600 × 200", brand.DocumentLogoWidth, brand.DocumentLogoHeight)
	}
	// The aspect limits 8:1 and 1:2 are allowed.
	files := testFS(t)
	files["wide.png"] = &fstest.MapFile{Data: pngBytes(t, 400, 50)}
	files["tall.png"] = &fstest.MapFile{Data: pngBytes(t, 50, 100)}
	for _, name := range []string{"wide.png", "tall.png"} {
		if _, err := brand.Parse(configJSON(t, map[string]string{"document_logo": name}), files); err != nil {
			t.Errorf("document_logo %s: %v", name, err)
		}
	}
	// An over-wide logo is rejected.
	files["wider.png"] = &fstest.MapFile{Data: pngBytes(t, 401, 50)}
	_, err := brand.Parse(configJSON(t, map[string]string{"document_logo": "wider.png"}), files)
	wantErr(t, err, "document_logo", "wider.png", "401 × 50")
}

func TestParse_RejectsTallLogo(t *testing.T) {
	files := testFS(t)
	files["tall.png"] = &fstest.MapFile{Data: pngBytes(t, 100, 201)}
	_, err := brand.Parse(configJSON(t, map[string]string{"app_logo": "tall.png"}), files)
	wantErr(t, err, "app_logo", "tall.png", "100 × 201")
}

func TestParse_RejectsSVGWithoutViewBox(t *testing.T) {
	files := testFS(t)
	files["logo.svg"] = &fstest.MapFile{Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="160" height="32"></svg>`)}
	_, err := brand.Parse(configJSON(t, nil), files)
	wantErr(t, err, "app_logo", "logo.svg", "viewBox")
	if strings.Contains(err.Error(), "cannot be decoded") {
		t.Errorf("error %q reports a missing viewBox as a decode failure", err)
	}

	// viewBox numbers may be separated by any whitespace or commas.
	files["logo.svg"] = &fstest.MapFile{Data: []byte("<svg xmlns=\"http://www.w3.org/2000/svg\" viewBox=\"0\t0,\n100\r\n 50\"></svg>")}
	if _, err := brand.Parse(configJSON(t, nil), files); err != nil {
		t.Errorf("viewBox with tab and newline separators: %v", err)
	}
}

func TestParse_FaviconMustBeSquare(t *testing.T) {
	files := testFS(t)
	files["wide.png"] = &fstest.MapFile{Data: pngBytes(t, 64, 32)}
	files["small.png"] = &fstest.MapFile{Data: pngBytes(t, 16, 16)}
	_, err := brand.Parse(configJSON(t, map[string]string{"favicon": "wide.png"}), files)
	wantErr(t, err, "favicon", "wide.png", "64 × 32")
	_, err = brand.Parse(configJSON(t, map[string]string{"favicon": "small.png"}), files)
	wantErr(t, err, "favicon", "small.png", "16 × 16")
}

func TestBrand_EmbeddedIsValid(t *testing.T) {
	c := brand.Load()
	if c.AppName != "Paperweight" {
		t.Errorf("AppName = %q", c.AppName)
	}
}

func TestBrand_GenUpToDate(t *testing.T) {
	got, err := brand.Generate(brand.Load(), os.DirFS("assets"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("brand_gen.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("brand_gen.go is out of date; run: go generate ./internal/platform/brand/...\ngot:\n%s", got)
	}
}

func TestStatic_ServesLogo(t *testing.T) {
	re := regexp.MustCompile(`^/static/brand/[a-z0-9-]+\.[0-9a-f]{8}\.(svg|png|jpg|jpeg)$`)
	for _, u := range []string{brand.AppLogoURL, brand.DocumentLogoURL, brand.FaviconURL} {
		if !re.MatchString(u) {
			t.Errorf("URL %q does not match %s", u, re)
		}
	}
	h := brand.Static()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+path.Base(brand.AppLogoURL), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q", got)
	}
	want, err := os.ReadFile("assets/logo.svg")
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(rec.Body); !bytes.Equal(body, want) {
		t.Error("body is not assets/logo.svg")
	}

	// The handler works with or without the mount prefix stripped.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, brand.AppLogoURL, nil))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD %s = %d, want 200", brand.AppLogoURL, rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+path.Base(brand.AppLogoURL), nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d, want 405", rec.Code)
	}

	for _, p := range []string{"/logo.svg", "/logo.00000000.svg", "/x/" + path.Base(brand.AppLogoURL), brand.AppLogoURL + "/x"} {
		rec = httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", p, rec.Code)
		}
	}
}
