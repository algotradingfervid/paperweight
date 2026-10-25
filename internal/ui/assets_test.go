package ui_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/cmd/buildassets/build"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // G304: test reads repo files
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestTokens_MatchResearch: tokens.css is research §9 verbatim, with the first
// `:root {` made the Tailwind theme. Tailwind rejects `color-scheme` inside
// `@theme`, so that one declaration stays on a plain `:root` rule above it.
func TestTokens_MatchResearch(t *testing.T) {
	md := readFile(t, "../../docs/research/frontend-tech.md")
	_, after, ok := strings.Cut(md, "\n## 9.")
	if !ok {
		t.Fatal("research has no section 9")
	}
	_, block, ok := strings.Cut(after, "```css\n")
	if !ok {
		t.Fatal("section 9 has no css block")
	}
	block, _, _ = strings.Cut(block, "```")
	want := strings.Replace(block, "\n:root {\n  color-scheme: light;\n", "\n:root { color-scheme: light; }\n\n@theme static {\n", 1)
	if want == block {
		t.Fatal("research block does not start with `:root {` and `color-scheme: light;`")
	}
	if got := readFile(t, "css/tokens.css"); got != want {
		t.Errorf("css/tokens.css differs from research §9 (with the first :root as @theme static)")
	}
}

var declRe = regexp.MustCompile(`(?:^|[\s{;])(--[a-z0-9-]+):\s*([^;]+);`)

func decls(css string) map[string]string {
	m := map[string]string{}
	for _, d := range declRe.FindAllStringSubmatch(css, -1) {
		m[d[1]] = strings.TrimSpace(d[2])
	}
	return m
}

// TestTheme_MatchesTokens: every daisyUI variable in theme.css equals the
// token named for it in docs/research/ui-library.md §3.
func TestTheme_MatchesTokens(t *testing.T) {
	tokens := decls(readFile(t, "css/tokens.css"))
	theme := decls(readFile(t, "css/theme.css"))
	// Value is a token name, or a literal when it does not start with "--".
	want := map[string]string{
		"--color-base-100":          "--color-surface",
		"--color-base-200":          "--color-canvas",
		"--color-base-300":          "--color-sunken",
		"--color-base-content":      "--color-text",
		"--color-primary":           "--color-primary-600",
		"--color-accent":            "--color-primary-600",
		"--color-secondary":         "--color-text-secondary",
		"--color-neutral":           "--color-text-secondary",
		"--color-info":              "--color-info-700",
		"--color-success":           "--color-success-700",
		"--color-warning":           "--color-warning-700",
		"--color-error":             "--color-danger-700",
		"--color-primary-content":   "--color-text-on-fill",
		"--color-secondary-content": "--color-text-on-fill",
		"--color-accent-content":    "--color-text-on-fill",
		"--color-neutral-content":   "--color-text-on-fill",
		"--color-info-content":      "--color-text-on-fill",
		"--color-success-content":   "--color-text-on-fill",
		"--color-warning-content":   "--color-text-on-fill",
		"--color-error-content":     "--color-text-on-fill",
		"--radius-selector":         "--radius-sm",
		"--radius-field":            "--radius-md",
		"--radius-box":              "--radius-lg",
		"--size-selector":           "0.25rem",
		"--size-field":              "0.275rem",
		"--border":                  "--border-w",
		"--depth":                   "0",
		"--noise":                   "0",
	}
	for name := range theme {
		if _, ok := want[name]; !ok {
			t.Errorf("theme.css sets %s, which has no token in ui-library §3", name)
		}
	}
	for name, src := range want {
		v := src
		if strings.HasPrefix(src, "--") {
			var ok bool
			if v, ok = tokens[src]; !ok {
				t.Errorf("tokens.css has no %s", src)
				continue
			}
		}
		if got, ok := theme[name]; !ok {
			t.Errorf("theme.css has no %s", name)
		} else if got != v {
			t.Errorf("theme.css %s = %q, want %q (%s)", name, got, v, src)
		}
	}
}

// staticDir is the package directory; //go:embed static serves the same files.
var staticDir = os.DirFS(".")

// buildAssets builds the assets once per test binary.
var built map[string][]byte

