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
)

// expectedPost sets the PO's Expected on (PO spec §27.1) from the posted
// version, expected_on (as typed) and note.
func (h *handlers) expectedPost(w http.ResponseWriter, r *http.Request) {
	h.expectedWrite(w, r, func(id int64, version int) error {
		on, err := purchase.ParseExpectedOn(r.PostFormValue("expected_on"))
		if err != nil {
			return err
		}
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.SetExpected(r.Context(), p, id, version, on, r.PostFormValue("note"))
	})
}

// expectedClear clears the PO's Expected on at the posted version.
func (h *handlers) expectedClear(w http.ResponseWriter, r *http.Request) {
	h.expectedWrite(w, r, func(id int64, version int) error {
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.ClearExpected(r.Context(), p, id, version)
	})
}

// expectedWrite reads the path id and the posted version (missing or not a
// number is AUTH's bad form, CD-146), runs write and redirects to the PO
// page. A 422 or 409 redraws through renderExpected; anything else (AUTH's
// 403 and 404 among them) is the platform's error page.
func (h *handlers) expectedWrite(w http.ResponseWriter, r *http.Request, write func(id int64, version int) error) {
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
		renderExpected(w, r, he.Status(), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}
