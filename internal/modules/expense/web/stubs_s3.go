package expenseweb

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// S3 (Pages) replaces this file.

// paidPage is the Mark as paid form.
func (h *handlers) paidPage(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}

// unpaidPage is the Undo paid confirmation.
func (h *handlers) unpaidPage(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "This page isn't built yet.", http.StatusNotImplemented)
}

// paidView is what E9's POST handlers pass to renderPaidPage: the advance and
// the values typed, so a redraw keeps them. S3 keeps the type.
//
//nolint:unused // E9's handlers build it
type paidView struct {
	AdvanceID int64
	Version   int
	PaidOn    string // as typed; blank on Undo paid
}

// renderPaidPage redraws the Mark as paid form or the Undo paid page with err
// (a 422 or 409). Until S3 builds the pages it answers through web.ErrorFrom:
// an error with a status gives that status and its message, anything else
// the plain internal error.
//
//nolint:unused // E9's POST handlers call it
func renderPaidPage(w http.ResponseWriter, r *http.Request, _ paidView, err error) {
	web.ErrorFrom(w, r, err)
}
