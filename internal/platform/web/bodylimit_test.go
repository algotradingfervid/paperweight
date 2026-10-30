package web_test

// Tests for polish P7 (CD-206 part 3): the request body ceiling. The refusal
// goes through web.Error like every other answer from this package (CD-47), so
// its text is read from the role="alert" div. This file imports nothing beyond
// the standard library and platform/web (CD-90): the real limit belongs to the
// caller, so these tests use their own.

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/web"
)

const (
	ceilingLimit = 1024
	ceilingText  = "This request is too large."
	// ceilingReadBuffer is the most a ceiling may read past the limit: one read
	// of the multipart reader's buffer.
	ceilingReadBuffer = 4096
)

// bytesRead counts what is read from the request body.
type bytesRead struct {
	r io.Reader
	n int64
}

func (b *bytesRead) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	b.n += int64(n)
	return n, err
}

// ceilingPost builds a POST of n bytes. Declared false sends no Content-Length,
// as a chunked body arrives.
func ceilingPost(n int, declared bool) (*http.Request, *bytesRead) {
	body := &bytesRead{r: bytes.NewReader(bytes.Repeat([]byte("x"), n))}
	req := httptest.NewRequest(http.MethodPost, "/", body)
	req.ContentLength = -1
	if declared {
		req.ContentLength = int64(n)
	}
	return req, body
}

// A body of exactly the limit reaches the handler whole, declared or chunked.
func TestBodyCeiling_ExactlyAtLimitPasses(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared bool
	}{{"declared", true}, {"chunked", false}} {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			var readErr error
			reached := false
			h := web.BodyCeiling(ceilingLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				reached = true
				got, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			req, _ := ceilingPost(ceilingLimit, tc.declared)

			rec := serve(t, h, req)

			if !reached {
				t.Fatalf("handler not reached; status = %d (body %q)", rec.Code, rec.Body.String())
			}
			if rec.Code != http.StatusNoContent {
				t.Errorf("status = %d, want 204 from the handler", rec.Code)
			}
			if readErr != nil {
				t.Errorf("reading the body: %v, want no error at exactly the limit", readErr)
			}
			if len(got) != ceilingLimit {
				t.Errorf("handler read %d bytes, want %d", len(got), ceilingLimit)
			}
		})
	}
}

// One declared byte over: 413 with the exact text, and neither the handler nor
// the body is touched.
func TestBodyCeiling_DeclaredLimitPlusOneRefused(t *testing.T) {
	reached := false
	h := web.BodyCeiling(ceilingLimit)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		reached = true
	}))
	req, body := ceilingPost(ceilingLimit+1, true)

	rec := serve(t, h, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 (body %q)", rec.Code, rec.Body.String())
	}
	m := alertRe.FindStringSubmatch(rec.Body.String())
	if m == nil || m[1] != ceilingText {
		t.Errorf("body %q, want a role=\"alert\" div holding exactly %q", rec.Body.String(), ceilingText)
	}
	if reached {
		t.Error("handler reached")
	}
	if body.n != 0 {
		t.Errorf("bytes read from body = %d, want 0", body.n)
	}
}

// A body with no Content-Length is only found out by reading it: the handler
// gets the limit's worth of bytes and then *http.MaxBytesError, and the rest is
// never read.
func TestBodyCeiling_ReadPastLimitSeesMaxBytesError(t *testing.T) {
	for _, tc := range []struct {
		name string
		size int
	}{{"limit_plus_one", ceilingLimit + 1}, {"far_over", ceilingLimit + 64<<10}} {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			var readErr error
			h := web.BodyCeiling(ceilingLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusNoContent)
			}))
			req, body := ceilingPost(tc.size, false)

			serve(t, h, req)

			var mb *http.MaxBytesError
			if !errors.As(readErr, &mb) {
				t.Fatalf("reading past the limit: error = %v, want *http.MaxBytesError", readErr)
			}
			if mb.Limit != ceilingLimit {
				t.Errorf("MaxBytesError.Limit = %d, want %d", mb.Limit, ceilingLimit)
			}
			if len(got) != ceilingLimit {
				t.Errorf("handler read %d bytes, want %d", len(got), ceilingLimit)
			}
			if body.n > ceilingLimit+ceilingReadBuffer {
				t.Errorf("bytes read from body = %d, want at most %d", body.n, ceilingLimit+ceilingReadBuffer)
			}
		})
	}
}

// A GET with no body passes as it came.
func TestBodyCeiling_GetWithNoBodyUntouched(t *testing.T) {
	reached := false
	var readErr error
	var got []byte
	h := web.BodyCeiling(ceilingLimit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		got, readErr = io.ReadAll(r.Body)
		_, _ = w.Write([]byte("HOME"))
	}))

	rec := serve(t, h, httptest.NewRequest(http.MethodGet, "/", nil))

	if !reached {
		t.Fatalf("handler not reached; status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "HOME" {
		t.Errorf("response = %d %q, want 200 \"HOME\"", rec.Code, rec.Body.String())
	}
	if readErr != nil || len(got) != 0 {
		t.Errorf("reading the empty body: %d bytes, error %v, want none", len(got), readErr)
	}
}

// A nil body stays nil: wrapping it would make the first read panic.
func TestBodyCeiling_NilBodyLeftAlone(t *testing.T) {
	reached := false
	var body io.ReadCloser
	h := web.BodyCeiling(ceilingLimit)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		reached = true
		body = r.Body
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Body = nil

	rec := serve(t, h, req)

	if !reached {
		t.Fatalf("handler not reached; status = %d (body %q)", rec.Code, rec.Body.String())
	}
	if body != nil {
		t.Errorf("handler saw body %T, want nil", body)
	}
}

// A limit below 1 is a programmer error, found when the middleware is built
// rather than on the first request.
func TestBodyCeiling_LimitBelowOnePanicsAtConstruction(t *testing.T) {
	for _, limit := range []int64{0, -1} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("BodyCeiling(%d) did not panic", limit)
				}
			}()
			web.BodyCeiling(limit)
		}()
	}
}

// One byte is the smallest legal limit.
func TestBodyCeiling_LimitOneIsLegal(t *testing.T) {
	reached := false
	h := web.BodyCeiling(1)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	req, _ := ceilingPost(1, true)

	rec := serve(t, h, req)

	if !reached {
		t.Errorf("handler not reached; status = %d (body %q)", rec.Code, rec.Body.String())
	}
}
