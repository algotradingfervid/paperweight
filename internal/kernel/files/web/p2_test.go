package web_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// FILE-013
func TestServe_UnicodeFileName(t *testing.T) {
	const name = "बिल रसीद.jpg"
	f := newFixture(t, nil)
	content := filestest.JPEG(4096)

	rec := serve(f, uploadReq(t, uploadPath, &f.deepak, part{"file", name, content}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST %s %q: status = %d, want 201 (body %q)", uploadPath, name, rec.Code, rec.Body.String())
	}
	var up struct {
		ID       int64  `json:"id"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &up); err != nil {
		t.Fatalf("upload response %q: %v", rec.Body.String(), err)
	}
	if up.FileName != name {
		t.Errorf("upload file_name = %q, want %q", up.FileName, name)
	}

	rec = serve(f, getReq(http.MethodGet, fileURL(up.ID), &f.deepak))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200 (body %q)", fileURL(up.ID), rec.Code, rec.Body.String())
	}
	const wantCD = `inline; filename="___ ____.jpg"; filename*=UTF-8''%E0%A4%AC%E0%A4%BF%E0%A4%B2%20%E0%A4%B0%E0%A4%B8%E0%A5%80%E0%A4%A6.jpg`
	if got := rec.Header().Values("Content-Disposition"); len(got) != 1 || got[0] != wantCD {
		t.Errorf("GET %s: Content-Disposition = %q, want exactly [%q]", fileURL(up.ID), got, wantCD)
	}
	if !bytes.Equal(rec.Body.Bytes(), content) {
		t.Errorf("GET %s: body differs from the uploaded bytes (%d vs %d bytes)", fileURL(up.ID), rec.Body.Len(), len(content))
	}

	list, err := f.env.Store.List(t.Context(), f.deepak, f.exp)
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	if len(list) != 1 || list[0].ID != up.ID || list[0].Name != name {
		t.Errorf("List = %+v, want one entry id %d named %q", list, up.ID, name)
	}
}
