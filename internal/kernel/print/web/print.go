package web

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
	platformweb "github.com/vardhan/internaltools/internal/platform/web"
)

const msgExpired = "Your session has expired. Sign in again."

// brandCSS is the print brand stylesheet served at /print/assets/brand.css (CD-99).
var brandCSS = brand.PrintCSS(brand.Load())

// css serves body as a stylesheet.
func css(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write([]byte(body))
	}
}

// print renders one document's print page inside DB.Read only (FILE-062).
// The page's watermark, badge and number come from Info alone; query
// parameters are never read (FILE-055).
func (h *handlers) print(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.PrincipalFrom(r.Context())
	if !ok {
		signedOut(w, r)
		return
	}
	dt := chi.URLParam(r, "doctype")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, doctype.ErrNotFound.Error(), http.StatusNotFound)
		return
	}
	var buf bytes.Buffer
	var info doctype.Info
	err = h.cfg.DB.Read(r.Context(), func(tx *db.Tx) error {
		var err error
		info, err = h.cfg.Info.Info(r.Context(), tx, dt, id)
		if err != nil {
			return err
		}
		ref := auth.RecordRef{
			DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
			EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
		}
		if err := h.cfg.Auth.Can(r.Context(), p, dt, "print", &ref); err != nil {
			return err
		}
		ctx := print.WithLetterhead(r.Context(), h.cfg.Letterhead)
		return h.cfg.Registry.Render(ctx, tx, &buf, info)
	})
	if err != nil {
		fail(w, r, info, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = buf.WriteTo(w)
}

// fail writes err: not found (CD-54) and no template as 404, forbidden as 403
// with the DocType label (CD-73), and anything else through the shared error
// writer (CD-44, CD-76).
func fail(w http.ResponseWriter, r *http.Request, info doctype.Info, err error) {
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		http.Error(w, doctype.ErrNotFound.Error(), http.StatusNotFound)
	case errors.Is(err, auth.ErrForbidden):
		http.Error(w, "You don't have permission to print "+info.DocLabel+". Ask your administrator for a role that allows it.", http.StatusForbidden)
	case errors.Is(err, print.ErrNoTemplate):
		http.Error(w, "No print template for DocType "+info.DocType+".", http.StatusNotFound)
	default:
		platformweb.ErrorFrom(w, r, err)
	}
}

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
