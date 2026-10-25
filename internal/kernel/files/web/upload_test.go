package web_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/files/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgTooLarge     = "File is larger than 10 MB. Choose a smaller file or compress it."
	msgStorage      = "Couldn't save the file. Try again or contact your administrator."
	msgNoFilePart   = "Choose a file to upload."
	msgTwoFileParts = "Upload one file at a time."
	msgEmpty        = "File is empty. Choose another file."
	msgExpired      = "Your session has expired. Sign in again."
	msgFileNotFound = "File not found."
	msgNoAddRight   = "You don't have permission to add files to Draft 42."
	msgNoChange     = "You don't have permission to change Expense Claim. Ask your administrator for a role that allows it."
	msgRemoveLocked = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
	textPlain       = "text/plain; charset=utf-8"
	uploadPath      = "/attachments/expense/42"
)

// fixture is an Env with Deepak (read and write on expense) and his draft
// expense Draft 42, served by web.Routes.
type fixture struct {
	env    *filestest.Env
	deepak auth.Principal
	exp    auth.RecordRef
	router chi.Router
}

func newFixture(t *testing.T, logger *slog.Logger) *fixture {
	t.Helper()
	env := filestest.New(t)
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", deepak.ID)
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	r := web.Routes(web.Config{Store: env.Store, DB: env.DB, Logger: logger})
	return &fixture{env: env, deepak: deepak, exp: exp, router: r}
}

type part struct {
	field, name string
	body        []byte
}

func multipartBody(t *testing.T, parts ...part) (body []byte, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		w, err := mw.CreateFormFile(p.field, p.name)
		if err != nil {
			t.Fatalf("CreateFormFile(%q, %q): %v", p.field, p.name, err)
		}
		if _, err := w.Write(p.body); err != nil {
			t.Fatalf("write part %q: %v", p.name, err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

// uploadReq builds a POST to target carrying parts, signed in as p (nil: signed out).
func uploadReq(t *testing.T, target string, p *auth.Principal, parts ...part) *http.Request {
	t.Helper()
	body, ct := multipartBody(t, parts...)
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *p))
	}
	return req
}

func serve(f *fixture, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// wantText checks status, a text/plain content type and the body (one trailing newline allowed).
func wantText(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status = %d, want %d (body %q)", what, rec.Code, status, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != textPlain {
		t.Errorf("%s: Content-Type = %q, want %q", what, ct, textPlain)
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != msg {
		t.Errorf("%s: body = %q, want %q", what, got, msg)
	}
}

func wantTmpEmpty(t *testing.T, env *filestest.Env) {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(env.DataDir, "files", "tmp"))
	if err != nil {
		t.Fatalf("read files/tmp: %v", err)
	}
	if len(ents) != 0 {
		names := make([]string, 0, len(ents))
		for _, e := range ents {
			names = append(names, e.Name())
		}
		t.Errorf("files/tmp = %v, want empty", names)
	}
}

func wantNoRows(t *testing.T, f *fixture) {
	t.Helper()
	if rows := f.env.Rows(f.exp); len(rows) != 0 {
		t.Errorf("attachment rows = %+v, want none", rows)
	}
}

func wantNoBlobs(t *testing.T, env *filestest.Env) {
	t.Helper()
	if b := env.Blobs(); len(b) != 0 {
		t.Errorf("blobs = %v, want none", b)
	}
}

// countingReader counts the bytes the handler reads from the body.
type countingReader struct {
	mu sync.Mutex
	r  io.Reader
	n  int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.mu.Lock()
	c.n += int64(n)
	c.mu.Unlock()
	return n, err
}

func (c *countingReader) count() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// FILE-003
func TestUpload_DeclaredSizeTooLarge(t *testing.T) {
	f := newFixture(t, nil)
	body, ct := multipartBody(t, part{"file", "bill.pdf", filestest.PDF(4096)})
	cr := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, uploadPath, cr)
	req.Header.Set("Content-Type", ct)
	req.ContentLength = 52428800
	req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))

	rec := serve(f, req)

	wantText(t, "upload declaring 52428800 bytes", rec, http.StatusRequestEntityTooLarge, msgTooLarge)
	if n := cr.count(); n != 0 {
		t.Errorf("bytes read from body = %d, want 0", n)
	}
	wantTmpEmpty(t, f.env)
	wantNoRows(t, f)
	wantNoBlobs(t, f.env)
}

// FILE-004
func TestUpload_StreamedOverLimitCleansTemp(t *testing.T) {
	f := newFixture(t, nil)
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w, err := mw.CreateFormFile("file", "big.pdf")
		if err != nil {
			_ = pw.CloseWithError(err)
			return
		}
		const total = 12 << 20
		chunk := filestest.PDF(64 << 10)
		for sent := 0; sent < total; sent += len(chunk) {
			if _, err := w.Write(chunk); err != nil {
				return // the handler stopped reading
			}
		}
		if err := mw.Close(); err != nil {
			return
		}
		_ = pw.Close()
	}()

	req := httptest.NewRequest(http.MethodPost, uploadPath, pr)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.ContentLength = -1
	req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))

	rec := serve(f, req)
	_ = pr.CloseWithError(io.ErrClosedPipe) // unblock the writer if the handler stopped early
	wg.Wait()

	wantText(t, "streamed 12 MB upload", rec, http.StatusRequestEntityTooLarge, msgTooLarge)
	wantTmpEmpty(t, f.env)
	wantNoRows(t, f)
	wantNoBlobs(t, f.env)
}

