package grnweb

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// Deps are what the module's screens need; internal/app fills them.
type Deps struct {
	Service *goodsreceipt.Service
}

// handlers carries Deps to each task's handler file.
type handlers struct {
	d Deps //nolint:unused // read by the handlers G7, G9 and S4 build
}

// Routes registers every module route (plan MOD-GRN G1 Do 4).
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Post("/modules/goodsreceipt/receipts/{id}/bill", h.saveBill)
		for _, report := range []string{"bills", "items"} {
			r.Get("/reports/grn/"+report, h.report)
			r.Get("/reports/grn/"+report+"/export.csv", h.exportCSV)
		}
	}
}

// guard is the right a route needs on the GRN, with no record.
type guard struct {
	docType string
	right   auth.Right
}

// guards are AUTH's guard of each route, by pattern. Record-level checks are
// the handlers': the bill POST checks write on its GRN (G7), and the CSV
// handlers check export (CD-145).
var guards = map[string]guard{
	"/modules/goodsreceipt/receipts/{id}/bill": {goodsreceipt.DocType, auth.RightWrite},
	"/reports/grn/bills":                       {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/bills/export.csv":            {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/items":                       {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/items/export.csv":            {goodsreceipt.DocType, auth.RightRead},
}

// Guard is the DocType and right route needs; ok is false for a route this
// package doesn't register.
func Guard(route string) (docType string, right auth.Right, ok bool) {
	g, ok := guards[route]
	return g.docType, g.right, ok
}

// msgNotBuilt answers a route whose owner hasn't built it yet.
const msgNotBuilt = "This page isn't built yet."

// notBuilt is every stub route's answer: 501, never success.
func notBuilt(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, msgNotBuilt, http.StatusNotImplemented)
}
