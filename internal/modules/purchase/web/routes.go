package purchaseweb

import (
	"context"
	"net/http"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// Deps are what purchase's screens need; internal/app fills them.
type Deps struct {
	Service   *purchase.Service
	Approvals *approval.Approvals
	Comments  *modkit.Comments
	Sessions  *scs.SessionManager
	CSRF      func(*http.Request) string
	// ReceiveLink is MOD-GRN's Receive goods link for a PO, ok false when p
	// may not receive against it; nil until MOD-GRN wires it (CD-283).
	ReceiveLink func(ctx context.Context, p auth.Principal, poID int64) (href string, ok bool)
}

// handlers carries Deps to each task's handler file.
type handlers struct {
	d Deps //nolint:unused // read by the handlers later tasks build (P9 to P18)
}

// Routes registers every purchase module route.
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Get("/modules/purchase", h.home)
		r.Get("/modules/purchase/q/{queue}", h.queue)
		r.Get("/modules/purchase/orders/{id}/handover", h.handoverPage)
		r.Post("/modules/purchase/orders/{id}/handover", h.handoverPost)
		r.Get("/modules/purchase/orders/{id}/send", h.sendPage)
		r.Post("/modules/purchase/orders/{id}/send", h.sendPost)
		r.Post("/modules/purchase/orders/{id}/send/undo", h.sendUndo)
		r.Post("/modules/purchase/orders/{id}/copy", h.copyPost)
		r.Get("/modules/purchase/orders/{id}/expected", h.expectedPage)
		r.Post("/modules/purchase/orders/{id}/expected", h.expectedPost)
		r.Post("/modules/purchase/orders/{id}/expected/clear", h.expectedClear)
		r.Get("/modules/purchase/requests/{id}/order", h.fromRequestPage)
		r.Post("/modules/purchase/requests/{id}/order", h.fromRequestPost)
		r.Get("/reports/purchase/pending", h.pendingReport)
		r.Get("/reports/purchase/pending/filter", h.pendingFilter)
		r.Get("/reports/purchase/pending/export.csv", h.pendingExport)
	}
}

// plainErrors is the stub page redraw: status with the messages as plain
// text, one per line. The screen tasks replace the render functions with
// their pages (P15d, P15e).
func plainErrors(w http.ResponseWriter, status int, errs []doctype.FieldError) { //nolint:unused // the render stubs' default until P15d and P15e
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
	}
	http.Error(w, strings.Join(msgs, "\n"), status)
}
