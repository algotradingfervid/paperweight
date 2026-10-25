package files_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// writeBlob writes b at its BlobPath, as an upload would have left it, and
// returns the path.
func writeBlob(t *testing.T, env *filestest.Env, b []byte) string {
	t.Helper()
	p, err := files.BlobPath(env.DataDir, shaHex(b))
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatalf("write blob %s: %v", p, err)
	}
	return p
}

// blobFile is the absolute path of the blob with this hash.
func blobFile(t *testing.T, env *filestest.Env, sha string) string {
	t.Helper()
	p, err := files.BlobPath(env.DataDir, sha)
	if err != nil {
		t.Fatalf("BlobPath(%q): %v", sha, err)
	}
	return p
}

// setMtime sets path's mtime to the Store clock's now minus ago. GC measures
// age against the Store's clock, never the wall clock.
func setMtime(t *testing.T, env *filestest.Env, path string, ago time.Duration) {
	t.Helper()
	at := env.Clock.Now().Add(-ago)
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return false
}

func runGC(t *testing.T, env *filestest.Env, grace time.Duration) files.GCReport {
	t.Helper()
	rep, err := env.Store.GC(t.Context(), grace)
	if err != nil {
		t.Fatalf("GC(%v) error = %v, want nil", grace, err)
	}
	return rep
}

// FILE-019
func TestBlobGC_GracePeriod(t *testing.T) {
	if files.GCGrace != 30*time.Minute {
		t.Errorf("GCGrace = %v, want 30m", files.GCGrace)
	}
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	young := writeBlob(t, env, filestest.PDF(1000)) // no row, 10 minutes old
	setMtime(t, env, young, 10*time.Minute)
	old := writeBlob(t, env, filestest.PDF(2000)) // no row, 31 minutes old
	setMtime(t, env, old, 31*time.Minute)

	used := filestest.PDF(3000) // has a row, 48 hours old
	if _, err := env.Attach(ravi, exp, "used.pdf", used); err != nil {
		t.Fatalf("Attach(used.pdf): %v", err)
	}
	usedPath := blobFile(t, env, shaHex(used))
	setMtime(t, env, usedPath, 48*time.Hour)

	tmpDir := filepath.Join(env.DataDir, "files", "tmp")
	tmpOld, tmpNew := filepath.Join(tmpDir, "old"), filepath.Join(tmpDir, "new")
	for _, p := range []string{tmpOld, tmpNew} {
		if err := os.WriteFile(p, []byte("partial upload"), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	setMtime(t, env, tmpOld, 31*time.Minute)
	setMtime(t, env, tmpNew, 1*time.Minute)

	rep := runGC(t, env, 30*time.Minute)

	if want := (files.GCReport{Deleted: 1, Kept: 2, TempDeleted: 1}); rep != want {
		t.Errorf("GC report = %+v, want %+v", rep, want)
	}
	if !exists(t, young) {
		t.Errorf("unreferenced blob 10 minutes old was deleted, want kept (inside the grace period)")
	}
	if exists(t, old) {
		t.Errorf("unreferenced blob 31 minutes old still exists, want deleted")
	}
	if !exists(t, usedPath) {
		t.Errorf("referenced blob 48 hours old was deleted, want kept (it has a row)")
	}
	if exists(t, tmpOld) {
		t.Errorf("files/tmp/old (31 minutes old) still exists, want deleted")
	}
	if !exists(t, tmpNew) {
		t.Errorf("files/tmp/new (1 minute old) was deleted, want kept")
	}
	want := []string{blobRel(shaHex(filestest.PDF(1000))), blobRel(shaHex(used))}
	slices.Sort(want)
	if got := env.Blobs(); !reflect.DeepEqual(got, want) {
		t.Errorf("Blobs() after GC = %q, want %q", got, want)
	}
}

// FILE-018
func TestAttachment_RemoveKeepsSharedBlob(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "purchase_order", "read", "write")
	env.Auth.Allow(ravi.ID, "grn", "read", "write")
	po := env.Doc("purchase_order", 12, "draft", "Draft 12", ravi.ID)
	grn := env.Doc("grn", 34, "approved", "GRN/26-27/00034", ravi.ID)

	content := filestest.PDF(50_000)
	sha := shaHex(content)
	poID, err := env.Attach(ravi, po, "bill.pdf", content)
	if err != nil {
		t.Fatalf("Attach to PO: %v", err)
	}
	grnID, err := env.Attach(ravi, grn, "bill.pdf", content)
	if err != nil {
		t.Fatalf("Attach to GRN: %v", err)
	}

	if err := remove(t, env, ravi, poID); err != nil {
		t.Fatalf("Remove(PO row %d) error = %v, want nil", poID, err)
	}
	if rows := env.Rows(po); len(rows) != 0 {
		t.Fatalf("PO rows after Remove = %d, want 0", len(rows))
	}
	// Old enough that only the GRN's row can keep it.
	path := blobFile(t, env, sha)
	setMtime(t, env, path, time.Hour)

	rep := runGC(t, env, 0)

	if want := (files.GCReport{Kept: 1}); rep != want {
		t.Errorf("GC report = %+v, want %+v", rep, want)
	}
	if !exists(t, path) {
		t.Fatalf("shared blob deleted by GC, want kept (the GRN row still uses it)")
	}
	b, m := openBytes(t, env, ravi, grnID)
	if !bytes.Equal(b, content) {
		t.Errorf("Open(GRN row) bytes differ from the upload (len %d, want %d)", len(b), len(content))
	}
	if m.SHA256 != sha {
		t.Errorf("Open(GRN row) SHA256 = %q, want %q", m.SHA256, sha)
	}
}

// FILE-042
func TestAttachment_DraftDeleteCascades(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	env.Auth.Allow(ravi.ID, "grn", "read", "write")
	exp := env.Doc("expense", 12, "draft", "Draft 12", ravi.ID)
	grn := env.Doc("grn", 34, "approved", "GRN/26-27/00034", ravi.ID)

	a, b := filestest.PDF(10_000), filestest.PDF(20_000)
	for _, f := range []struct {
		name    string
		content []byte
	}{{"a.pdf", a}, {"b.pdf", b}} {
		if _, err := env.Attach(ravi, exp, f.name, f.content); err != nil {
			t.Fatalf("Attach(%s) to expense 12: %v", f.name, err)
		}
	}
	if _, err := env.Attach(ravi, grn, "b.pdf", b); err != nil {
		t.Fatalf("Attach(b.pdf) to grn 34: %v", err)
	}

	err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return env.Store.DeleteAll(t.Context(), tx, exp)
	})
	if err != nil {
		t.Fatalf("DeleteAll(expense 12) error = %v, want nil", err)
	}
	if rows := env.Rows(exp); len(rows) != 0 {
		t.Fatalf("expense 12 rows after DeleteAll = %d, want 0", len(rows))
	}
	if rows := env.Rows(grn); len(rows) != 1 {
		t.Fatalf("grn 34 rows after DeleteAll = %d, want 1", len(rows))
	}

	// GC runs after the grace period.
	aPath, bPath := blobFile(t, env, shaHex(a)), blobFile(t, env, shaHex(b))
	setMtime(t, env, aPath, time.Hour)
	setMtime(t, env, bPath, time.Hour)

	rep := runGC(t, env, 0)

	if want := (files.GCReport{Deleted: 1, Kept: 1}); rep != want {
		t.Errorf("GC report = %+v, want %+v", rep, want)
	}
	if exists(t, aPath) {
		t.Errorf("a.pdf's blob (no rows left) still exists, want deleted")
	}
	if !exists(t, bPath) {
		t.Errorf("b.pdf's blob was deleted, want kept (grn 34 still uses it)")
	}
	if got, want := env.Blobs(), []string{blobRel(shaHex(b))}; !reflect.DeepEqual(got, want) {
		t.Errorf("Blobs() after GC = %q, want %q", got, want)
	}
}