// FILE-015
func TestUpload_DBFailureLeavesNoRow(t *testing.T) {
	f := newFixture(t, nil)
	err := f.env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(),
			`CREATE TRIGGER fail_attach BEFORE INSERT ON attachment BEGIN SELECT RAISE(ABORT, 'forced'); END;`)
		return err
	})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", "bill.pdf", filestest.PDF(4096)}))

	wantText(t, "upload with the insert forced to fail", rec, http.StatusInternalServerError, msgStorage)
	wantNoRows(t, f)
	wantTmpEmpty(t, f.env)
	for _, b := range f.env.Blobs() {
		data, err := os.ReadFile(filepath.Join(f.env.DataDir, "files", filepath.FromSlash(b)))
		if err != nil {
			t.Fatalf("read blob %s: %v", b, err)
		}
		sum := sha256.Sum256(data)
		if got, want := hex.EncodeToString(sum[:]), path.Base(b); got != want {
			t.Errorf("blob %s hashes to %s, want its name (complete, never partial)", b, got)
		}
	}
}

// FILE-037 (same message as FILE-015, CD-73)
func TestUpload_StorageWriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	old := slog.Default()
	slog.SetDefault(logger) // the Store logs to slog.Default() when its Config has no Logger
	t.Cleanup(func() { slog.SetDefault(old) })

	f := newFixture(t, logger)
	filesDir := filepath.Join(f.env.DataDir, "files")
	if err := os.Chmod(filesDir, 0o555); err != nil { //nolint:gosec // test dir made read-only to force a storage write error
		t.Fatalf("chmod files/: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filesDir, 0o700) }) //nolint:gosec // restores the test dir for cleanup

	rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", "bill.pdf", filestest.PDF(4096)}))

	wantText(t, "upload with files/ read-only", rec, http.StatusInternalServerError, msgStorage)
	wantNoRows(t, f)
	wantTmpEmpty(t, f.env)

	found := false
	for _, line := range strings.Split(strings.TrimSpace(logBuf.String()), "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) != nil {
			continue
		}
		if s, ok := m["err"].(string); ok && strings.Contains(s, "permission denied") {
			found = true
		}
	}
	if !found {
		t.Errorf("log = %q, want a JSON line whose \"err\" contains %q", logBuf.String(), "permission denied")
	}
}

// Review Focus 4
func TestUpload_FilePartRules(t *testing.T) {
	t.Run("no file part", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"note", "bill.pdf", filestest.PDF(4096)}))
		wantText(t, "upload with no file part", rec, http.StatusUnprocessableEntity, msgNoFilePart)
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)
	})

	t.Run("two file parts", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := serve(f, uploadReq(t, uploadPath, &f.deepak,
			part{"file", "bill.pdf", filestest.PDF(4096)},
			part{"file", "r.png", filestest.PNG(4096)}))
		wantText(t, "upload with two file parts", rec, http.StatusUnprocessableEntity, msgTwoFileParts)
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)
		wantNoBlobs(t, f.env)
	})

	t.Run("empty file part", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", "bill.pdf", nil}))
		wantText(t, "upload with an empty file part", rec, http.StatusUnprocessableEntity, msgEmpty)
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)
	})
}

