package authweb_test

// Polish P7 (CD-206 part 3): web.BodyCeiling in front of the real AUTH guard.
// validCSRF calls PostFormValue, so a multipart POST is parsed before anything
// else looks at it. Each body carries the session's own csrf_token as a form
// field: left unlimited it passes the guard and reaches the handler, so only
// the ceiling can stop it.

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/web"
)

const (
	p7Limit = 1 << 20
	p7Text  = "This request is too large."
	// p7FileSize is the oversize file part. It must exceed the 32 MiB that
	// ParseMultipartForm holds in memory, or no temp file would ever appear
	// and the spool checks below could not fail.
	p7FileSize = 40 << 20
	// p7ReadBuffer is the most the ceiling may read past the limit: one read of
	// the multipart reader's buffer.
	p7ReadBuffer = 4096
)

// p7Zeros reads as an endless run of zero bytes.
type p7Zeros struct{}

func (p7Zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// p7Body is a request body that counts the bytes read from it and, after each
// read, looks in tmp: ReadForm removes its temp file when it fails, so what
// remains at the end doesn't show whether one was created on the way.
type p7Body struct {
	r       io.Reader
	tmp     string
	read    int64
	sawTemp bool
}

func (b *p7Body) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.read += int64(n)
	if ents, derr := os.ReadDir(b.tmp); derr == nil && len(ents) > 0 {
		b.sawTemp = true
	}
	return n, err
}

// p7Multipart returns a multipart body holding the csrf_token field and a file
// part of size bytes, its length and its content type. Nothing is held in
// memory for the file part, which is why the writer's framing is split by hand.
func p7Multipart(t *testing.T, token string, size int64) (io.Reader, int64, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("csrf_token", token); err != nil {
		t.Fatalf("write csrf_token: %v", err)
	}
	if _, err := mw.CreateFormFile("file", "big.pdf"); err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	head := bytes.Clone(buf.Bytes())
	buf.Reset()
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	tail := bytes.Clone(buf.Bytes())
	body := io.MultiReader(bytes.NewReader(head), io.LimitReader(p7Zeros{}, size), bytes.NewReader(tail))
	return body, int64(len(head)) + size + int64(len(tail)), mw.FormDataContentType()
}

// p7World signs Ravi in with write on the guarded save route. It returns the
// ceiling in front of the real guard, the stub counters, Ravi's session cookie
// and CSRF token. TMPDIR is set last, so the temp directory it returns sees
// only what the request creates.
func p7World(t *testing.T) (gate http.Handler, st *t10Stubs, ck *http.Cookie, token, tmp string) {
	t.Helper()
	w := authtest.NewWorld(t)
	w.Grant(t, "PO Writer", authtest.DTOrder, auth.RightWrite, auth.ScopeAll)
	w.AddRole(t, t10Ravi, "PO Writer")
	h, st := t10Harness(t, w)
	c := signedIn(t, h, t10Ravi)
	token = csrfOf(t, c)
	ck = c.Cookie()
	if ck == nil {
		t.Fatal("no session cookie after sign-in")
	}
	tmp = t.TempDir()
	t.Setenv("TMPDIR", tmp)
	return web.BodyCeiling(p7Limit, p7Text)(h.Router), st, ck, token, tmp
}

// p7Post builds Ravi's multipart POST to the guarded save route. Its
// ContentLength is -1, as a chunked body arrives.
func p7Post(ck *http.Cookie, body io.Reader, contentType string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/po/1/save", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(ck)
	req.ContentLength = -1
	return req
}

func p7WantNoTemp(t *testing.T, tmp string, body *p7Body) {
	t.Helper()
	if body.sawTemp {
		t.Error("a temp file appeared in TMPDIR while the body was read")
	}
	ents, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("read TMPDIR: %v", err)
	}
	for _, e := range ents {
		t.Errorf("TMPDIR holds %q, want nothing", e.Name())
	}
}

func TestBodyCeiling_DeclaredOversizeMultipartRefusedBeforeCSRF(t *testing.T) {
	gate, st, ck, token, tmp := p7World(t)
	src, size, ct := p7Multipart(t, token, p7FileSize)
	body := &p7Body{r: src, tmp: tmp}
	req := p7Post(ck, body, ct)
	req.ContentLength = size

	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 (body %q)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), p7Text) {
		t.Errorf("body %q, want %q", rec.Body.String(), p7Text)
	}
	if st.Saves() != 0 {
		t.Errorf("reached the handler %d times", st.Saves())
	}
	if body.read != 0 {
		t.Errorf("bytes read from body = %d, want 0", body.read)
	}
	p7WantNoTemp(t, tmp, body)
}

func TestBodyCeiling_ChunkedOversizeMultipartNeverSpools(t *testing.T) {
	gate, st, ck, token, tmp := p7World(t)

	// The same request under the limit passes the guard, so a refusal below
	// is the ceiling's and not a broken fixture's.
	src, _, ct := p7Multipart(t, token, 4096)
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, p7Post(ck, src, ct))
	if rec.Code != http.StatusOK || st.Saves() != 1 {
		t.Fatalf("under the limit: status = %d, saves = %d, want 200 and 1 (body %q)", rec.Code, st.Saves(), rec.Body.String())
	}

	src, _, ct = p7Multipart(t, token, p7FileSize)
	body := &p7Body{r: src, tmp: tmp}
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, p7Post(ck, body, ct))

	if rec.Code != http.StatusForbidden && rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 403 or 413 (body %q)", rec.Code, rec.Body.String())
	}
	if st.Saves() != 1 {
		t.Errorf("oversize body reached the handler: saves = %d, want still 1", st.Saves())
	}
	if body.read > p7Limit+p7ReadBuffer {
		t.Errorf("bytes read from body = %d, want at most %d", body.read, p7Limit+p7ReadBuffer)
	}
	p7WantNoTemp(t, tmp, body)
}
