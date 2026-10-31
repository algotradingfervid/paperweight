package purchaseweb

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// sendPost marks the PO as sent (PO spec §23) from the posted version,
// sent_on, sent_via and note. A sent_on that doesn't parse, blank included,
// is passed as the zero date for MarkSent to refuse (AD-13).
func (h *handlers) sendPost(w http.ResponseWriter, r *http.Request) {
	h.sendWrite(w, r, func(id int64, version int) error {
		on, _ := types.ParseDate(r.PostFormValue("sent_on"))
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.MarkSent(r.Context(), p, id, version, purchase.SentInput{
			SentOn: on, SentVia: r.PostFormValue("sent_via"), Note: r.PostFormValue("note"),
		})
	})
}

// sendUndo marks the PO as not sent at the posted version.
func (h *handlers) sendUndo(w http.ResponseWriter, r *http.Request) {
	h.sendWrite(w, r, func(id int64, version int) error {
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.UnmarkSent(r.Context(), p, id, version)
	})
}

// sendWrite reads the path id and the posted version (missing or not a
// number is AUTH's bad form, CD-146), runs write and redirects to the PO
// page. A 422 or 409 redraws through renderSend; anything else (AUTH's 403
// and 404 among them) is the platform's error page.
func (h *handlers) sendWrite(w http.ResponseWriter, r *http.Request, write func(id int64, version int) error) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no PO has (404)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	err = write(id, version)
	var he doctype.HTTPError
	switch {
	case err == nil:
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: err.Error()}}
		}
		renderSend(w, r, he.Status(), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}
