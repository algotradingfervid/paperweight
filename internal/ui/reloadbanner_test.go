package ui_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// reloadBannerAllowed are the files that may still pass a "Reload" link to
// components.Banner, with how many such calls each holds. A count that no
// longer matches fails the gate, so a stale entry can't hide a new call.
var reloadBannerAllowed = map[string]int{
	"internal/ui/docscreen/lines.templ": 1, // the line grid's non-409 refusal (CD-365: stays for now)
	"internal/ui/gallery/pages.templ":   1, // the gallery's danger banner with a link
}

// reloadBanners is the line of each components.Banner call in src whose
// arguments hold `Label: "Reload"`.
func reloadBanners(src string) []int {
	const call = "components.Banner("
	var lines []int
	for i := 0; ; {
		j := strings.Index(src[i:], call)
		if j < 0 {
			return lines
		}
		start := i + j + len(call)
		end, depth := start, 1
		for end < len(src) && depth > 0 {
			switch src[end] {
			case '(':
				depth++
			case ')':
				depth--
			}
			end++
		}
		if strings.Contains(src[start:end], `Label: "Reload"`) {
			lines = append(lines, strings.Count(src[:i+j], "\n")+1)
		}
		i = start
	}
}

// reloadBannerTree is every reloadBanners finding in the .go and .templ
// sources under root, outside internal/ui/components, generated files and
// tests, as "path:line" per call; an allowed file's calls are reported only
// when their count differs from reloadBannerAllowed.
func reloadBannerTree(root string) []string {
	var out []string
	seen := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch {
			case rel == "internal/ui/components":
				return filepath.SkipDir
			case rel != "." && slices.Contains([]string{"testdata", "node_modules", ".git", ".worktrees", "vendor"}, d.Name()):
				return filepath.SkipDir
			}
			return nil
		}
		source := strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, ".templ")
		if !source || strings.HasSuffix(rel, "_templ.go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // G304: test reads repo files
		if err != nil {
			return err
		}
		lines := reloadBanners(string(b))
		if _, ok := reloadBannerAllowed[rel]; ok {
			seen[rel] = len(lines)
			return nil
		}
		for _, n := range lines {
			out = append(out, rel+":"+strconv.Itoa(n))
		}
		return nil
	})
	if err != nil {
		out = append(out, err.Error())
	}
	for rel, want := range reloadBannerAllowed {
		if got := seen[rel]; got != want {
			out = append(out, rel+": "+strconv.Itoa(got)+" allowed calls, want "+strconv.Itoa(want))
		}
	}
	return out
}

// A 409 banner is components.ConflictBanner (CD-365): a danger Banner with a
// "Reload" link outside the components package fails the gate.
func TestReloadBanner_Repo(t *testing.T) {
	for _, f := range reloadBannerTree("../..") {
		t.Errorf("components.Banner with Label: \"Reload\" (use components.ConflictBanner, CD-365): %s", f)
	}
}

// The gate finds the call across lines, in .go and .templ, and skips the
// components package, generated files and tests; allowed counts must match.
func TestReloadBanner_Finds(t *testing.T) {
	const bad = "x\ncomponents.Banner(components.BannerProps{\n\tKind: components.Danger, Link: &components.Link{Label: \"Reload\", Href: f(a)},\n})\n"
	const good = "components.Banner(components.BannerProps{Kind: components.Danger})\nLink{Label: \"Reload\"}\n"
	root := t.TempDir()
	files := map[string]string{
		"a/bad.go":                          bad,
		"a/bad.templ":                       "@" + good + bad,
		"a/good.go":                         good,
		"a/bad_templ.go":                    bad,
		"a/bad_test.go":                     bad,
		"internal/ui/components/c.templ":    bad,
		"internal/ui/docscreen/lines.templ": bad,
		"internal/ui/gallery/pages.templ":   bad + bad,
		"a/testdata/bad.go":                 bad,
	}
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got := reloadBannerTree(root)
	want := []string{"a/bad.go:2", "a/bad.templ:4", "internal/ui/gallery/pages.templ: 2 allowed calls, want 1"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings = %q, want %q", got, want)
	}
}
