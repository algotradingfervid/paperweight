package purchaseweb

import (
	"errors"
	"fmt"
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
// page with the flash `{display} is now {name}'s.` (UX §5.12). A missing
// or non-numeric version is AUTH's bad form (CD-146). A 422
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
	// The flash names the PO and the new owner from the page's state, read
	// first: after the hand-over the old owner may no longer read it. When it
	// can't be read (the POST then refuses too), there is no flash.
	st, _ := h.d.Service.HandOverState(r.Context(), p, id)
	err = h.d.Service.HandOver(r.Context(), p, id, version, types.UserID(to), r.PostFormValue("note"))
	var he doctype.HTTPError
	switch {
	case err == nil:
		for _, e := range st.Eligible {
			if e.ID == types.UserID(to) {
				web.Flash(r.Context(), "success", fmt.Sprintf(msgHandedOver, st.Display, e.Name))
			}
		}
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: err.Error()}}
		}
		renderHandover(w, h.withHandlers(r), he.Status(), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}
