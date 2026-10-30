// Package expensetype is the expense type master (MST Task 11): a code and
// name with an optional default HSN/SAC and default tax code that expense
// claim lines start from.
package expensetype

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/expensetype/dbq"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Expense Type"

// Kind is the expense type master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Expense Type", Table: "expense_type", HasCode: true}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Expense Type",
		PluralLabel: "Expense Types",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "default_hsn_sac", Label: "Default HSN/SAC", Column: "default_hsn_sac", Type: doctype.Text, Importable: true, MaxLen: 8},
			{Name: "default_tax_code_id", Label: "Default tax code", Column: "default_tax_code_id", Type: doctype.Link, Link: taxcode.DocType, Importable: true},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, Filterable: true, ListPriority: 3},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
	}, mstkit.StandardLookup(Kind))
}

// ExpenseType is one expense type.
type ExpenseType struct {
	ID               int64
	Code, Name       string
	DefaultHSNSAC    string
	DefaultTaxCodeID int64
	Disabled         bool
	Version          int
}

// Input is what Create and Update accept. DefaultHSNSAC "" and
// DefaultTaxCodeID 0 are none.
type Input struct {
	Code, Name       string
	DefaultHSNSAC    string
	DefaultTaxCodeID int64
}

// Validate normalises code, name and the default HSN/SAC. A default starting
// 99 is a SAC (ValidateSAC); any other is a goods HSN (ValidateGoodsHSN,
// MST-122).
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
	hsnSAC := strings.TrimSpace(in.DefaultHSNSAC)
	if hsnSAC != "" {
		check := gstin.ValidateGoodsHSN
		if strings.HasPrefix(hsnSAC, "99") {
			check = gstin.ValidateSAC
		}
		if _, err := check(hsnSAC); err != nil {
			fields = append(fields, doctype.FieldError{Field: "default_hsn_sac", Message: err.Error()})
		}
	}
	if len(fields) > 0 {
		return Input{}, &mstkit.ValidationError{Fields: fields}
	}
	return Input{Code: code, Name: name, DefaultHSNSAC: hsnSAC, DefaultTaxCodeID: in.DefaultTaxCodeID}, nil
}

// Service writes and reads expense types.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Create adds an expense type.
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
	if err := taxcode.New(s.env).CheckEnabledTx(ctx, tx, in.DefaultTaxCodeID); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertExpenseType(ctx, dbq.InsertExpenseTypeParams{
		Code: in.Code, Name: in.Name, DefaultHsnSac: in.DefaultHSNSAC, DefaultTaxCodeID: nullID(in.DefaultTaxCodeID),
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

// Update replaces the expense type's code, name and defaults.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx. The UPDATE is conditional on the posted
// version (CD-94).
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
	if err := taxcode.New(s.env).CheckEnabledTx(ctx, tx, in.DefaultTaxCodeID); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, tx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	before := auditFields(Input{Code: old.Code, Name: old.Name, DefaultHSNSAC: old.DefaultHSNSAC, DefaultTaxCodeID: old.DefaultTaxCodeID})
	if mstkit.Unchanged(before, auditFields(in)) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateExpenseType(ctx, dbq.UpdateExpenseTypeParams{
		Code: in.Code, Name: in.Name, DefaultHsnSac: in.DefaultHSNSAC, DefaultTaxCodeID: nullID(in.DefaultTaxCodeID),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		// CheckVersion passed inside BEGIN IMMEDIATE, so this can't happen.
		return mstkit.Saved{}, fmt.Errorf("update expense type %d: no row changed", id)
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, auditFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1}, nil
}

// SetDisabled disables or enables the expense type.
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

// Delete deletes the expense type unless it is referenced.
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

// Get reads the expense type; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (ExpenseType, error) {
	r, err := dbq.New(tx.SQL()).GetExpenseType(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return ExpenseType{}, doctype.ErrNotFound
	}
	if err != nil {
		return ExpenseType{}, err
	}
	return fromRow(dbq.GetExpenseTypeByCodeRow(r)), nil
}

// ByCode reads the expense type with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (ExpenseType, bool, error) {
	r, err := dbq.New(tx.SQL()).GetExpenseTypeByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return ExpenseType{}, false, nil
	}
	if err != nil {
		return ExpenseType{}, false, err
	}
	return fromRow(r), true, nil
}

// Defaults is the default HSN/SAC and tax code id an expense line starts
// from ("" and 0 when none). A disabled tax code is no default (CD-180): it
// reads as 0 while the expense type keeps it stored.
func (s *Service) Defaults(ctx context.Context, tx *db.Tx, id int64) (hsnSAC string, taxCodeID int64, err error) {
	et, err := s.Get(ctx, tx, id)
	if err != nil {
		return "", 0, err
	}
	if et.DefaultTaxCodeID == 0 {
		return et.DefaultHSNSAC, 0, nil
	}
	tc, err := taxcode.New(s.env).Get(ctx, tx, et.DefaultTaxCodeID)
	if err != nil {
		return "", 0, err
	}
	if tc.Disabled {
		return et.DefaultHSNSAC, 0, nil
	}
	return et.DefaultHSNSAC, tc.ID, nil
}

func fromRow(r dbq.GetExpenseTypeByCodeRow) ExpenseType {
	return ExpenseType{
		ID: r.ID, Code: r.Code, Name: r.Name, DefaultHSNSAC: r.DefaultHsnSac, DefaultTaxCodeID: r.DefaultTaxCodeID.Int64,
		Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

// auditFields is the expense type as audited.
func auditFields(in Input) map[string]string {
	taxCode := ""
	if in.DefaultTaxCodeID != 0 {
		taxCode = strconv.FormatInt(in.DefaultTaxCodeID, 10)
	}
	return map[string]string{
		"code": in.Code, "name": in.Name, "default_hsn_sac": in.DefaultHSNSAC, "default_tax_code_id": taxCode,
	}
}
