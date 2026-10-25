package files_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const msgTooLarge = "File is larger than 10 MB. Choose a smaller file or compress it."

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// blobRel is the path of a blob relative to <data_dir>/files, as Env.Blobs returns it.
func blobRel(sha string) string { return sha[:2] + "/" + sha }

func readBlob(t *testing.T, env *filestest.Env, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(env.DataDir, "files", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read blob %s: %v", rel, err)
	}
	return b
}

func tmpEntries(t *testing.T, env *filestest.Env) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(env.DataDir, "files", "tmp"))
	if err != nil {
		t.Fatalf("read files/tmp: %v", err)
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// FILE-001
func TestAttachment_UploadJPEGToDraft(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	content := filestest.JPEG(2456789)
	sha := shaHex(content)

	id, err := env.Attach(ravi, exp, "receipt.jpg", content)
	if err != nil {
		t.Fatalf("Attach(receipt.jpg) error = %v, want nil", err)
	}

	if dt, pid := env.RowParent(id); dt != "expense" || pid != 42 {
		t.Errorf("RowParent(%d) = (%q, %d), want (\"expense\", 42)", id, dt, pid)
	}

	rows := env.Rows(exp)
	if len(rows) != 1 {
		t.Fatalf("Rows(expense 42) = %d rows, want 1: %+v", len(rows), rows)
	}
	m := rows[0]
	if m.ID != id {
		t.Errorf("Meta.ID = %d, want %d", m.ID, id)
	}
	if m.Name != "receipt.jpg" {
		t.Errorf("Meta.Name = %q, want %q", m.Name, "receipt.jpg")
	}
	if m.Size != 2456789 {
		t.Errorf("Meta.Size = %d, want 2456789", m.Size)
	}
	if m.MIME != "image/jpeg" {
		t.Errorf("Meta.MIME = %q, want %q", m.MIME, "image/jpeg")
	}
	if m.SHA256 != sha {
		t.Errorf("Meta.SHA256 = %q, want %q", m.SHA256, sha)
	}
	if m.UploadedBy != ravi.ID {
		t.Errorf("Meta.UploadedBy = %v, want %v", m.UploadedBy, ravi.ID)
	}
	wantAt := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if !m.UploadedAt.Equal(wantAt) {
		t.Errorf("Meta.UploadedAt = %v, want %v", m.UploadedAt, wantAt)
	}

	var stored string
	if err := env.DB.SQL().QueryRowContext(t.Context(),
		"SELECT uploaded_at FROM attachment WHERE id = ?", id).Scan(&stored); err != nil {
		t.Fatalf("read uploaded_at: %v", err)
	}
	if stored != "2026-10-24T05:00:00.000Z" {
		t.Errorf("stored uploaded_at = %q, want %q", stored, "2026-10-24T05:00:00.000Z")
	}

	if got, want := env.Blobs(), []string{blobRel(sha)}; !reflect.DeepEqual(got, want) {
		t.Errorf("Blobs() = %q, want %q", got, want)
	}
	if p, err := files.BlobPath(env.DataDir, sha); err != nil {
		t.Errorf("BlobPath: %v", err)
	} else if b, err := os.ReadFile(p); err != nil {
		t.Errorf("blob at %s: %v", p, err)
	} else if !bytes.Equal(b, content) {
		t.Errorf("blob bytes differ from the upload (len %d, want %d)", len(b), len(content))
	}

	want := "receipt.jpg · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:30"
	if got := files.ListEntry(m, "Ravi Kulkarni"); got != want {
		t.Errorf("ListEntry = %q, want %q", got, want)
	}
}

// FILE-002
func TestAttachment_SizeLimitBoundary(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	atLimit := filestest.PDF(10_485_760)
	if _, err := env.Attach(ravi, exp, "at-limit.pdf", atLimit); err != nil {
		t.Fatalf("Attach(10,485,760 bytes) error = %v, want saved", err)
	}

	_, err := env.Attach(ravi, exp, "over-limit.pdf", filestest.PDF(10_485_761))
	if err == nil {
		t.Fatal("Attach(10,485,761 bytes) error = nil, want refused")
	}
	if k := files.KindOf(err); k != files.KindTooLarge {
		t.Errorf("KindOf = %v, want KindTooLarge (%v)", k, files.KindTooLarge)
	}
	if err.Error() != msgTooLarge {
		t.Errorf("message = %q, want %q", err.Error(), msgTooLarge)
	}

	if rows := env.Rows(exp); len(rows) != 1 {
		t.Errorf("Rows = %d, want 1 (no row for the refused file)", len(rows))
	}
	if got, want := env.Blobs(), []string{blobRel(shaHex(atLimit))}; !reflect.DeepEqual(got, want) {
		t.Errorf("Blobs() = %q, want %q (no blob for the refused file)", got, want)
	}
	if tmp := tmpEntries(t, env); len(tmp) != 0 {
		t.Errorf("files/tmp = %q, want empty", tmp)
	}
}

// FILE-016
func TestAttachment_DedupeAcrossDocuments(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "purchase_order", "read", "write")
	env.Auth.Allow(ravi.ID, "grn", "read", "write")
	po := env.Doc("purchase_order", 12, "draft", "Draft 12", ravi.ID)
	grn := env.Doc("grn", 34, "approved", "GRN/26-27/00034", ravi.ID)

	content := filestest.PDF(50_000)
	sha := shaHex(content)
	if _, err := env.Attach(ravi, po, "bill.pdf", content); err != nil {
		t.Fatalf("Attach to PO: %v", err)
	}
	if _, err := env.Attach(ravi, grn, "bill.pdf", content); err != nil {
		t.Fatalf("Attach to GRN: %v", err)
	}

	poRows, grnRows := env.Rows(po), env.Rows(grn)
	if len(poRows) != 1 || len(grnRows) != 1 {
		t.Fatalf("rows: PO %d, GRN %d; want 1 each", len(poRows), len(grnRows))
	}
	if poRows[0].SHA256 != sha || grnRows[0].SHA256 != sha {
		t.Errorf("SHA256: PO %q, GRN %q; want both %q", poRows[0].SHA256, grnRows[0].SHA256, sha)
	}
	if poRows[0].ID == grnRows[0].ID {
		t.Errorf("both documents share row %d, want two rows", poRows[0].ID)
	}

	blobs := env.Blobs()
	if want := []string{blobRel(sha)}; !reflect.DeepEqual(blobs, want) {
		t.Fatalf("Blobs() = %q, want %q", blobs, want)
	}
	if b := readBlob(t, env, blobs[0]); !bytes.Equal(b, content) {
		t.Errorf("blob bytes differ from the upload (len %d, want %d)", len(b), len(content))
	}
}

