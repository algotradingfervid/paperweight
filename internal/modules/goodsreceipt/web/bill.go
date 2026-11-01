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
)

// billTitle is the Bill details card's title (spec §3.3).
const billTitle = "Bill details"

// saveBill is POST /modules/goodsreceipt/receipts/{id}/bill (plan G7 Do 4,
// UX §5.7): the posted version (missing or not a number is AUTH's bad form,
// CD-183) and the three fields, as typed, go to SaveBill. htmx gets the
// card, drawn as the page draws it (billViewOf): after a save the read
// view with the polite status (200, never a toast), after a 422 or 409 the
// form with the typed values and the errors. Without htmx it redirects to
// the GRN page at #bill, a 422 or 409 first put in the session for that
// page's card (before the response starts: scs saves at the first write),
// except on a draft, whose page has no card.
// Anything else (AUTH's 403 and 404 among them) is the platform's error
// answer.
func (h *handlers) saveBill(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no GRN has (404)
	version, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		web.ErrorFrom(w, r, auth.ErrBadForm)
		return
	}
	p, _ := auth.PrincipalFrom(r.Context())
	in := goodsreceipt.BillInput{InvoiceNo: r.PostFormValue("invoice_no"), InvoiceDate: r.PostFormValue("invoice_date"), InvoiceAmount: r.PostFormValue("invoice_amount")}
	err = h.d.Service.SaveBill(r.Context(), p, id, version, in)
	var (
		he     doctype.HTTPError
		kept   *billPost
		status = http.StatusOK
	)
	switch {
	case err == nil:
	case errors.As(err, &he) && (he.Status() == http.StatusUnprocessableEntity || he.Status() == http.StatusConflict):
		k := billPostOf(in, version, he)
		kept, status = &k, he.Status()
	default:
		web.ErrorFrom(w, r, err)
		return
	}
	var b goodsreceipt.BillCard
	if kept != nil || ui.IsHTMX(r) {
		if b, err = h.d.Service.BillCard(r.Context(), p, id); err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
	}
	if !ui.IsHTMX(r) {
		// A writer's draft redirects to its check page, which draws no card:
		// a kept post would wait there and show after approval.
		if kept != nil && b.State != doctype.StateDraft {
			h.d.Sessions.Put(r.Context(), billKey(id), *kept)
		}
		http.Redirect(w, r, docPath(id)+"#bill", http.StatusSeeOther)
		return
	}
	v, _ := billViewOf(id, b, kept, false, kept == nil) // always a card: after a save or with kept
	ui.Render(w, r, status, nil, billCard(v))
}