// Task 7 Interfaces: 201 JSON on success; signed out 401 (CD-17); a bad id 404;
// no write on the parent keeps FILE-024's text (CD-126).
func TestUpload_Responses(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		f := newFixture(t, nil)
		content := filestest.PDF(4096)
		rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", "bill.pdf", content}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload bill.pdf: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
		}
		rows := f.env.Rows(f.exp)
		if len(rows) != 1 {
			t.Fatalf("rows = %+v, want 1", rows)
		}
		want := map[string]any{
			"id":           float64(rows[0].ID),
			"file_name":    "bill.pdf",
			"size":         float64(len(content)),
			"content_type": "application/pdf",
		}
		if len(got) != len(want) {
			t.Errorf("body keys = %v, want exactly %v", got, want)
		}
		for k, v := range want {
			if got[k] != v {
				t.Errorf("body[%q] = %v, want %v", k, got[k], v)
			}
		}
		wantTmpEmpty(t, f.env)
	})

	t.Run("signed out", func(t *testing.T) {
		f := newFixture(t, nil)
		rec := serve(f, uploadReq(t, uploadPath, nil, part{"file", "bill.pdf", filestest.PDF(4096)}))
		wantText(t, "signed-out upload", rec, http.StatusUnauthorized, msgExpired)
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)

		req := uploadReq(t, uploadPath, nil, part{"file", "bill.pdf", filestest.PDF(4096)})
		req.Header.Set("HX-Request", "true")
		rec = serve(f, req)
		wantText(t, "signed-out htmx upload", rec, http.StatusUnauthorized, msgExpired)
		if got, want := rec.Header().Get("HX-Redirect"), "/signin?next=%2Fattachments%2Fexpense%2F42"; got != want {
			t.Errorf("HX-Redirect = %q, want %q", got, want)
		}
		wantNoRows(t, f)
	})

	t.Run("bad id", func(t *testing.T) {
		f := newFixture(t, nil)
		for _, target := range []string{"/attachments/expense/abc", "/attachments/expense/0", "/attachments/expense/-1"} {
			rec := serve(f, uploadReq(t, target, &f.deepak, part{"file", "bill.pdf", filestest.PDF(4096)}))
			wantText(t, "upload to "+target, rec, http.StatusNotFound, msgFileNotFound)
		}
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)
	})

	t.Run("no write", func(t *testing.T) {
		f := newFixture(t, nil)
		ravi := f.env.User("ravi@vardhan.test")
		f.env.Auth.Allow(ravi.ID, "expense", "read")
		rec := serve(f, uploadReq(t, uploadPath, &ravi, part{"file", "bill.pdf", filestest.PDF(4096)}))
		wantText(t, "upload as Ravi without write", rec, http.StatusForbidden, msgNoAddRight)
		wantNoRows(t, f)
		wantTmpEmpty(t, f.env)
		wantNoBlobs(t, f.env)
	})
}

// Task 7 Step 4: POST /files/{id}/remove → Store.Remove inside db.Write, 204 on
// success; refusals use the Store's status and text (CD-126).
func TestRemove_Route(t *testing.T) {
	setup := func(t *testing.T) (*fixture, int64) {
		t.Helper()
		f := newFixture(t, nil)
		id, err := f.env.Attach(f.deepak, f.exp, "receipt.jpg", filestest.JPEG(4096))
		if err != nil {
			t.Fatalf("Attach(receipt.jpg): %v", err)
		}
		return f, id
	}
	removeReq := func(id string, p *auth.Principal) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/files/"+id+"/remove", nil)
		if p != nil {
			req = req.WithContext(auth.WithPrincipal(req.Context(), *p))
		}
		return req
	}
	idStr := func(id int64) string { return strconv.FormatInt(id, 10) }

	t.Run("success", func(t *testing.T) {
		f, id := setup(t)
		rec := serve(f, removeReq(idStr(id), &f.deepak))
		if rec.Code != http.StatusNoContent {
			t.Errorf("remove: status = %d, want 204 (body %q)", rec.Code, rec.Body.String())
		}
		wantNoRows(t, f)
		removed := 0
		for _, e := range f.env.Audit.Entries() {
			if e.Action == "attachment_removed" {
				removed++
			}
		}
		if removed != 1 {
			t.Errorf("attachment_removed audit entries = %d, want 1", removed)
		}
	})

	t.Run("no write", func(t *testing.T) {
		f, id := setup(t)
		ravi := f.env.User("ravi@vardhan.test")
		f.env.Auth.Allow(ravi.ID, "expense", "read")
		rec := serve(f, removeReq(idStr(id), &ravi))
		wantText(t, "remove as Ravi without write", rec, http.StatusForbidden, msgNoChange)
		if rows := f.env.Rows(f.exp); len(rows) != 1 {
			t.Errorf("rows after refused remove = %+v, want the row kept", rows)
		}
	})

	t.Run("hidden parent", func(t *testing.T) {
		f, id := setup(t)
		f.env.Auth.Hide("expense", 42)
		rec := serve(f, removeReq(idStr(id), &f.deepak))
		wantText(t, "remove with the parent hidden", rec, http.StatusNotFound, msgFileNotFound)
		if rows := f.env.Rows(f.exp); len(rows) != 1 {
			t.Errorf("rows after refused remove = %+v, want the row kept", rows)
		}
	})

	t.Run("missing attachment", func(t *testing.T) {
		f, id := setup(t)
		for _, target := range []string{idStr(id + 1000), "abc", "0"} {
			rec := serve(f, removeReq(target, &f.deepak))
			wantText(t, "remove /files/"+target, rec, http.StatusNotFound, msgFileNotFound)
		}
	})

	t.Run("submitted", func(t *testing.T) {
		f, id := setup(t)
		f.env.SetState(f.exp, "pending_approval")
		rec := serve(f, removeReq(idStr(id), &f.deepak))
		wantText(t, "remove from a submitted expense", rec, http.StatusUnprocessableEntity, msgRemoveLocked)
	})

	t.Run("signed out", func(t *testing.T) {
		f, id := setup(t)
		rec := serve(f, removeReq(idStr(id), nil))
		wantText(t, "signed-out remove", rec, http.StatusUnauthorized, msgExpired)
		if rows := f.env.Rows(f.exp); len(rows) != 1 {
			t.Errorf("rows after signed-out remove = %+v, want the row kept", rows)
		}
	})
}