func buildAssets(t *testing.T) map[string][]byte {
	t.Helper()
	if built == nil {
		b, err := build.BuildAssets(".")
		if err != nil {
			t.Fatal(err)
		}
		built = b
	}
	return built
}

func file(t *testing.T, files map[string][]byte, url string) []byte {
	t.Helper()
	b, ok := files[strings.TrimPrefix(url, "/static/ui/")]
	if !ok {
		t.Fatalf("no file for %s", url)
	}
	return b
}

func TestCSS_Built(t *testing.T) {
	css := string(file(t, buildAssets(t), ui.CSSURL))
	if !strings.Contains(css, "[data-theme=paperweight]") {
		t.Error("built CSS has no [data-theme=paperweight] theme")
	}
	if strings.Contains(strings.ReplaceAll(css, " ", ""), "prefers-color-scheme:dark") {
		t.Error("built CSS has a prefers-color-scheme:dark rule")
	}
	if strings.Contains(css, "--color-red-500") {
		t.Error("built CSS still has Tailwind's default colours")
	}
	if !strings.HasSuffix(css, brand.CSS(brand.Load())) {
		t.Error("built CSS does not end with the brand block")
	}
	if strings.Contains(css, "__INTER__") || strings.Contains(css, "__NOTO__") {
		t.Error("font URL placeholders were not replaced")
	}
	if !strings.Contains(css, ui.InterURL) || !strings.Contains(css, ui.NotoURL) {
		t.Error("built CSS does not reference the hashed font URLs")
	}
}

