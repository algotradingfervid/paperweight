// Package taxcode is the GST tax code master (MST Task 9): a GST treatment
// and a notified rate, and the CGST/SGST/IGST rate split.
package taxcode

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Tax Code"

// Kind is the tax code master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Tax Code", Table: "tax_code", HasCode: true}

// Treatment is a GST treatment (D-MST-16).
type Treatment string

// The five treatments, in display order.
const (
	Taxable   Treatment = "Taxable"
	NilRated  Treatment = "Nil-Rated"
	Exempted  Treatment = "Exempted"
	NonGST    Treatment = "Non-GST"
	ZeroRated Treatment = "Zero-Rated"
)

// treatments is the five treatments in display order.
var treatments = []Treatment{Taxable, NilRated, Exempted, NonGST, ZeroRated}

const (
	msgTreatment   = "Treatment must be Taxable, Nil-Rated, Exempted, Non-GST or Zero-Rated."
	msgTaxableZero = "A Taxable tax code needs a GST rate above 0. Use Nil-Rated or Exempted for 0%."
	msgNotNotified = "GST rate %s%% is not a notified rate. Use one of 0.1, 0.25, 1, 1.5, 3, 5, 6, 7.5, 12, 18, 28, 40."

	msgDefaultDisabled = "Tax code %s is disabled. Pick another tax code."
	MsgDefaultNotFound = "Default tax code not found. Pick one from the list." // DOC-032's wording

	msgRateLocked      = "Rate can't change after this tax code is used. Disable it and create a new one."
	msgTreatmentLocked = "Treatment can't change after this tax code is used. Disable it and create a new one."
)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Tax Code",
		PluralLabel: "Tax Codes",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "treatment", Label: "Treatment", Column: "treatment", Type: doctype.Select, Required: true, Importable: true,
				Options: []string{string(Taxable), string(NilRated), string(Exempted), string(NonGST), string(ZeroRated)}},
			{Name: "rate", Label: "Rate", Column: "rate", Type: doctype.Percent, Importable: true, ListPriority: 2},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, ListPriority: 3},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}, {Field: "id"}},
	}, mstkit.StandardLookup(Kind))
}

// TaxCode is one tax code.
type TaxCode struct {
	ID         int64
	Code, Name string
	Treatment  Treatment
	Rate       types.Percent
	Disabled   bool
	Version    int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name string
	Treatment  Treatment
	Rate       types.Percent
}

// Snapshot is what a document line copies from its tax code.
type Snapshot struct {
	Code      string
	Treatment Treatment
	Rate      types.Percent
}

// Validate normalises code and name. An empty or unknown treatment is one
// error on treatment and no rate check (CD-166). A Taxable code needs a notified rate
// above 0 (MST-094, MST-096); every other treatment stores 0 (MST-095).
func Validate(in Input) (Input, error) {
	var fields []doctype.FieldError
	add := func(err error) {
		var ve *mstkit.ValidationError
		if errors.As(err, &ve) {
			fields = append(fields, ve.Fields...)
		}
	}
	code, err := mstkit.NormalizeCode(in.Code)
	add(err)
	name, err := mstkit.NormalizeName(in.Name)
	add(err)
	rate := in.Rate
	switch {
	case !slices.Contains(treatments, in.Treatment):
		fields = append(fields, doctype.FieldError{Field: "treatment", Message: msgTreatment})
	case in.Treatment != Taxable:
		rate = 0
	case rate == 0:
		fields = append(fields, doctype.FieldError{Field: "rate", Message: msgTaxableZero})
	case !slices.Contains(allowedRates, rate):
		fields = append(fields, doctype.FieldError{Field: "rate", Message: fmt.Sprintf(msgNotNotified, rate.Plain())})
	}
	if len(fields) > 0 {
		return Input{}, &mstkit.ValidationError{Fields: fields}
	}
	return Input{Code: code, Name: name, Treatment: in.Treatment, Rate: rate}, nil
}

// Service writes and reads tax codes.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

