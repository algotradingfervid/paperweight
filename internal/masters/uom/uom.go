// Package uom is the units-of-measure master (MST Task 8) and the pure
// conversion and whole-number maths that item and documents use (M4).
package uom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "UoM"

const msgFractionalUse = "%s is used with fractional quantities (e.g. %s). Create a new UoM instead."

// Kind is the UoM master.
var Kind = mstkit.Kind{DocType: DocType, Label: "UoM", Table: "uom", HasCode: true}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "UoM",
		PluralLabel: "UoMs",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "whole_number", Label: "Whole number", Column: "whole_number", Type: doctype.Check, Importable: true},
			{Name: "uqc", Label: "UQC", Column: "uqc", Type: doctype.Text, Importable: true},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, ListPriority: 3},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}, {Field: "id"}},
	}, mstkit.StandardLookup(Kind))
}

// UoM is one unit of measure.
type UoM struct {
	ID          int64
	Code, Name  string
	UQC         string
	WholeNumber bool
	Disabled    bool
	Version     int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name  string
	UQC         string
	WholeNumber bool
}

// Validate normalises code, name and UQC (trimmed, upper-cased).
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
	if len(fields) > 0 {
		return Input{}, &mstkit.ValidationError{Fields: fields}
	}
	return Input{Code: code, Name: name, UQC: strings.ToUpper(strings.TrimSpace(in.UQC)), WholeNumber: in.WholeNumber}, nil
}

// Service writes and reads UoMs.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// Create adds a UoM.
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
	id, err := dbq.New(tx.SQL()).InsertUoM(ctx, dbq.InsertUoMParams{
		Code: in.Code, Name: in.Name, Uqc: in.UQC, WholeNumber: flag(in.WholeNumber),
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

// Update replaces the UoM's code, name, UQC and whole-number flag.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx. The whole-number flag can't be turned on while
// a document outside draft holds a fractional quantity in this UoM (MST-092).
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightWrite, ref(id)); err != nil {
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
	if err := s.env.CheckCodeChange(ctx, tx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	if err := checkWholeFlag(ctx, tx, old, in); err != nil {
		return mstkit.Saved{}, err
	}
	before := auditFields(Input{Code: old.Code, Name: old.Name, UQC: old.UQC, WholeNumber: old.WholeNumber})
	if mstkit.Unchanged(before, auditFields(in)) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	if err := dbq.New(tx.SQL()).UpdateUoM(ctx, dbq.UpdateUoMParams{
		Code: in.Code, Name: in.Name, Uqc: in.UQC, WholeNumber: flag(in.WholeNumber),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	}); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, auditFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1}, nil
}

// checkWholeFlag refuses turning the whole-number flag on over fractional
// quantities (MST-092).
func checkWholeFlag(ctx context.Context, tx *db.Tx, old UoM, in Input) error {
	if !in.WholeNumber || old.WholeNumber {
		return nil
	}
	label, used, err := fractionalUse(ctx, tx, old.ID)
	if err != nil || !used {
		return err
	}
	var es mstkit.Errors
	es.Add("whole_number", fmt.Sprintf(msgFractionalUse, old.Code, label))
	return es.Err()
}

// SetDisabled disables or enables the UoM.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx.
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// Delete deletes the UoM unless it is referenced.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// DeleteTx is Delete inside tx.
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightDelete, ref(id)); err != nil {
		return err
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version)
}

// Get reads the UoM; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (UoM, error) {
	r, err := dbq.New(tx.SQL()).GetUoM(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return UoM{}, doctype.ErrNotFound
	}
	if err != nil {
		return UoM{}, err
	}
	return fromRow(dbq.GetUoMByCodeRow(r)), nil
}

// ByCode reads the UoM with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (UoM, bool, error) {
	r, err := dbq.New(tx.SQL()).GetUoMByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return UoM{}, false, nil
	}
	if err != nil {
		return UoM{}, false, err
	}
	return fromRow(r), true, nil
}

func fromRow(r dbq.GetUoMByCodeRow) UoM {
	return UoM{
		ID: r.ID, Code: r.Code, Name: r.Name, UQC: r.Uqc,
		WholeNumber: r.WholeNumber != 0, Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

// auditFields is the UoM as audited.
func auditFields(in Input) map[string]string {
	return map[string]string{
		"code": in.Code, "name": in.Name, "uqc": in.UQC, "whole_number": strconv.FormatBool(in.WholeNumber),
	}
}
