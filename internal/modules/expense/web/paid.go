package expenseweb

import (
	"errors"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// msgPaidOnRequired is DOC's required text for a blank Paid on.
const msgPaidOnRequired = "Paid on is required."

// markPaid posts Mark as paid: version and paid_on (dd-mm-yyyy). Success goes
// back to the advance page; a 422 or 409 redraws the form through
// renderPaidPage with the typed date kept; anything else is its own status.
func (h *handlers) markPaid(w http.ResponseWriter, r *http.Request) {
	v, ok := paidForm(w, r)
	if !ok {
		return
	}
	v.PaidOn = r.PostFormValue("paid_on")
	on, err := types.ParseDate(v.PaidOn)
	if err != nil {
		h.renderPaidPage(w, r, v, paidOnError(err))
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	h.paidDone(w, r, v, h.d.Service.MarkPaid(r.Context(), p, v.AdvanceID, v.Version, on))
}

// undoPaid posts Undo paid: version only. Answers as markPaid.
func (h *handlers) undoPaid(w http.ResponseWriter, r *http.Request) {
	v, ok := paidForm(w, r)
	if !ok {
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	h.paidDone(w, r, v, h.d.Service.UndoPaid(r.Context(), p, v.AdvanceID, v.Version))
}

// paidForm reads the advance id (a bad one is AUTH's 404, CD-245) and the
// posted version (missing or not a number is 400, CD-183). ok is false when
// it has answered.
func paidForm(w http.ResponseWriter, r *http.Request) (paidView, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrNotFound)
		return paidView{}, false
	}
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return paidView{}, false
	}
	return paidView{AdvanceID: id, Version: version}, true
}

// paidFlash is the flash after each post, by route (UX §5.18).
var paidFlash = map[string]string{"paid": "Marked as paid.", "unpaid": "No longer marked as paid."}

// paidDone flashes and redirects to the advance page on success, redraws a 422 or 409
// and answers anything else with its own status.
func (h *handlers) paidDone(w http.ResponseWriter, r *http.Request, v paidView, err error) {
	var se interface{ Status() int }
	switch {
	case err == nil:
		web.Flash(r.Context(), "success", paidFlash[path.Base(r.URL.Path)])
		http.Redirect(w, r, "/doc/"+expense.AdvanceDocType+"/"+strconv.FormatInt(v.AdvanceID, 10), http.StatusSeeOther)
	case errors.As(err, &se) && (se.Status() == http.StatusUnprocessableEntity || se.Status() == http.StatusConflict):
		h.renderPaidPage(w, r, v, err)
	default:
		web.ErrorFrom(w, r, err)
	}
}

// paidOnError is the 422 for a Paid on that doesn't parse, at the field.
func paidOnError(err error) error {
	msg := err.Error()
	if errors.Is(err, types.ErrEmpty) {
		msg = msgPaidOnRequired
	}
	return &doc.Error{Kind: doc.KindInvalid, Message: msg, Fields: []doc.FieldError{{Field: "paid_on", Message: msg}}}
}