// Create adds a tax code.
func (s *Service) Create(ctx context.Context, p auth.Principal, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// CreateTx is Create inside tx.
func (s *Service) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.CanTx(ctx, tx, p, DocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertTaxCode(ctx, dbq.InsertTaxCodeParams{
		Code: in.Code, Name: in.Name, Treatment: string(in.Treatment), Rate: int64(in.Rate),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the tax code's code, name, treatment and rate.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx. The rate and treatment are locked once a
// document outside draft uses the tax code (MST-099, CD-199). The UPDATE is
// conditional on the posted version (CD-94).
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.CanTx(ctx, tx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	old, err := s.Get(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLocks(ctx, tx, old, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, tx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	before := auditFields(Input{Code: old.Code, Name: old.Name, Treatment: old.Treatment, Rate: old.Rate})
	if mstkit.Unchanged(before, auditFields(in)) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateTaxCode(ctx, dbq.UpdateTaxCodeParams{
		Code: in.Code, Name: in.Name, Treatment: string(in.Treatment), Rate: int64(in.Rate),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		// CheckVersion passed inside BEGIN IMMEDIATE, so this can't happen.
		return mstkit.Saved{}, fmt.Errorf("update tax code %d: no row changed", id)
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, auditFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1}, nil
}

// checkLocks refuses a changed rate or treatment on a tax code in use. The
// treatment is compared itself: Exempted to Nil-Rated keeps rate 0.
func (s *Service) checkLocks(ctx context.Context, tx *db.Tx, old TaxCode, in Input) error {
	rate, treatment := in.Rate != old.Rate, in.Treatment != old.Treatment
	if !rate && !treatment {
		return nil
	}
	_, used, err := s.env.UsedOutsideDrafts(ctx, tx, Kind.Table, old.ID)
	if err != nil || !used {
		return err
	}
	var es mstkit.Errors
	if rate {
		es.Add("rate", msgRateLocked)
	}
	if treatment {
		es.Add("treatment", msgTreatmentLocked)
	}
	return es.Err()
}

// SetDisabled disables or enables the tax code.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx.
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.CanTx(ctx, tx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// Delete deletes the tax code unless it is referenced.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// DeleteTx is Delete inside tx.
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.Auth.CanTx(ctx, tx, p, DocType, auth.RightDelete, ref(id)); err != nil {
		return err
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version)
}

// Get reads the tax code; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (TaxCode, error) {
	r, err := dbq.New(tx.SQL()).GetTaxCode(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return TaxCode{}, doctype.ErrNotFound
	}
	if err != nil {
		return TaxCode{}, err
	}
	return fromRow(dbq.GetTaxCodeByCodeRow(r)), nil
}

// ByCode reads the tax code with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (TaxCode, bool, error) {
	r, err := dbq.New(tx.SQL()).GetTaxCodeByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return TaxCode{}, false, nil
	}
	if err != nil {
		return TaxCode{}, false, err
	}
	return fromRow(r), true, nil
}

// CheckEnabledTx refuses a default_tax_code_id that doesn't exist or is
// disabled (MST-074, CD-170), reading inside tx (CD-94). 0 is none.
func (s *Service) CheckEnabledTx(ctx context.Context, tx *db.Tx, id int64) error {
	if id == 0 {
		return nil
	}
	var es mstkit.Errors
	tc, err := s.Get(ctx, tx, id)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("default_tax_code_id", MsgDefaultNotFound)
	case err != nil:
		return err
	case tc.Disabled:
		es.Add("default_tax_code_id", fmt.Sprintf(msgDefaultDisabled, tc.Code))
	}
	return es.Err()
}

// Snapshot is the code, treatment and rate a document line copies.
func (s *Service) Snapshot(ctx context.Context, tx *db.Tx, id int64) (Snapshot, error) {
	tc, err := s.Get(ctx, tx, id)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Code: tc.Code, Treatment: tc.Treatment, Rate: tc.Rate}, nil
}

func fromRow(r dbq.GetTaxCodeByCodeRow) TaxCode {
	return TaxCode{
		ID: r.ID, Code: r.Code, Name: r.Name, Treatment: Treatment(r.Treatment), Rate: types.Percent(r.Rate),
		Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

// auditFields is the tax code as audited.
func auditFields(in Input) map[string]string {
	return map[string]string{
		"code": in.Code, "name": in.Name, "treatment": string(in.Treatment), "rate": in.Rate.Plain(),
	}
}
