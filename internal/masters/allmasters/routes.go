package allmasters

import (
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/files"
	mastersweb "github.com/vardhan/internaltools/internal/masters/allmasters/web"
	expensetypeweb "github.com/vardhan/internaltools/internal/masters/expensetype/web"
	taxcodeweb "github.com/vardhan/internaltools/internal/masters/taxcode/web"
	uomweb "github.com/vardhan/internaltools/internal/masters/uom/web"
)

// WebDeps is what the screens need besides the services (CD-206, CD-209):
// AUTH's session manager and the real file store, because masters need Spool
// and the logo reads through the caller's tx.
type WebDeps struct {
	Sessions *scs.SessionManager
	Files    *files.Store
}

// Routes mounts every master's screens under /m/{doctype-slug}/, their
// pickers at /pick/{DocType} and the CSV import wizard at /m/import (MST Task
// 22, CD-195, CD-206); WP3 mounts it behind AUTH's middleware. Each master
// adds one line.
func (s *Services) Routes(r chi.Router, d WebDeps) {
	uomweb.Routes(s.UoM, s.env)(r)
	taxcodeweb.Routes(s.TaxCode, s.env)(r)
	expensetypeweb.Routes(s.ExpenseType, s.env)(r)
	mastersweb.Routes(s.env, d.Sessions)(r)
}
