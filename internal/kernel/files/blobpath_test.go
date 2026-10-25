package files_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
)

// FILE-014
func TestBlobPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	h := "ab" + strings.Repeat("0123456789abcdef", 4)[2:]
	for _, bad := range []string{
		"../../app.db",
		"ab/../../x",
		h[:63],
		strings.ToUpper(h),
	} {
		if _, err := files.BlobPath(dir, bad); !errors.Is(err, files.ErrBadHash) {
			t.Errorf("BlobPath(%q) err = %v, want ErrBadHash", bad, err)
		}
	}

	p, err := files.BlobPath(dir, h)
	if err != nil {
		t.Fatalf("BlobPath(valid) err = %v", err)
	}
	if want := filepath.Join(dir, "files", "ab", h); p != want {
		t.Errorf("BlobPath(valid) = %q, want %q", p, want)
	}

	t.Run("symlinked files dir", func(t *testing.T) {
		dir := t.TempDir()
		real := t.TempDir()
		if err := os.Symlink(real, filepath.Join(dir, "files")); err != nil {
			t.Fatal(err)
		}
		p, err := files.BlobPath(dir, h)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(real, h[:2], h)); err != nil {
			t.Errorf("blob not under the real directory: %v", err)
		}
	})
}
