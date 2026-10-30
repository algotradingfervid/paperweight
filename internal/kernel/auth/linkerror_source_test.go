package auth

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linkErrorText is the part of LinkError's text (CD-240) only this package
// may build.
const linkErrorText = "isn't available to you"

// TestLinkErrorText_OnlyInAuth fails if a .go or .templ file outside
// internal/kernel/auth contains LinkError's text: other packages return
// auth's LinkError instead of copying it (CD-240). Test files outside auth
// are skipped: they must quote the producing package's text (CD-142,
// CD-155), e.g. internal/kernel/doc/nosecondconn_test.go. The floors stop a
// moved root or messages.go from making the walk pass on nothing.
func TestLinkErrorText_OnlyInAuth(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repo root %s has no go.mod: %v", root, err)
	}
	authDir := filepath.Join("internal", "kernel", "auth") + string(filepath.Separator)
	skip := map[string]bool{
		".git": true, ".worktrees": true, ".work": true, "bin": true,
		filepath.Join("internal", "ui", "vendor"): true,
	}
	scanned, inMessages := 0, false
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[rel] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if (!strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".templ")) || strings.HasSuffix(name, "_templ.go") {
			return nil
		}
		inAuth := strings.HasPrefix(rel, authDir)
		if !inAuth && strings.HasSuffix(name, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path) //nolint:gosec // G304: path comes from walking the repo
		if err != nil {
			return err
		}
		scanned++
		if !strings.Contains(string(b), linkErrorText) {
			return nil
		}
		if rel == filepath.Join("internal", "kernel", "auth", "messages.go") {
			inMessages = true
		}
		if !inAuth {
			t.Errorf("%s contains %q: return auth's *LinkError instead (CD-240)", rel, linkErrorText)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 300 {
		t.Errorf("scanned %d files under %s, want at least 300", scanned, root)
	}
	if !inMessages {
		t.Errorf("internal/kernel/auth/messages.go doesn't contain %q, so the walk proves nothing", linkErrorText)
	}
}
