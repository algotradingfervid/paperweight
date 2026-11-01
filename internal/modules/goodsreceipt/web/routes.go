package grnweb

import (
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// Deps are what the module's screens need; internal/app fills them.
// Sessions keep a bill details post refused without htmx for the document
// page's next load (UX doc §5.7).
type Deps struct {
	Service  *goodsreceipt.Service
	Sessions *scs.SessionManager
	Auth     auth.Service // S4: the report filters' master read checks
}

// handlers carries Deps to each task's handler file.
type handlers struct {
	d Deps
}

// Routes registers every module route (plan MOD-GRN G1 Do 4).
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Post("/modules/goodsreceipt/receipts/{id}/bill", h.saveBill)
		r.Get(poSearchURL, h.poSearch)
		r.Get("/reports/grn", h.reports)
		r.Get("/reports/grn/bills", h.report)
		r.Get("/reports/grn/bills/filter", h.filterPage)
		r.Get("/reports/grn/bills/export.csv", h.exportCSV("bills", h.billsRecords))
		r.Get("/reports/grn/items", h.report)
		r.Get("/reports/grn/items/filter", h.filterPage)
		r.Get("/reports/grn/items/export.csv", h.exportCSV("items", h.itemsRecords))
	}
}

// guard is the right a route needs on the GRN, with no record.
type guard struct {
	docType string
	right   auth.Right
}

// guards are AUTH's guard of each route, by pattern. Record-level checks are
// the handlers': the bill POST checks write on its GRN (G7), and the CSV
// handlers check export (CD-145). The PO search asks read, as step 1's list
// does no create check of its own; its rows are read-scoped on the PO.
var guards = map[string]guard{
	poSearchURL: {goodsreceipt.DocType, auth.RightRead},
	"/modules/goodsreceipt/receipts/{id}/bill": {goodsreceipt.DocType, auth.RightWrite},
	"/reports/grn":                  {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/bills":            {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/bills/filter":     {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/bills/export.csv": {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/items":            {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/items/filter":     {goodsreceipt.DocType, auth.RightRead},
	"/reports/grn/items/export.csv": {goodsreceipt.DocType, auth.RightRead},
}

// Guard is the DocType and right route needs; ok is false for a route this
// package doesn't register.
func Guard(route string) (docType string, right auth.Right, ok bool) {
	g, ok := guards[route]
	return g.docType, g.right, ok
}
