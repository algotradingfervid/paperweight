// Package companyweb is the Company and Branch screens (MST Task 22, CD-89,
// CD-201): the one company under /m/company, which has no new, disable or
// delete (MST-019), the branches under /m/branch with their picker at
// /pick/Branch, and the company logo (logo.go, CD-140). The screens themselves
// are mstweb's.
package companyweb

import (
	"context"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// companyID is the id of the install's one company row.
const companyID = 1

// Routes mounts the Company screens under /m/company and the Branch screens
// under /m/branch, with their pickers at /pick/{DocType}.
func Routes(svc *company.Service, env mstkit.Env) func(chi.Router) {
	companies := mstweb.Screen[company.Company, company.CompanyInput]{
		DocType: company.CompanyDocType,
		Base:    "/m/company",
		Copy: mstweb.Copy{
			Plural: "Company", Noun: "companies", One: "company", Search: "Search companies", Empty: "No company yet.",
			Saved: "Company saved.",
		},
		Svc:      companyService{svc: svc},
		Env:      env,
		ToInput:  companyInput,
		Validate: company.ValidateCompany,
		Values:   companyValues,
		Head:     companyHead,
		Ref:      companyRef,
		Codes:    map[string]components.CodeKind{"pan": components.PAN},
		NoNew:    true, NoDisable: true, NoDelete: true,
		Extra: logoLink(env),
	}.Routes()
	branches := mstweb.Screen[company.Branch, company.BranchInput]{
		DocType: company.DocType,
		Base:    "/m/branch",
		Copy: mstweb.Copy{
			Plural: "Branches", Noun: "branches", One: "branch", New: "New branch", Search: "Search branches", Empty: "No branches yet.",
			Saved: "Branch saved.", Disabled: "Branch disabled.", Enabled: "Branch enabled.", Deleted: "Branch deleted.",
		},
		Svc:     svc,
		Env:     env,
		ToInput: branchInput,
		Values:  branchValues,
		Head:    branchHead,
		Ref:     branchRef,
		Codes:   map[string]components.CodeKind{"gstin": components.GSTIN},
	}.Routes()
	return func(r chi.Router) {
		companies(r)
		branches(r)
	}
}

// companyService is the Company's service as a master's standard surface
// (CD-201). The embedded interface stays nil: NoNew, NoDisable and NoDelete
// leave out the only routes that call Create, SetDisabled and Delete.
type companyService struct {
	mstweb.Service[company.Company, company.CompanyInput]
	svc *company.Service
}

// Get is the one company; every other id is not found, so /m/company/2 can't
// show it.
func (s companyService) Get(ctx context.Context, tx *db.Tx, id int64) (company.Company, error) {
	c, ok, err := s.svc.Company(ctx, tx)
	if err != nil {
		return company.Company{}, err
	}
	if !ok || id != c.ID {
		return company.Company{}, doctype.ErrNotFound
	}
	return c, nil
}

// Update saves the company at the version the page posted. Version 0 is
// SaveCompany's create path, which no page posts: below 1 goes in as -1, which
// is stale like any version that isn't the company's (not found before setup).
func (s companyService) Update(ctx context.Context, p auth.Principal, id int64, version int, in company.CompanyInput) (mstkit.Saved, error) {
	if id != companyID {
		return mstkit.Saved{}, doctype.ErrNotFound
	}
	if version < 1 {
		version = -1
	}
	return mstkit.Saved{ID: id}, s.svc.SaveCompany(ctx, p, in, version)
}

// companyInput is the form's values as the service input. A company has only
// text, so nothing can fail to parse.
func companyInput(v map[string]string) (company.CompanyInput, []doctype.FieldError) {
	return company.CompanyInput{LegalName: v["name"], PAN: v["pan"]}, nil
}

func companyValues(c company.Company) map[string]string {
	return map[string]string{"name": c.LegalName, "pan": c.PAN}
}

func companyHead(c company.Company) mstweb.Head {
	return mstweb.Head{Name: c.LegalName, Version: c.Version}
}

// companyRef is the record AUTH checks, as company.Service builds it.
func companyRef(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: company.CompanyDocType, ID: id}
}

// branchInput is the form's values as the service input. A branch has only
// text and a checkbox, so nothing can fail to parse.
func branchInput(v map[string]string) (company.BranchInput, []doctype.FieldError) {
	return company.BranchInput{
		Code: v["code"], Name: v["name"], StateCode: v["state_code"], GSTIN: v["gstin"], IsHeadOffice: v["is_head_office"] != "",
	}, nil
}

// branchValues is a stored branch as the form's values: Check fields "1" or
// "0".
func branchValues(b company.Branch) map[string]string {
	return map[string]string{
		"code": b.Code, "name": b.Name, "state_code": b.StateCode, "gstin": b.GSTIN, "is_head_office": check(b.IsHeadOffice),
	}
}

func branchHead(b company.Branch) mstweb.Head {
	return mstweb.Head{Code: b.Code, Name: b.Name, Disabled: b.Disabled, Version: b.Version}
}

// branchRef is the record AUTH checks, as company.Service builds it.
func branchRef(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: company.DocType, ID: id} }

func check(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
