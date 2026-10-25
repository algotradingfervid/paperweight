package web

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
)

// inlineMIME are the stored types a browser may show in place.
var inlineMIME = map[string]bool{
	"application/pdf": true,
	"image/jpeg":      true,
	"image/png":       true,
	"image/webp":      true,
}

// download serves one attachment's bytes with the stored type and safe
// headers; http.ServeContent handles Range and HEAD.
func (h *handlers) download(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		signedOut(w, r)
		return
	}
	id, ok := parseID(chi.URLParam(r, "id"))
	if !ok {
		notFound(w, r)
		return
	}
	rc, m, err := h.cfg.Store.Open(r.Context(), p, id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer func() { _ = rc.Close() }()
	rs, ok := rc.(io.ReadSeeker)
	if !ok {
		h.fail(w, r, errNotSeeker)
		return
	}
	kind := "attachment"
	if inlineMIME[m.MIME] && r.URL.Query().Get("download") != "1" {
		kind = "inline"
	}
	hd := w.Header()
	hd.Set("Content-Type", m.MIME)
	setSecurityHeaders(hd)
	hd.Set("Content-Disposition", contentDisposition(kind, m.Name))
	http.ServeContent(errorHeaders{w}, r, "", m.UploadedAt, rs)
}

// setSecurityHeaders sets the headers every download reply carries.
func setSecurityHeaders(hd http.Header) {
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	hd.Set("Cache-Control", "private, no-store")
}

// errorHeaders re-applies the security headers on ServeContent's own error
// replies (416, 412), whose serveError deletes Cache-Control.
type errorHeaders struct{ http.ResponseWriter }

func (e errorHeaders) WriteHeader(code int) {
	if code >= http.StatusBadRequest {
		setSecurityHeaders(e.Header())
	}
	e.ResponseWriter.WriteHeader(code)
}

// errNotSeeker means Store.Open returned a reader ServeContent can't use.
var errNotSeeker = errors.New("files/web: blob reader does not seek")

// notFound answers 404 "File not found.", also for paths no route matches.
func notFound(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, files.MsgNotFound, http.StatusNotFound)
}

// contentDisposition builds `{kind}; filename="{ascii}"`, where ascii replaces
// every non-ASCII or control rune and `"`, `\`, `;` with `_`, plus an RFC 5987
// filename* with the full name when ascii differs from it.
func contentDisposition(kind, name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r >= 0x7f || r == '"' || r == '\\' || r == ';' {
			return '_'
		}
		return r
	}, name)
	v := kind + `; filename="` + ascii + `"`
	if ascii != name {
		v += "; filename*=UTF-8''" + url.PathEscape(name)
	}
	return v
}