// FILE-020
func TestBlobGC_ConcurrentUploadSameContent(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	content := filestest.PDF(4096)
	sha := shaHex(content)
	path := blobFile(t, env, sha)

	for i := range 200 {
		// An orphan blob older than the grace period.
		if !exists(t, path) {
			writeBlob(t, env, content)
		}
		setMtime(t, env, path, time.Hour)

		var (
			wg        sync.WaitGroup
			attachErr error
			gcErr     error
			rep       files.GCReport
		)
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, attachErr = env.Attach(ravi, exp, "bill.pdf", content)
		}()
		go func() {
			defer wg.Done()
			<-start
			rep, gcErr = env.Store.GC(t.Context(), 0)
		}()
		close(start)
		wg.Wait()

		if attachErr != nil {
			t.Fatalf("iteration %d: Attach error = %v, want nil", i, attachErr)
		}
		if gcErr != nil {
			t.Fatalf("iteration %d: GC error = %v, want nil", i, gcErr)
		}
		if rep.Deleted+rep.Kept != 1 {
			t.Fatalf("iteration %d: GC report = %+v, want the one blob either deleted or kept", i, rep)
		}
		rows := env.Rows(exp)
		if len(rows) != 1 {
			t.Fatalf("iteration %d: rows = %d, want 1", i, len(rows))
		}
		for _, r := range rows {
			if r.SHA256 != sha {
				t.Fatalf("iteration %d: row %d SHA256 = %q, want %q", i, r.ID, r.SHA256, sha)
			}
			b, err := os.ReadFile(blobFile(t, env, r.SHA256))
			if err != nil {
				t.Fatalf("iteration %d: row %d's blob: %v (a committed row points to a missing blob)", i, r.ID, err)
			}
			if got := shaHex(b); got != r.SHA256 {
				t.Fatalf("iteration %d: row %d's blob hashes to %q, want its name %q", i, r.ID, got, r.SHA256)
			}
		}
		// Orphan the blob again for the next round.
		for _, r := range rows {
			env.DeleteRow(r.ID)
		}
	}
}
