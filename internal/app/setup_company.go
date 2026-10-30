package app

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const (
	companyPath         = "/setup/company"
	msgCompanyAdminOnly = "Only an administrator can set up the company."
	// headOffice is what SetupTx names a head office left blank (CD-165).
	headOffice = "Head Office"
)

// The form's names for the fields the two services report under their own.
var (
	branchFields  = map[string]string{"code": "branch_code", "name": "branch_name"}
	companyFields = map[string]string{"name": "legal_name"}
)

// companyView is the form: the typed values by field name and the refusals.
type companyView struct {
	Values map[string]string
	Fields map[string]string // the error on each field
	Errors []components.FieldErr
}

func (v companyView) field(name, label, hint string, optional bool) components.FieldProps {
	return components.FieldProps{
		ID: components.FieldID(name), Name: name, Label: label, Hint: hint, Optional: optional,
		Value: v.Values[name], Error: v.Fields[name],
	}
}

// companyExists reports whether first-run setup has saved the company.
func (a *App) companyExists(ctx context.Context) (bool, error) {
	var exists bool
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		var err error
		_, exists, err = a.masters.Company.Company(ctx, tx)
		return err
	})
	return exists, err
}

// companyGet is the second first-run step (CD-231): the form, while no company
// exists.
func (a *App) companyGet(w http.ResponseWriter, r *http.Request) {
	exists, err := a.companyExists(r.Context())
	if err == nil && exists {
		err = auth.ErrNotFound
	}
	if err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, companyPage(companyView{}), nil)
}

// companyPost saves the company and its head office (CD-165). The check that
// none exists runs inside the write, so two administrators can't both save.
func (a *App) companyPost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	v := companyView{Values: map[string]string{}, Fields: map[string]string{}}
	for _, name := range []string{"legal_name", "state_code", "gstin", "pan", "branch_code", "branch_name"} {
		v.Values[name] = r.PostFormValue(name)
	}
	in := company.SetupInput{
		LegalName: v.Values["legal_name"], StateCode: v.Values["state_code"], GSTIN: v.Values["gstin"],
		PAN: v.Values["pan"], BranchCode: v.Values["branch_code"], BranchName: v.Values["branch_name"],
	}
	// SetupTx reports "name" for the branch and for the company, so the branch
	// is checked on its own first: a "name" from SetupTx is then the legal name.
	names := companyFields
	err := a.db.Write(ctx, func(tx *db.Tx) error {
		_, exists, err := a.masters.Company.Company(ctx, tx)
		if err != nil {
			return err
		}
		if exists {
			return auth.ErrNotFound
		}
		if err := checkBranch(in); err != nil {
			names = branchFields
			return err
		}
		return a.masters.Company.SetupTx(ctx, tx, p, in)
	})
	var refused doctype.HTTPError
	if errors.As(err, &refused) && len(refused.FieldErrors()) > 0 {
		for _, f := range refused.FieldErrors() {
			name := f.Field
			if form, ok := names[name]; ok {
				name = form
			}
			v.Fields[name] = f.Message
			v.Errors = append(v.Errors, components.FieldErr{ID: components.FieldID(name), Message: f.Message})
		}
		ui.Render(w, r, http.StatusUnprocessableEntity, companyPage(v), nil)
		return
	}
	if err != nil {
		a.srv.WriteError(w, r, err)
		return
	}
	http.Redirect(w, r, homePath, http.StatusSeeOther)
}

// checkBranch is SetupTx's branch check on the same input.
func checkBranch(in company.SetupInput) error {
	name := in.BranchName
	if strings.TrimSpace(name) == "" {
		name = headOffice
	}
	_, err := company.ValidateBranch(company.BranchInput{
		Code: in.BranchCode, Name: name, StateCode: in.StateCode, GSTIN: in.GSTIN, IsHeadOffice: true,
	}, "")
	return err
}