func TestTailwind_ChecksumEnforced(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "internal", "ui")
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "tailwind-v4.3.3.sha256"), []byte(readFile(t, "vendor/tailwind-v4.3.3.sha256")), 0o600); err != nil {
		t.Fatal(err)
	}
	tool := filepath.Join(tmp, "bin", "tools", build.TailwindName())
	if err := os.MkdirAll(filepath.Dir(tool), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tool, []byte("not tailwind"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := build.EnsureTailwind(root)
	if err == nil {
		t.Fatal("EnsureTailwind accepted a file with the wrong sha256")
	}
	if !strings.Contains(err.Error(), tool) {
		t.Errorf("error %q does not name %s", err, tool)
	}

	sums := readFile(t, "vendor/daisyui/SHA256SUMS")
	for _, name := range []string{"daisyui.mjs", "daisyui-theme.mjs"} {
		sum := sha256.Sum256([]byte(readFile(t, "vendor/daisyui/"+name)))
		if line := hex.EncodeToString(sum[:]) + "  " + name; !strings.Contains(sums, line) {
			t.Errorf("vendor/daisyui/%s does not match SHA256SUMS", name)
		}
	}
	if _, err := os.Stat("vendor/daisyui/LICENSE"); err != nil {
		t.Error(err)
	}
}

func TestAssets_UpToDate(t *testing.T) {
	files := buildAssets(t)
	var names []string
	err := fs.WalkDir(staticDir, "static", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		names = append(names, strings.TrimPrefix(p, "static/"))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for n := range files {
		want = append(want, n)
	}
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("static/ has %v, build makes %v; run make generate", names, want)
	}
	for _, n := range names {
		got, _ := fs.ReadFile(staticDir, "static/"+n)
		if !bytes.Equal(got, files[n]) {
			t.Errorf("static/%s is out of date; run make generate", n)
		}
	}
	urls := []string{ui.CSSURL, ui.JSURL, ui.InterURL, ui.NotoURL}
	var fromConsts []string
	for _, u := range urls {
		fromConsts = append(fromConsts, strings.TrimPrefix(u, "/static/ui/"))
	}
	sort.Strings(fromConsts)
	if strings.Join(fromConsts, " ") != strings.Join(want, " ") {
		t.Errorf("assets_gen.go names %v, build makes %v; run make generate", fromConsts, want)
	}
	nameRe := regexp.MustCompile(`^/static/ui/[a-z-]+\.[0-9a-f]{8}\.(css|js|woff2)$`)
	for _, u := range urls {
		if !nameRe.MatchString(u) {
			t.Errorf("%s is not a hashed /static/ui/ name", u)
		}
	}
}

func staticGet(t *testing.T, url string) *httptest.ResponseRecorder {
	t.Helper()
	h := http.StripPrefix("/static/ui/", ui.Static())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestStatic_ImmutableAndTypes(t *testing.T) {
	rec := staticGet(t, "/static/ui/"+filepath.Base(ui.CSSURL))
	if rec.Code != http.StatusOK {
		t.Fatalf("css: status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/css; charset=utf-8" {
		t.Errorf("css: Content-Type %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("css: Cache-Control %q", got)
	}
	rec = staticGet(t, ui.InterURL)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "font/woff2" {
		t.Errorf("inter: status %d, Content-Type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec := staticGet(t, "/static/ui/nope.css"); rec.Code != http.StatusNotFound {
		t.Errorf("nope.css: status %d, want 404", rec.Code)
	}
}

func TestWP0Alpine_IsCSPBuild(t *testing.T) {
	js := readFile(t, "../platform/web/static/alpine.min.js")
	if strings.Contains(js, "new Function") || strings.Contains(js, "eval(") {
		t.Error("WP0's alpine.min.js is not the CSP build (it uses new Function or eval)")
	}
	re := regexp.MustCompile(`^/static/.+\.[0-9a-f]{8}\.js$`)
	for _, u := range []string{web.HTMXURL, web.AlpineURL} {
		if !re.MatchString(u) {
			t.Errorf("%q is not a content-hashed /static/ URL", u)
		}
	}
}

func TestAssets_JSBudget(t *testing.T) {
	b, err := fs.ReadFile(staticDir, "static/"+strings.TrimPrefix(ui.JSURL, "/static/ui/"))
	if err != nil {
		t.Fatal(err)
	}
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	if _, err := w.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if gz.Len() >= 3072 {
		t.Errorf("gzip(app.js) = %d bytes, budget < 3072", gz.Len())
	}
	for _, bad := range []string{"eval(", "new Function", "innerHTML", "insertAdjacentHTML"} {
		if bytes.Contains(b, []byte(bad)) {
			t.Errorf("app.js contains %q", bad)
		}
	}
}

func TestFonts_PresentAndSmall(t *testing.T) {
	for _, f := range []struct {
		name string
		max  int
	}{{"fonts/inter-var-sub.woff2", 50 << 10}, {"fonts/noto-sans-devanagari-sub.woff2", 130 << 10}} {
		b := readFile(t, f.name)
		if !strings.HasPrefix(b, "wOF2") {
			t.Errorf("%s is not woff2", f.name)
		}
		if len(b) >= f.max {
			t.Errorf("%s is %d bytes, want < %d", f.name, len(b), f.max)
		}
	}
	for _, name := range []string{"fonts/OFL-Inter.txt", "fonts/OFL-Noto.txt"} {
		if _, err := os.Stat(name); err != nil {
			t.Error(err)
		}
	}
}

func TestRender_VaryAndFragment(t *testing.T) {
	full := templ.Raw("<html>full</html>")
	frag := templ.Raw("<p>fragment</p>")
	for _, tc := range []struct {
		name string
		hdr  map[string]string
		want string
	}{
		{"plain GET", nil, "full"},
		{"htmx", map[string]string{"HX-Request": "true"}, "fragment"},
		{"history restore", map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"}, "full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tc.hdr {
				r.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			ui.Render(rec, r, http.StatusUnprocessableEntity, full, frag)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("status %d", rec.Code)
			}
			if got := rec.Header().Get("Vary"); got != "HX-Request" {
				t.Errorf("Vary %q", got)
			}
			if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
				t.Errorf("Content-Type %q", got)
			}
			body, _ := io.ReadAll(rec.Body)
			if !strings.Contains(string(body), tc.want) {
				t.Errorf("body %q, want the %s", body, tc.want)
			}
		})
	}
}

// The default app name comes from brand.json (CD-99), not a literal.
func TestShell_DefaultsWhenAbsent(t *testing.T) {
	got := ui.ShellFrom(context.Background())
	if got.AppName == "" || got.AppName != brand.Load().AppName {
		t.Errorf("AppName %q, want brand.Load().AppName %q", got.AppName, brand.Load().AppName)
	}
	ctx := ui.WithShell(context.Background(), ui.Shell{AppName: "X", CSRF: "tok"})
	if s := ui.ShellFrom(ctx); s.CSRF != "tok" || s.AppName != "X" {
		t.Errorf("ShellFrom after WithShell = %+v", s)
	}
}
