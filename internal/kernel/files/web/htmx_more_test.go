package web_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// CD-241: refusals TestFiles_HTMXRefusalNotices doesn't reach (a body that is
// not multipart, a download with a bad id, a non-file part over the limit) are
// notices for htmx too.
func TestFiles_HTMXRefusalsOutsideTheTable(t *testing.T) {
	t.Run("not multipart", func(t *testing.T) {
		f := newFixture(t, nil)
		req := httptest.NewRequest(http.MethodPost, uploadPath, strings.NewReader("x"))
		req.Header.Set("Content-Type", "text/plain")
		req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))
		wantNotice(t, "not multipart", serve(f, asHTMX(req, true)), http.StatusUnprocessableEntity, msgNoFilePart)
	})

	t.Run("download with a bad id", func(t *testing.T) {
		f := newFixture(t, nil)
		req := httptest.NewRequest(http.MethodGet, "/files/abc", nil)
		req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))
		wantNotice(t, "download with a bad id", serve(f, asHTMX(req, true)), http.StatusNotFound, msgFileNotFound)
	})

	t.Run("non-file part over the limit", func(t *testing.T) {
		f := newFixture(t, nil)
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		w, err := mw.CreateFormFile("note", "big.pdf")
		if err != nil {
			t.Fatalf("CreateFormFile: %v", err)
		}
		if _, err := w.Write(filestest.PDF(int(files.MaxFileBytes) + 128<<10)); err != nil {
			t.Fatalf("write part: %v", err)
		}
		if err := mw.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, uploadPath, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.ContentLength = -1
		req = req.WithContext(auth.WithPrincipal(req.Context(), f.deepak))
		wantNotice(t, "non-file part over the limit", serve(f, asHTMX(req, true)), http.StatusRequestEntityTooLarge, msgTooLarge)
	})
}
