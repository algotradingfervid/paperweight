// Package allmasters wires the masters together for WP3: their services
// (this file) and their screens (routes.go, the only file here that imports
// the web/ subpackages, CD-89).
package allmasters

import (
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// Services is every master's service over one Env (CD-195).
type Services struct {
	Company     *company.Service
	Terms       *paymentterms.Service
	Party       *party.Service
	Address     *address.Service
	UoM         *uom.Service
	TaxCode     *taxcode.Service
	Item        *item.Service
	ExpenseType *expensetype.Service
	Project     *project.Service
	Site        *site.Service
	Employee    *employee.Service
	Directory   *employee.Directory
	Letterhead  company.Letterhead

	env mstkit.Env
}

// New builds the services.
func New(env mstkit.Env) *Services {
	s := &Services{
		Company:     company.New(env),
		Terms:       paymentterms.New(env),
		Party:       party.New(env),
		Address:     address.New(env),
		UoM:         uom.New(env),
		TaxCode:     taxcode.New(env),
		Item:        item.New(env),
		ExpenseType: expensetype.New(env),
		Project:     project.New(env),
		Site:        site.New(env),
		Employee:    employee.New(env),
		env:         env,
	}
	s.Directory = &employee.Directory{Employees: s.Employee, Projects: s.Project, Sites: s.Site}
	s.Letterhead = company.Letterhead{S: s.Company, AddressLines: s.Address.BranchAddressLines}
	return s
}
