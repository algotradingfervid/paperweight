package purchaseweb

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// fromRequestPost creates a PO draft from the request in the path (PO spec
// §20.4 item 1) with the posted version (missing or not a number is AUTH's
// bad form, CD-146) and vendor_id (blank or not a number is no vendor), then
// redirects to the new PO's Details step with the created flash (UX §5.18).
// A 422 or 409 redraws through renderFromRequest; anything else (AUTH's 403
// and 404 among them) is the platform's error page.
func (h *handlers) fromRequestPost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no request has (404)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	vendorID, _ := strconv.ParseInt(r.PostFormValue("vendor_id"), 10, 64)
	p, _ := auth.PrincipalFrom(r.Context())
	poID, source, err := h.d.Service.CreateFromRequestWithSource(r.Context(), p, id, version, vendorID)
	var he doctype.HTTPError
	switch {
	case err == nil:
		web.Flash(r.Context(), "success", fmt.Sprintf(msgCreatedFromRequest, poID, source))
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(poID, 10)+"/edit/details", http.StatusSeeOther)
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: err.Error()}}
		}
		renderFromRequest(h, w, r, he.Status(), doc.IsStale(err), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}
