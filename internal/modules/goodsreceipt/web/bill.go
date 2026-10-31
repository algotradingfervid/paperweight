package grnweb

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// billTitle is the Bill details card's title (spec §3.3).
const billTitle = "Bill details"

// saveBill is POST /modules/goodsreceipt/receipts/{id}/bill (plan G7 Do 4,
// UX §5.7): the posted version (missing or not a number is AUTH's bad form,
// CD-183) and the three fields, as typed, go to SaveBill. A save answers
// htmx with the card holding its warnings (200, never a toast) and a no-JS
// post with a redirect to the GRN page at #bill. A 422 or 409 is the card
// with the errors; anything else (AUTH's 403 and 404 among them) is the
// platform's error answer.
func (h *handlers) saveBill(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no GRN has (404)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	warnings, err := h.d.Service.SaveBill(r.Context(), p, id, version, goodsreceipt.BillInput{
		InvoiceNo: r.PostFormValue("invoice_no"), InvoiceDate: r.PostFormValue("invoice_date"), InvoiceAmount: r.PostFormValue("invoice_amount"),
	})
	var he doctype.HTTPError
	switch {
	case err == nil && !ui.IsHTMX(r):
		http.Redirect(w, r, "/doc/"+goodsreceipt.DocType+"/"+strconv.FormatInt(id, 10)+"#bill", http.StatusSeeOther)
	case err == nil:
		ui.Render(w, r, http.StatusOK, nil, billCard(warnings, nil, ""))
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		var errs []components.FieldErr
		for _, f := range he.FieldErrors() {
			errs = append(errs, components.FieldErr{ID: f.Field, Message: f.Message})
		}
		msg := err.Error()
		if len(errs) > 0 {
			msg = ""
		}
		card := billCard(nil, errs, msg)
		ui.Render(w, r, he.Status(), card, card)
	default:
		web.ErrorFrom(w, r, err)
	}
}
