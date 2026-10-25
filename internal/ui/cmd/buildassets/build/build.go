// Package build makes internal/ui's static assets: the CSS with the pinned
// Tailwind standalone executable and daisyUI (CD-98), the JS bundle with
// esbuild, and the fonts, each under a content-hashed name.
package build

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

// TailwindVersion is the pinned Tailwind CSS standalone release.
const TailwindVersion = "v4.3.3"

// URLPrefix is where WP3 mounts ui.Static().
const URLPrefix = "/static/ui/"

// releaseAsset is the release file name for this platform, as listed in
// vendor/tailwind-v4.3.3.sha256.
func releaseAsset() string {
	osName := map[string]string{"darwin": "macos", "linux": "linux", "windows": "windows"}[runtime.GOOS]
	arch := map[string]string{"arm64": "arm64", "amd64": "x64"}[runtime.GOARCH]
	name := "tailwindcss-" + osName + "-" + arch
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

// TailwindName is the executable's file name under bin/tools/.
func TailwindName() string {
	return strings.Replace(releaseAsset(), "tailwindcss-", "tailwindcss-"+TailwindVersion+"-", 1)
}

// EnsureTailwind returns the path of the Tailwind executable in the module's
// bin/tools/ (root is internal/ui). It downloads the release file when it is
// missing, and fails when the file's sha256 differs from the one in
// vendor/tailwind-v4.3.3.sha256.
func EnsureTailwind(root string) (string, error) {
	sums, err := os.ReadFile(filepath.Join(root, "vendor", "tailwind-"+TailwindVersion+".sha256")) //nolint:gosec // G304: fixed file under root
	if err != nil {
		return "", err
	}
	asset := releaseAsset()
	want := ""
	for line := range strings.SplitSeq(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "./") == asset {
			want = f[0]
		}
	}
	if want == "" {
		return "", fmt.Errorf("vendor/tailwind-%s.sha256 has no checksum for %s", TailwindVersion, asset)
	}
	path, err := filepath.Abs(filepath.Join(root, "..", "..", "bin", "tools", TailwindName()))
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(path) //nolint:gosec // G304: fixed tool path under the module
	if err == nil {
		if got := sha256Hex(b); got != want {
			return "", fmt.Errorf("%s has sha256 %s, want %s from vendor/tailwind-%s.sha256; delete it to download it again", path, got, want, TailwindVersion)
		}
		return path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	url := "https://github.com/tailwindlabs/tailwindcss/releases/download/" + TailwindVersion + "/" + asset
	if b, err = download(url); err != nil {
		return "", err
	}
	if got := sha256Hex(b); got != want {
		return "", fmt.Errorf("%s has sha256 %s, want %s from vendor/tailwind-%s.sha256", url, got, want, TailwindVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, b, 0o700); err != nil { //nolint:gosec // G306: the file is an executable
		return "", err
	}
	return path, nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// download fetches url and returns its bytes.
func download(url string) ([]byte, error) {
	resp, err := http.Get(url) //nolint:gosec // G107: URL built from the pinned version
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// hashed returns "<base>.<first 8 hex of sha256>.<ext>".
func hashed(base, ext string, b []byte) string {
	return base + "." + sha256Hex(b)[:8] + "." + ext
}

// BuildAssets builds every file of internal/ui/static (root is internal/ui)
// and returns them by hashed name.
func BuildAssets(root string) (map[string][]byte, error) {
	out := map[string][]byte{}
	fontURL := map[string]string{}
	for placeholder, base := range map[string]string{"__INTER__": "inter-var-sub", "__NOTO__": "noto-sans-devanagari-sub"} {
		b, err := os.ReadFile(filepath.Join(root, "fonts", base+".woff2")) //nolint:gosec // G304: fixed names
		if err != nil {
			return nil, err
		}
		name := hashed(base, "woff2", b)
		out[name] = b
		fontURL[placeholder] = URLPrefix + name
	}

	css, err := buildCSS(root)
	if err != nil {
		return nil, err
	}
	for placeholder, url := range fontURL {
		css = bytes.ReplaceAll(css, []byte(placeholder), []byte(url))
	}
	out[hashed("app", "css", css)] = css

	js, err := buildJS(root)
	if err != nil {
		return nil, err
	}
	out[hashed("app", "js", js)] = js
	return out, nil
}

// buildCSS runs Tailwind on css/app.css and appends the brand block last and
// unlayered, so brand values win (CD-99).
func buildCSS(root string) ([]byte, error) {
	tool, err := EnsureTailwind(root)
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp("", "buildassets-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	outFile := filepath.Join(tmp, "app.css")
	cmd := exec.Command(tool, "-i", filepath.Join("css", "app.css"), "-o", outFile, "--minify") //nolint:gosec // G204: checksum-verified tool
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tailwindcss: %w\n%s", err, stderr.String())
	}
	css, err := os.ReadFile(outFile) //nolint:gosec // G304: our temp file
	if err != nil {
		return nil, err
	}
	css = bytes.TrimRight(css, "\n")
	return append(css, brand.CSS(brand.Load())...), nil
}

// buildJS bundles js/app.js with esbuild.
func buildJS(root string) ([]byte, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	r := api.Build(api.BuildOptions{
		AbsWorkingDir:     abs,
		EntryPoints:       []string{"js/app.js"},
		Bundle:            true,
		MinifyWhitespace:  true,
		MinifyIdentifiers: true,
		MinifySyntax:      true,
		Engines: []api.Engine{
			{Name: api.EngineChrome, Version: "111"},
			{Name: api.EngineSafari, Version: "16.4"},
			{Name: api.EngineFirefox, Version: "115"},
		},
		Format: api.FormatIIFE,
		Write:  false,
	})
	if len(r.Errors) > 0 {
		var msgs []string
		for _, m := range r.Errors {
			msgs = append(msgs, m.Text)
		}
		return nil, fmt.Errorf("esbuild: %s", strings.Join(msgs, "; "))
	}
	if len(r.OutputFiles) != 1 {
		return nil, fmt.Errorf("esbuild: %d output files, want 1", len(r.OutputFiles))
	}
	return r.OutputFiles[0].Contents, nil
}
