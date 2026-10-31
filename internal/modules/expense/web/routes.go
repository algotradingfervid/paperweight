package expenseweb

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Deps are what the module's screens need; internal/app fills them.
type Deps struct {
	Service  *expense.Service
	Engine   doc.Engine
	Auth     auth.Service
	Files    *files.Store
	Employee Employees
}

// Employees reads employees for the claim's details step (S1): an
// Administrator's employee code, and the stored employee's code.
// *employee.Service satisfies it.
type Employees interface {
	ByCode(ctx context.Context, tx *db.Tx, code string) (employee.Employee, bool, error)
	Get(ctx context.Context, tx *db.Tx, id int64) (employee.Employee, error)
}

// handlers carries Deps to each task's handler file.
type handlers struct{ d Deps }

// Routes registers every module route (plan MOD-EXP E1 Do 5).
func Routes(d Deps) func(r chi.Router) {
	h := &handlers{d: d}
	return func(r chi.Router) {
		r.Post("/modules/expense/claims/{id}/receipt", h.receipt)
		for _, report := range []string{"open", "lines", "advances"} {
			r.Get("/reports/expense/"+report, h.report)
			r.Get("/reports/expense/"+report+"/filter", h.filterPage)
			r.Get("/reports/expense/"+report+"/export.csv", h.exportCSV)
		}
		r.Get("/modules/expense/advances/{id}/paid", h.paidPage)
		r.Post("/modules/expense/advances/{id}/paid", h.markPaid)
		r.Get("/modules/expense/advances/{id}/unpaid", h.unpaidPage)
		r.Post("/modules/expense/advances/{id}/unpaid", h.undoPaid)
	}
}

// guard is the right a route needs on its DocType, with no record.
type guard struct {
	docType string
	right   auth.Right
}

// guards are AUTH's guard of each route, by pattern. Record-level checks are
// the handlers': the CSV handlers check export (CD-145), and Mark as paid
// and Undo paid check cancel on the advance (spec §4).
var guards = map[string]guard{
	"/modules/expense/claims/{id}/receipt":  {expense.ClaimDocType, auth.RightWrite},
	"/reports/expense/open":                 {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/open/export.csv":      {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/open/filter":          {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/lines":                {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/lines/export.csv":     {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/lines/filter":         {expense.ClaimDocType, auth.RightRead},
	"/reports/expense/advances":             {expense.AdvanceDocType, auth.RightRead},
	"/reports/expense/advances/export.csv":  {expense.AdvanceDocType, auth.RightRead},
	"/reports/expense/advances/filter":      {expense.AdvanceDocType, auth.RightRead},
	"/modules/expense/advances/{id}/paid":   {expense.AdvanceDocType, auth.RightRead},
	"/modules/expense/advances/{id}/unpaid": {expense.AdvanceDocType, auth.RightRead},
}

// Guard is the DocType and right route needs; ok is false for a route this
// package doesn't register.
func Guard(route string) (docType string, right auth.Right, ok bool) {
	g, ok := guards[route]
	return g.docType, g.right, ok
}
