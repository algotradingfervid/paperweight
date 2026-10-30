package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgExpired    = "Your session has expired. Sign in again."
	msgNoFilePart = "Choose a file to upload."
)

// MsgTwoFileParts refuses an upload with more than one "file" part (CD-209).
const MsgTwoFileParts = "Upload one file at a time."

// signedOut answers a request with no principal like AUTH's middleware
// (CD-17, CD-63): a browser GET is sent to sign in; htmx, /api/* and any other
// method get 401 (htmx also gets HX-Redirect).
func signedOut(w http.ResponseWriter, r *http.Request) {
	signin := "/signin?next=" + url.QueryEscape(r.URL.RequestURI())
	htmx := r.Header.Get("HX-Request") == "true"
	if r.Method == http.MethodGet && !htmx && !strings.HasPrefix(r.URL.Path, "/api/") {
		http.Redirect(w, r, signin, http.StatusSeeOther)
		return
	}
	if htmx {
		w.Header().Set("HX-Redirect", signin)
	}
	http.Error(w, msgExpired, http.StatusUnauthorized)
}

// fail writes err as text: a *files.Error or any doctype.HTTPError (CD-44)
// with its status and text, an over-long body as 413, db.ErrBusy as 503
// (CD-76), and anything else as 500 with the storage message, logged.
func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	var fe *files.Error
	var mb *http.MaxBytesError
	var he doctype.HTTPError
	switch {
	case errors.As(err, &fe):
		http.Error(w, fe.Message, fe.Code)
	case errors.As(err, &mb):
		http.Error(w, files.MsgTooLarge, http.StatusRequestEntityTooLarge)
	case errors.As(err, &he):
		http.Error(w, he.Error(), he.Status())
	case errors.Is(err, db.ErrBusy):
		http.Error(w, db.ErrBusy.Error(), http.StatusServiceUnavailable)
	default:
		h.cfg.Logger.ErrorContext(r.Context(), "files/web: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		http.Error(w, files.MsgStorage, http.StatusInternalServerError)
	}
}