// FILE-034
func TestAttachment_BytesUnchanged(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	content := filestest.JPEGWithEXIF(6)
	sha := shaHex(content)
	if _, err := env.Attach(ravi, exp, "photo.jpg", content); err != nil {
		t.Fatalf("Attach(photo.jpg): %v", err)
	}

	rows := env.Rows(exp)
	if len(rows) != 1 || rows[0].SHA256 != sha {
		t.Fatalf("Rows = %+v, want one row with SHA256 %q", rows, sha)
	}
	blobs := env.Blobs()
	if want := []string{blobRel(sha)}; !reflect.DeepEqual(blobs, want) {
		t.Fatalf("Blobs() = %q, want %q", blobs, want)
	}
	b := readBlob(t, env, blobs[0])
	if !bytes.Equal(b, content) {
		t.Errorf("stored bytes differ from uploaded bytes (len %d, want %d)", len(b), len(content))
	}
	if !bytes.Contains(b, []byte("Exif\x00\x00")) {
		t.Error("stored bytes lost the EXIF APP1 segment")
	}
}

// FILE-047
func TestUpload_ConcurrentSameContent(t *testing.T) {
	content := filestest.PDF(3 << 20)
	sha := shaHex(content)
	for i := range 100 {
		env := filestest.New(t)
		ravi := env.User("ravi@vardhan.test")
		deepak := env.User("deepak@vardhan.test")
		env.Auth.Allow(ravi.ID, "purchase_order", "read", "write")
		env.Auth.Allow(deepak.ID, "purchase_order", "read", "write")
		docs := []auth.RecordRef{
			env.Doc("purchase_order", 12, "draft", "Draft 12", ravi.ID),
			env.Doc("purchase_order", 13, "draft", "Draft 13", deepak.ID),
		}
		users := []auth.Principal{ravi, deepak}

		var wg sync.WaitGroup
		errsOut := make([]error, 2)
		for g := range 2 {
			wg.Go(func() {
				// Spool outside any transaction (CD-29), then attach inside db.Write.
				sp, err := env.Store.Spool(bytes.NewReader(content), files.MaxFileBytes)
				if err != nil {
					errsOut[g] = err
					return
				}
				defer func() { _ = sp.Cleanup() }()
				errsOut[g] = env.DB.Write(t.Context(), func(tx *db.Tx) error {
					_, err := env.Store.Attach(t.Context(), tx, users[g], docs[g], "bill.pdf", sp)
					return err
				})
			})
		}
		wg.Wait()

		for g, err := range errsOut {
			if err != nil {
				t.Fatalf("iteration %d: upload %d error = %v, want nil", i, g, err)
			}
		}
		for g, d := range docs {
			if rows := env.Rows(d); len(rows) != 1 || rows[0].SHA256 != sha {
				t.Fatalf("iteration %d: doc %d rows = %+v, want one row with SHA256 %q", i, g, rows, sha)
			}
		}
		blobs := env.Blobs()
		if want := []string{blobRel(sha)}; !reflect.DeepEqual(blobs, want) {
			t.Fatalf("iteration %d: Blobs() = %q, want %q", i, blobs, want)
		}
		b := readBlob(t, env, blobs[0])
		if got := shaHex(b); got != filepath.Base(blobs[0]) {
			t.Fatalf("iteration %d: sha256(blob) = %q, want its name %q", i, got, filepath.Base(blobs[0]))
		}
	}
}
