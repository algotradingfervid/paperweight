package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// The 201 body comes from the upload's own transaction, so an uploader with
// write but no read right still gets it.
func TestUpload_WriteOnlyGetsCreated(t *testing.T) {
	f := newFixture(t, nil)
	ravi := f.env.User("ravi@vardhan.test")
	f.env.Auth.Allow(ravi.ID, "expense", "write")
	content := filestest.PDF(4096)

	rec := serve(f, uploadReq(t, uploadPath, &ravi, part{"file", "bill.pdf", content}))

	if rec.Code != http.StatusCreated {
		t.Fatalf("upload as Ravi with write only: status = %d, want 201 (body %q)", rec.Code, rec.Body.String())
	}
	rows := f.env.Rows(f.exp)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want 1", rows)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("body %q is not JSON: %v", rec.Body.String(), err)
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
}

// A malformed multipart body is the client's fault: 422, not a storage error.
func TestUpload_MalformedBody(t *testing.T) {
	f := newFixture(t, nil)
	body := "--zz\r\nnot a header line\r\n\r\n%PDF-1.4\r\n--zz--\r\n"
	req := httptest.NewRequest(http.MethodPost, uploadPath, strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=zz")
	req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))

	rec := serve(f, req)

	wantText(t, "malformed upload", rec, http.StatusUnprocessableEntity, msgNoFilePart)
	wantNoRows(t, f)
	wantTmpEmpty(t, f.env)
}
