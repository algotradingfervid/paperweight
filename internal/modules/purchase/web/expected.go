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

// expectedPost sets the PO's Expected on (PO spec §27.1) from the posted
// version, expected_on (as typed) and note.
func (h *handlers) expectedPost(w http.ResponseWriter, r *http.Request) {
	h.expectedWrite(w, r, func(id int64, version int) error {
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.SetExpectedPosted(r.Context(), p, id, version, r.PostFormValue("expected_on"), r.PostFormValue("note"))
	}, func(display string) string { return fmt.Sprintf(msgExpectedSaved, display) })
}

// expectedClear clears the PO's Expected on at the posted version.
func (h *handlers) expectedClear(w http.ResponseWriter, r *http.Request) {
	h.expectedWrite(w, r, func(id int64, version int) error {
		p, _ := auth.PrincipalFrom(r.Context())
		return h.d.Service.ClearExpected(r.Context(), p, id, version)
	}, func(display string) string { return fmt.Sprintf(msgExpectedCleared, display) })
}

// expectedWrite reads the path id and the posted version (missing or not a
// number is AUTH's bad form, CD-146), runs write and redirects to the PO
// page with the flash flash(display) (UX §5.20). A 422 or 409 redraws
// through renderExpected; anything else (AUTH's 403 and 404 among them) is
// the platform's error page.
func (h *handlers) expectedWrite(w http.ResponseWriter, r *http.Request, write func(id int64, version int) error, flash func(display string) string) {
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
		if display, ok := h.expectedDisplay(r, id); ok {
			web.Flash(r.Context(), "success", flash(display))
		}
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		errs := he.FieldErrors()
		if len(errs) == 0 {
			errs = []doctype.FieldError{{Message: err.Error()}}
		}
		renderExpected(w, h.withHandlers(r), he.Status(), doc.IsStale(err), errs)
	default:
		web.ErrorFrom(w, r, err)
	}
}

// expectedDisplay is the PO's display for the flash, read after a write that
// passed the same checks; ok false (no flash) if it can't be read.
func (h *handlers) expectedDisplay(r *http.Request, id int64) (string, bool) {
	p, _ := auth.PrincipalFrom(r.Context())
	st, err := h.d.Service.ExpectedState(r.Context(), p, id)
	return st.Display, err == nil
}
