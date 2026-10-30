// Package company is the Company master (one per install, MST-019) and its
// branches (MST-021…MST-027), and implements print.Letterhead (M11, CD-154).
package company

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company/dbq"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// CompanyDocType is the Company's registry name.
const CompanyDocType = "Company"

// CompanyKind is the Company master; its one row has id 1.
var CompanyKind = mstkit.Kind{DocType: CompanyDocType, Label: "Company", Table: "company"}

const (
	companyID      = 1
	msgSingleton   = "This install supports one company. Add a branch instead."
	msgPAN         = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."
	msgPANRequired = "PAN is required."
)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:         CompanyDocType,
		Label:        "Company",
		PluralLabel:  "Companies",
		Table:        "company",
		SearchFields: []string{"name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
		Fields: []doctype.Field{
			{Name: "name", Label: "Legal name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
			{Name: "pan", Label: "PAN", Column: "pan", Type: doctype.Text, Required: true, MaxLen: 10},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
		},
	}, mstkit.StandardLookup(CompanyKind))
}

// Company is the install's one company.
type Company struct {
	ID             int64
	LegalName, PAN string
	Version        int
}

// CompanyInput is what a company save posts.
type CompanyInput struct{ LegalName, PAN string }

// ValidateCompany trims the legal name and normalises the PAN, which is
// required (MST-020, CD-165).
func ValidateCompany(in CompanyInput) (CompanyInput, error) {
	var es mstkit.Errors
	name, err := mstkit.NormalizeName(in.LegalName)
	addErr(&es, err)
	pan, err := gstin.ValidatePAN(in.PAN)
	if err != nil {
		es.Add("pan", msgPAN)
	} else if pan == "" {
		es.Add("pan", msgPANRequired)
	}
	return CompanyInput{LegalName: name, PAN: pan}, es.Err()
}

// Service is the company and branch service.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

// Company returns the install's company; false when none is set up yet.
func (s *Service) Company(ctx context.Context, tx *db.Tx) (Company, bool, error) {
	row, err := dbq.New(tx.SQL()).GetCompany(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return Company{}, false, nil
	}
	if err != nil {
		return Company{}, false, err
	}
	return Company{ID: row.ID, LegalName: row.Name, PAN: row.Pan, Version: int(row.Version)}, true, nil
}

// SaveCompany wraps SaveCompanyTx in env.Write.
func (s *Service) SaveCompany(ctx context.Context, p auth.Principal, in CompanyInput, version int) error {
	return s.env.Write(ctx, p, CompanyKind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SaveCompanyTx(ctx, tx, p, in, version)
	})
}

// SaveCompanyTx creates the company (version 0) or updates it at the version
// the page posted. A create while a company exists is refused (MST-019).
func (s *Service) SaveCompanyTx(ctx context.Context, tx *db.Tx, p auth.Principal, in CompanyInput, version int) error {
	var rec *auth.RecordRef
	if version != 0 {
		rec = &auth.RecordRef{DocType: CompanyDocType, ID: companyID}
	}
	if err := s.env.Auth.Can(ctx, p, CompanyDocType, auth.RightWrite, rec); err != nil {
		return err
	}
	in, err := ValidateCompany(in)
	if err != nil {
		return err
	}
	cur, exists, err := s.Company(ctx, tx)
	if err != nil {
		return err
	}
	q := dbq.New(tx.SQL())
	now := types.TimestampOf(s.env.Clock.Now()).String()
	by := userID(p)
	after := map[string]string{"name": in.LegalName, "pan": in.PAN}
	if version == 0 {
		if exists {
			return refused(msgSingleton)
		}
		if err := q.InsertCompany(ctx, dbq.InsertCompanyParams{
			Name: in.LegalName, Pan: in.PAN, CreatedAt: now, CreatedBy: by, UpdatedAt: now, UpdatedBy: by,
		}); err != nil {
			return err
		}
		return s.env.Audit(ctx, tx, p, CompanyKind, companyID, "create", nil, after)
	}
	if err := mstkit.CheckVersion(ctx, tx, CompanyKind, companyID, version); err != nil {
		return err
	}
	if err := q.UpdateCompany(ctx, dbq.UpdateCompanyParams{
		Name: in.LegalName, Pan: in.PAN, UpdatedAt: now, UpdatedBy: by, Version: int64(version),
	}); err != nil {
		return err
	}
	return s.env.Audit(ctx, tx, p, CompanyKind, companyID, "save",
		map[string]string{"name": cur.LegalName, "pan": cur.PAN}, after)
}

// DeleteCompanyTx always refuses: the install keeps its one company
// (MST-019).
func (s *Service) DeleteCompanyTx(context.Context, *db.Tx, auth.Principal, int) error {
	return refused(msgSingleton)
}

// refused is a 422 refusal that belongs to no field.
func refused(msg string) error { return &mstkit.DeleteRefusedError{Message: msg} }

// addErr copies a *mstkit.ValidationError's field errors into es.
func addErr(es *mstkit.Errors, err error) {
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) {
		for _, f := range ve.Fields {
			es.Add(f.Field, f.Message)
		}
	}
}

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}
