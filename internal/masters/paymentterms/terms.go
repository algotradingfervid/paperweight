// Package paymentterms is the Payment Terms master (MST Task 4): portions of
// a document's total due a number of days after its date (D-MST-23).
package paymentterms

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/paymentterms/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Payment Terms"

// Kind is the Payment Terms master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Payment Terms", Table: "payment_terms", HasCode: true}

// rowKind is the owned rows table; its references block a delete too.
var rowKind = mstkit.Kind{DocType: DocType, Label: "Payment Terms", Table: "payment_terms_row"}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Payment Terms",
		PluralLabel: "Payment Terms",
		Table:       Kind.Table,
		LinesTable:  rowKind.Table,
		Importable:  false,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
			{Name: "portion", Label: "Portion", Column: "portion", Type: doctype.Percent, Required: true, OnLine: true},
			{Name: "days", Label: "Days", Column: "days", Type: doctype.Int, Required: true, OnLine: true},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
	}, mstkit.StandardLookup(Kind))
}

// Terms is one Payment Terms record with its rows in order.
type Terms struct {
	ID         int64
	Code, Name string
	Rows       []Row
	Disabled   bool
	Version    int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name string
	Rows       []Row
}

// Validate normalises code and name and checks the rows.
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
	add(ValidateRows(in.Rows))
	if len(fields) > 0 {
		return Input{}, &mstkit.ValidationError{Fields: fields}
	}
	return Input{Code: code, Name: name, Rows: in.Rows}, nil
}

// Service writes and reads Payment Terms.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

// Create adds a Payment Terms record.
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
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightCreate, nil); err != nil {
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
	q := dbq.New(tx.SQL())
	id, err := q.InsertTerms(ctx, dbq.InsertTermsParams{
		Code: in.Code, Name: in.Name,
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := insertRows(ctx, q, id, in.Rows); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(in.Code, in.Name, in.Rows)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the record's code, name and rows.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx.
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
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
	if err := s.env.CheckCodeChange(ctx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	before, after := auditFields(old.Code, old.Name, old.Rows), auditFields(in.Code, in.Name, in.Rows)
	if mstkit.Unchanged(before, after) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	q := dbq.New(tx.SQL())
	n, err := q.UpdateTerms(ctx, dbq.UpdateTermsParams{
		Code: in.Code, Name: in.Name,
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		// The row moved off the posted version: CheckVersion names the stale case.
		if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
			return mstkit.Saved{}, err
		}
		return mstkit.Saved{}, doctype.ErrNotFound
	}
	if err := q.DeleteRows(ctx, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := insertRows(ctx, q, id, in.Rows); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, after); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: old.Version + 1}, nil
}

// SetDisabled disables or enables the record.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx.
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// Delete deletes the record and its rows unless it is referenced.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// DeleteTx is Delete inside tx.
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightDelete, ref(id)); err != nil {
		return err
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version, mstkit.Child{Kind: rowKind, FKColumn: "parent_id"})
}

// Get reads the record and its rows; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Terms, error) {
	r, err := dbq.New(tx.SQL()).GetTerms(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Terms{}, doctype.ErrNotFound
	}
	if err != nil {
		return Terms{}, err
	}
	return s.withRows(ctx, tx, dbq.GetTermsByCodeRow(r))
}

// ByCode reads the record with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Terms, bool, error) {
	r, err := dbq.New(tx.SQL()).GetTermsByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Terms{}, false, nil
	}
	if err != nil {
		return Terms{}, false, err
	}
	t, err := s.withRows(ctx, tx, r)
	return t, err == nil, err
}

// Rows returns the record's rows in order.
func (s *Service) Rows(ctx context.Context, tx *db.Tx, termsID int64) ([]Row, error) {
	list, err := dbq.New(tx.SQL()).ListRows(ctx, termsID)
	if err != nil {
		return nil, err
	}
	rows := make([]Row, len(list))
	for i, r := range list {
		rows[i] = Row{Portion: types.Percent(r.Portion), Days: int(r.Days)}
	}
	return rows, nil
}

func (s *Service) withRows(ctx context.Context, tx *db.Tx, r dbq.GetTermsByCodeRow) (Terms, error) {
	rows, err := s.Rows(ctx, tx, r.ID)
	if err != nil {
		return Terms{}, err
	}
	return Terms{ID: r.ID, Code: r.Code, Name: r.Name, Rows: rows, Disabled: r.Disabled != 0, Version: int(r.Version)}, nil
}

func insertRows(ctx context.Context, q *dbq.Queries, parentID int64, rows []Row) error {
	for i, r := range rows {
		if err := q.InsertRow(ctx, dbq.InsertRowParams{
			ParentID: parentID, LineNo: int64(i + 1), Portion: int64(r.Portion), Days: int64(r.Days),
		}); err != nil {
			return err
		}
	}
	return nil
}

// auditFields is the record as audited: rows read "50% at 0 days; 50% at 30 days".
func auditFields(code, name string, rows []Row) map[string]string {
	parts := make([]string, len(rows))
	for i, r := range rows {
		parts[i] = types.FormatPercent(r.Portion) + " at " + strconv.Itoa(r.Days) + " days"
	}
	return map[string]string{"code": code, "name": name, "rows": strings.Join(parts, "; ")}
}
