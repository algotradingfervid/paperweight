package web_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgBlobMissing = "The file is missing from storage. Ask your administrator to restore it from backup."
	hdrCSP         = "sandbox; default-src 'none'"
	hdrCache       = "private, no-store"
)

// getReq builds a request for target signed in as p (nil: signed out).
func getReq(method, target string, p *auth.Principal) *http.Request {
	req := httptest.NewRequest(method, target, nil)
	if p != nil {
		req = req.WithContext(auth.WithPrincipal(req.Context(), *p))
	}
	return req
}

func fileURL(id int64) string { return "/files/" + strconv.FormatInt(id, 10) }

// attach attaches b to ref as p and returns the stored row.
func attach(t *testing.T, f *fixture, p auth.Principal, ref auth.RecordRef, name string, b []byte) files.Meta {
	t.Helper()
	id, err := f.env.Attach(p, ref, name, b)
	if err != nil {
		t.Fatalf("Attach(%q): %v", name, err)
	}
	for _, m := range f.env.Rows(ref) {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("Attach(%q): row %d not found", name, id)
	return files.Meta{}
}

// wantSecurityHeaders checks the download headers every 200/206 carries.
func wantSecurityHeaders(t *testing.T, what string, rec *httptest.ResponseRecorder, mime string) {
	t.Helper()
	for _, h := range []struct{ name, want string }{
		{"Content-Type", mime},
		{"X-Content-Type-Options", "nosniff"},
		{"Content-Security-Policy", hdrCSP},
		{"Cache-Control", hdrCache},
	} {
		if got := rec.Header().Values(h.name); len(got) != 1 || got[0] != h.want {
			t.Errorf("%s: %s = %q, want exactly [%q]", what, h.name, got, h.want)
		}
	}
}

// wantNoSecret checks that no header value and not the body contains any of secrets.
func wantNoSecret(t *testing.T, what string, rec *httptest.ResponseRecorder, secrets ...string) {
	t.Helper()
	for _, s := range secrets {
		if strings.Contains(rec.Body.String(), s) {
			t.Errorf("%s: body contains %q", what, s)
		}
		for name, vals := range rec.Header() {
			for _, v := range vals {
				if strings.Contains(v, s) {
					t.Errorf("%s: header %s = %q contains %q", what, name, v, s)
				}
			}
		}
	}
}

// FILE-027
func TestServe_RequiresLogin(t *testing.T) {
	f := newFixture(t, nil)
	content := filestest.PDF(4096)
	m := attach(t, f, f.deepak, f.exp, "bill.pdf", content)
	if m.ID != 1 {
		t.Fatalf("first attachment id = %d, want 1", m.ID)
	}

	rec := serve(f, getReq(http.MethodGet, "/files/1", nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("signed-out GET /files/1: status = %d, want 303 (body %q)", rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/signin?next=%2Ffiles%2F1"; got != want {
		t.Errorf("signed-out GET /files/1: Location = %q, want %q", got, want)
	}
	if bytes.Contains(rec.Body.Bytes(), content[:64]) {
		t.Errorf("signed-out GET /files/1: body contains the file's bytes")
	}

	req := getReq(http.MethodGet, "/files/1", nil)
	req.Header.Set("HX-Request", "true")
	rec = serve(f, req)
	wantText(t, "signed-out htmx GET /files/1", rec, http.StatusUnauthorized, msgExpired)
	if bytes.Contains(rec.Body.Bytes(), content[:64]) {
		t.Errorf("signed-out htmx GET /files/1: body contains the file's bytes")
	}

	// The signed-in owner gets the file, so the refusals above are about the session.
	rec = serve(f, getReq(http.MethodGet, "/files/1", &f.deepak))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), content) {
		t.Errorf("signed-in GET /files/1: status = %d, body %d bytes; want 200 with the %d stored bytes", rec.Code, rec.Body.Len(), len(content))
	}

	// No static route serves files/ directly.
	shard := m.SHA256[:2]
	for _, target := range []string{"/files/" + shard + "/" + m.SHA256, "/files/"} {
		rec := serve(f, getReq(http.MethodGet, target, &f.deepak))
		if rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: status = %d, want 404", target, rec.Code)
		}
		if bytes.Contains(rec.Body.Bytes(), content[:64]) {
			t.Errorf("GET %s: body contains the file's bytes", target)
		}
	}
}

