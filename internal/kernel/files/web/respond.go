package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/db"
	platformweb "github.com/vardhan/internaltools/internal/platform/web"
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
	htmx := isHTMX(r)
	if r.Method == http.MethodGet && !htmx && !strings.HasPrefix(r.URL.Path, "/api/") {
		http.Redirect(w, r, signin, http.StatusSeeOther)
		return
	}
	if htmx {
		w.Header().Set("HX-Redirect", signin)
	}
	http.Error(w, msgExpired, http.StatusUnauthorized)
}

func isHTMX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

// refresh asks htmx to reload the page after a success (CD-241).
func refresh(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		w.Header().Set("HX-Refresh", "true")
	}
}

// refuse writes a refusal of msg with status: plain text, or for htmx an
// empty body, HX-Reswap none and an error notice (CD-241). Every refusal goes
// through it.
func refuse(w http.ResponseWriter, r *http.Request, msg string, status int) {
	if !isHTMX(r) {
		http.Error(w, msg, status)
		return
	}
	w.Header().Set("HX-Reswap", "none")
	platformweb.SetNotice(w.Header(), "error", msg)
	w.WriteHeader(status)
}

// fail writes err through refuse: a *files.Error or any doctype.HTTPError (CD-44)
// with its status and text, an over-long body as 413, db.ErrBusy as 503
// (CD-76), and anything else as 500 with the storage message, logged.
func (h *handlers) fail(w http.ResponseWriter, r *http.Request, err error) {
	var fe *files.Error
	var mb *http.MaxBytesError
	var he doctype.HTTPError
	switch {
	case errors.As(err, &fe):
		refuse(w, r, fe.Message, fe.Code)
	case errors.As(err, &mb):
		refuse(w, r, files.MsgTooLarge, http.StatusRequestEntityTooLarge)
	case errors.As(err, &he):
		refuse(w, r, he.Error(), he.Status())
	case errors.Is(err, db.ErrBusy):
		refuse(w, r, db.ErrBusy.Error(), http.StatusServiceUnavailable)
	default:
		h.cfg.Logger.ErrorContext(r.Context(), "files/web: request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		refuse(w, r, files.MsgStorage, http.StatusInternalServerError)
	}
}
