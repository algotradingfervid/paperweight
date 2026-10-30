package web_test

// Polish P7 (CD-206 part 3): web.BodyCeiling with the real limit in front of
// the real upload handler. The handler keeps its own limit and answers with the
// same text as plain text, so the ceiling's answer is told from the handler's
// by its alert markup.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/files/web"
	platformweb "github.com/vardhan/internaltools/internal/platform/web"
)

const realCeiling = files.MaxFileBytes + web.MultipartOverhead

// ceiled serves req through the ceiling at the real limit, then Routes.
func ceiled(f *fixture, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	platformweb.BodyCeiling(realCeiling, files.MsgTooLarge)(f.router).ServeHTTP(rec, req)
	return rec
}

// overLimitBody is a real upload whose whole body is one byte over the ceiling.
func overLimitBody(t *testing.T) (body []byte, contentType string) {
	t.Helper()
	const probe = 4096
	sample, _ := multipartBody(t, part{"file", "big.pdf", filestest.PDF(probe)})
	framing := int64(len(sample)) - probe
	body, contentType = multipartBody(t, part{"file", "big.pdf", filestest.PDF(int(realCeiling + 1 - framing))})
	if int64(len(body)) != realCeiling+1 {
		t.Fatalf("body = %d bytes, want %d", len(body), realCeiling+1)
	}
	return body, contentType
}

func TestBodyCeiling_LegalMaximumUploadPasses(t *testing.T) {
	f := newFixture(t, nil)
	content := filestest.PDF(int(files.MaxFileBytes))
	req := uploadReq(t, uploadPath, &f.deepak,
		part{"csrf_token", "", bytes.Repeat([]byte("t"), 43)}, // a page's form sends this first
		part{"file", "max.pdf", content})
	if req.ContentLength <= files.MaxFileBytes || req.ContentLength > realCeiling {
		t.Fatalf("body = %d bytes, want more than the file (%d) and at most the ceiling (%d)", req.ContentLength, files.MaxFileBytes, realCeiling)
	}

	rec := ceiled(f, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
	}
	var got struct {
		Size int64 `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
	}
	if got.Size != files.MaxFileBytes {
		t.Errorf("size = %d, want %d", got.Size, files.MaxFileBytes)
	}
	if rows := f.env.Rows(f.exp); len(rows) != 1 {
		t.Errorf("rows = %+v, want 1", rows)
	}
	wantTmpEmpty(t, f.env)
}

// The declared case is the ceiling's own 413: the upload handler never runs, so
// the answer is the ceiling's full alert page, not the handler's plain text, and
// the body is not read.
func TestBodyCeiling_OverLimitUploadRefused(t *testing.T) {
	f := newFixture(t, nil)
	body, ct := overLimitBody(t)
	cr := &countingReader{r: bytes.NewReader(body)}
	req := httptest.NewRequest(http.MethodPost, uploadPath, cr)
	req.Header.Set("Content-Type", ct)
	req.ContentLength = int64(len(body))
	req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))

	rec := ceiled(f, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 (body %q)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want the ceiling's text/html page, not the handler's %q", ct, textPlain)
	}
	if want := `<div role="alert">` + msgTooLarge + `</div>`; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("body %q, want the ceiling's alert %q", rec.Body.String(), want)
	}
	if n := cr.count(); n != 0 {
		t.Errorf("bytes read from body = %d, want 0", n)
	}
	wantTmpEmpty(t, f.env)
	wantNoRows(t, f)
	wantNoBlobs(t, f.env)
}

// With no Content-Length the ceiling can't answer first, so the request reaches
// the handler, which must still give its own 413 and leave nothing behind.
func TestBodyCeiling_ChunkedOverLimitUploadRefused(t *testing.T) {
	f := newFixture(t, nil)
	body, ct := overLimitBody(t)
	req := httptest.NewRequest(http.MethodPost, uploadPath, bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	req.ContentLength = -1
	req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))

	rec := ceiled(f, req)

	wantText(t, "chunked upload over the ceiling", rec, http.StatusRequestEntityTooLarge, msgTooLarge)
	wantTmpEmpty(t, f.env)
	wantNoRows(t, f)
	wantNoBlobs(t, f.env)
}
