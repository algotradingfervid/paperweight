package allmasters

import (
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/files"
	mastersweb "github.com/vardhan/internaltools/internal/masters/allmasters/web"
	companyweb "github.com/vardhan/internaltools/internal/masters/company/web"
	employeeweb "github.com/vardhan/internaltools/internal/masters/employee/web"
	expensetypeweb "github.com/vardhan/internaltools/internal/masters/expensetype/web"
	itemweb "github.com/vardhan/internaltools/internal/masters/item/web"
	partyweb "github.com/vardhan/internaltools/internal/masters/party/web"
	paymenttermsweb "github.com/vardhan/internaltools/internal/masters/paymentterms/web"
	projectweb "github.com/vardhan/internaltools/internal/masters/project/web"
	siteweb "github.com/vardhan/internaltools/internal/masters/site/web"
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
// 22, CD-195, CD-206); WP3 mounts it behind AUTH's middleware. It registers
// the importers too (CD-210). Each master adds one line.
func (s *Services) Routes(r chi.Router, d WebDeps) {
	RegisterImporters(s)
	companyweb.Routes(s.Company, s.env)(r)
	companyweb.LogoRoutes(s.Company, s.env, d.Files)(r)
	paymenttermsweb.Routes(s.Terms, s.env)(r)
	uomweb.Routes(s.UoM, s.env)(r)
	taxcodeweb.Routes(s.TaxCode, s.env)(r)
	itemweb.Routes(s.Item, s.env)(r)
	expensetypeweb.Routes(s.ExpenseType, s.env)(r)
	projectweb.Routes(s.Project, s.env)(r)
	siteweb.Routes(s.Site, s.env)(r)
	employeeweb.Routes(s.Employee, s.env, d.Sessions)(r)
	partyweb.Routes(s.Party, s.Address, s.env, d.Sessions)(r)
	mastersweb.Routes(s.env, d.Sessions)(r)
}