// FILE-028
func TestServe_IDORAndHashGuess(t *testing.T) {
	f := newFixture(t, nil)
	priya := f.env.User("priya@vardhan.test")
	f.env.Auth.Allow(priya.ID, "expense", "read")
	doc41 := f.env.Doc("expense", 41, "draft", "Draft 41", f.deepak.ID)
	contentA := filestest.PDF(4096)
	contentB := filestest.PNG(4096)
	a := attach(t, f, f.deepak, doc41, "a.pdf", contentA)
	b := attach(t, f, f.deepak, f.exp, "b.png", contentB)
	f.env.Auth.Hide("expense", 42)

	targets := []string{
		fileURL(b.ID),
		"/files/" + b.SHA256,
		fileURL(a.ID) + "/../" + strconv.FormatInt(b.ID, 10),
	}
	for _, target := range targets {
		rec := serve(f, getReq(http.MethodGet, target, &priya))
		wantText(t, "Priya GET "+target, rec, http.StatusNotFound, msgFileNotFound)
		wantNoSecret(t, "Priya GET "+target, rec, a.SHA256, b.SHA256)
	}

	rec := serve(f, getReq(http.MethodGet, fileURL(a.ID), &priya))
	if rec.Code != http.StatusOK {
		t.Errorf("Priya GET %s: status = %d, want 200 (body %q)", fileURL(a.ID), rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), contentA) {
		t.Errorf("Priya GET %s: body is %d bytes, want a.pdf's %d bytes", fileURL(a.ID), rec.Body.Len(), len(contentA))
	}
	wantNoSecret(t, "Priya GET "+fileURL(a.ID), rec, a.SHA256, b.SHA256)
}

// FILE-031
func TestServe_SecurityHeaders(t *testing.T) {
	f := newFixture(t, nil)
	content := filestest.PDF(4096)
	m := attach(t, f, f.deepak, f.exp, "bill.pdf", content)
	if m.MIME != "application/pdf" {
		t.Fatalf("stored MIME = %q, want application/pdf", m.MIME)
	}

	rec := serve(f, getReq(http.MethodGet, fileURL(m.ID), &f.deepak))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET bill.pdf: status = %d, want 200 (body %q)", rec.Code, rec.Body.String())
	}
	wantSecurityHeaders(t, "GET bill.pdf", rec, "application/pdf")
	if got, want := rec.Header().Get("Content-Length"), strconv.FormatInt(m.Size, 10); got != want {
		t.Errorf("GET bill.pdf: Content-Length = %q, want %q (stored size)", got, want)
	}
	if got, want := rec.Header().Get("Content-Disposition"), `inline; filename="bill.pdf"`; got != want {
		t.Errorf("GET bill.pdf: Content-Disposition = %q, want %q", got, want)
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Errorf("GET bill.pdf: body is %d bytes, want the %d stored bytes", rec.Body.Len(), len(content))
	}
}

// FILE-032
func TestServe_InlineVsAttachment(t *testing.T) {
	f := newFixture(t, nil)
	cases := []struct {
		name    string
		content []byte
		kind    string
	}{
		{"bill.pdf", filestest.PDF(4096), "inline"},
		{"r.jpg", filestest.JPEG(4096), "inline"},
		{"r.png", filestest.PNG(4096), "inline"},
		{"r.webp", filestest.WEBP(4096), "inline"},
		{"r.heic", filestest.HEIC("heic"), "attachment"},
		{"rates.xlsx", filestest.XLSX(), "attachment"},
		{"quote.docx", filestest.DOCX(), "attachment"},
		{"items.csv", []byte("Item,Qty,Rate\nCement,10,380\nSteel,1200,62\n"), "attachment"},
	}
	for _, c := range cases {
		m := attach(t, f, f.deepak, f.exp, c.name, c.content)
		t.Run(c.name, func(t *testing.T) {
			rec := serve(f, getReq(http.MethodGet, fileURL(m.ID), &f.deepak))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200 (body %q)", c.name, rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != m.MIME {
				t.Errorf("GET %s: Content-Type = %q, want the stored %q", c.name, got, m.MIME)
			}
			if got, want := rec.Header().Get("Content-Disposition"), c.kind+`; filename="`+c.name+`"`; got != want {
				t.Errorf("GET %s: Content-Disposition = %q, want %q", c.name, got, want)
			}

			rec = serve(f, getReq(http.MethodGet, fileURL(m.ID)+"?download=1", &f.deepak))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s?download=1: status = %d, want 200 (body %q)", c.name, rec.Code, rec.Body.String())
			}
			if got, want := rec.Header().Get("Content-Disposition"), `attachment; filename="`+c.name+`"`; got != want {
				t.Errorf("GET %s?download=1: Content-Disposition = %q, want %q", c.name, got, want)
			}
		})
	}
}

