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

// handoverPost hands the PO over (PO spec §21.2) from the posted version, to
// (a user id; absent is nobody picked) and note, and redirects to the PO
// page. A missing or non-numeric version is AUTH's bad form (CD-146). A 422
// or 409 redraws through renderHandover; anything else (AUTH's 403 and 404
// among them) is the platform's error page.
func (h *handlers) handoverPost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no PO has (404)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	to, _ := strconv.ParseInt(r.PostFormValue("to"), 10, 64) // absent or not a number: 0, nobody picked
	p, _ := auth.PrincipalFrom(r.Context())
	err = h.d.Service.HandOver(r.Context(), p, id, version, types.UserID(to), r.PostFormValue("note"))
	var he doctype.HTTPError
	switch {
	case err == nil:
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: err.Error()}}
		}
		renderHandover(w, r, he.Status(), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}