// FILE-036
func TestServe_MissingBlob(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logBuf, nil))
	old := slog.Default()
	slog.SetDefault(logger) // the Store logs to slog.Default() when its Config has no Logger
	t.Cleanup(func() { slog.SetDefault(old) })

	f := newFixture(t, logger)
	m := attach(t, f, f.deepak, f.exp, "bill.pdf", filestest.PDF(4096))
	p, err := files.BlobPath(f.env.DataDir, m.SHA256)
	if err != nil {
		t.Fatalf("BlobPath: %v", err)
	}
	if err := os.Remove(p); err != nil {
		t.Fatalf("delete blob: %v", err)
	}

	rec := serve(f, getReq(http.MethodGet, fileURL(m.ID), &f.deepak))

	wantText(t, "GET with the blob deleted", rec, http.StatusNotFound, msgBlobMissing)
	wantNoSecret(t, "GET with the blob deleted", rec, m.SHA256)

	found := false
	for _, line := range strings.Split(strings.TrimSpace(logBuf.String()), "\n") {
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e["level"] == "ERROR" && e["attachment_id"] == float64(m.ID) && e["sha256"] == m.SHA256 {
			found = true
		}
	}
	if !found {
		t.Errorf("log = %q, want an ERROR line with attachment_id %d and sha256 %s", logBuf.String(), m.ID, m.SHA256)
	}
}

// Review Focus 2
func TestServe_RangeAndHead(t *testing.T) {
	f := newFixture(t, nil)
	content := filestest.PDF(4096)
	m := attach(t, f, f.deepak, f.exp, "bill.pdf", content)

	req := getReq(http.MethodGet, fileURL(m.ID), &f.deepak)
	req.Header.Set("Range", "bytes=0-99")
	rec := serve(f, req)
	if rec.Code != http.StatusPartialContent {
		t.Errorf("Range bytes=0-99: status = %d, want 206 (body %q)", rec.Code, rec.Body.String())
	}
	if !bytes.Equal(rec.Body.Bytes(), content[:100]) {
		t.Errorf("Range bytes=0-99: body is %d bytes, want the first 100 stored bytes", rec.Body.Len())
	}
	wantSecurityHeaders(t, "Range bytes=0-99", rec, "application/pdf")

	rec = serve(f, getReq(http.MethodHead, fileURL(m.ID), &f.deepak))
	if rec.Code != http.StatusOK {
		t.Errorf("HEAD: status = %d, want 200", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Errorf("HEAD: body is %d bytes, want none", rec.Body.Len())
	}
	if got, want := rec.Header().Get("Content-Length"), strconv.Itoa(len(content)); got != want {
		t.Errorf("HEAD: Content-Length = %q, want %q", got, want)
	}
	wantSecurityHeaders(t, "HEAD", rec, "application/pdf")
}

// Review Focus 3
func TestServe_QuoteInFileName(t *testing.T) {
	f := newFixture(t, nil)
	m := attach(t, f, f.deepak, f.exp, `bill "final";x.pdf`, filestest.PDF(4096))
	if m.Name != "bill _final__x.pdf" {
		t.Fatalf("stored name = %q, want %q (Task 1 sanitising)", m.Name, "bill _final__x.pdf")
	}

	rec := serve(f, getReq(http.MethodGet, fileURL(m.ID), &f.deepak))
	if rec.Code != http.StatusOK {
		t.Errorf("GET %q: status = %d, want 200 (body %q)", m.Name, rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Values("Content-Disposition"), []string{`inline; filename="bill _final__x.pdf"`}; len(got) != 1 || got[0] != want[0] {
		t.Errorf("GET %q: Content-Disposition = %q, want %q", m.Name, got, want)
	}

	// A row written directly, bypassing the upload's sanitising.
	const raw = "a\"b\r\nc.pdf"
	var rawID int64
	err := f.env.DB.Write(t.Context(), func(tx *db.Tx) error {
		res, err := tx.SQL().ExecContext(t.Context(),
			`INSERT INTO attachment (parent_doctype, parent_id, file_name, size, content_type, sha256, uploaded_by, uploaded_at)
			 SELECT parent_doctype, parent_id, ?, size, content_type, sha256, uploaded_by, uploaded_at FROM attachment WHERE id = ?`,
			raw, m.ID)
		if err != nil {
			return err
		}
		rawID, err = res.LastInsertId()
		return err
	})
	if err != nil {
		t.Fatalf("insert row named %q: %v", raw, err)
	}

	rec = serve(f, getReq(http.MethodGet, fileURL(rawID), &f.deepak))
	if rec.Code != http.StatusOK {
		t.Errorf("GET %q: status = %d, want 200 (body %q)", raw, rec.Code, rec.Body.String())
	}
	got := rec.Header().Values("Content-Disposition")
	if len(got) != 1 {
		t.Fatalf("GET %q: Content-Disposition = %q, want one value", raw, got)
	}
	cd := got[0]
	if strings.ContainsAny(cd, "\r\n") {
		t.Errorf("GET %q: Content-Disposition %q is not a single line", raw, cd)
	}
	if !strings.Contains(cd, `filename="a_b__c.pdf"`) {
		t.Errorf("GET %q: Content-Disposition %q lacks %q", raw, cd, `filename="a_b__c.pdf"`)
	}
	if want := `inline; filename="a_b__c.pdf"; filename*=UTF-8''` + url.PathEscape(raw); cd != want {
		t.Errorf("GET %q: Content-Disposition = %q, want %q", raw, cd, want)
	}
}
