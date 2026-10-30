// Package party is the Party master (MST Task 5): one table for vendors and
// customers (D-MST-04). GSTINs live on the party's addresses (M5); the
// address package calls ApplyGSTINTx to back-fill PAN and category.
package party

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party/dbq"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Party"

// Kind is the Party master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Party", Table: "party", HasCode: true}

// owned are the party's own rows, deleted with it (MST-046). The kinds are
// literal because party must not import address (M4).
var owned = []mstkit.Child{
	{Kind: mstkit.Kind{DocType: "Address", Label: "Address", Table: "address"}, FKColumn: "party_id"},
	{Kind: mstkit.Kind{DocType: "Contact", Label: "Contact", Table: "contact"}, FKColumn: "party_id"},
}

// MsgTermsNotFound refuses payment terms that don't exist (CD-216).
const MsgTermsNotFound = "Payment Terms not found. Pick one from the list."

const (
	msgRole     = "Tick Vendor, Customer or both."
	msgCategory = "GST category must be Registered Regular, Registered Composition, Unregistered, SEZ or Overseas."
	// MST-047, as the address package words it (CD-177).
	msgOverseas = "Addresses outside India must have GST category Overseas."
)

func init() {
	categories := make([]string, 0, len(gstin.Categories()))
	for _, c := range gstin.Categories() {
		categories = append(categories, string(c))
	}
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Party",
		PluralLabel: "Parties",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "is_vendor", Label: "Vendor", Column: "is_vendor", Type: doctype.Check, Importable: true, Filterable: true},
			{Name: "is_customer", Label: "Customer", Column: "is_customer", Type: doctype.Check, Importable: true, Filterable: true},
			{Name: "pan", Label: "PAN", Column: "pan", Type: doctype.Text, Importable: true, MaxLen: 10},
			{Name: "gst_category", Label: "GST category", Column: "gst_category", Type: doctype.Select, Importable: true, ListPriority: 5, Options: categories},
			{Name: "default_payment_terms_id", Label: "Payment Terms", Column: "default_payment_terms_id", Type: doctype.Link, Importable: true, Link: paymentterms.DocType},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, Filterable: true, ListPriority: 3},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
	}, mstkit.StandardLookup(Kind))
}

// Role is the side of a document a party stands on.
type Role int

// The two roles.
const (
	Vendor Role = iota
	Customer
)

// Party is one Party record.
type Party struct {
	ID                    int64
	Code, Name            string
	IsVendor, IsCustomer  bool
	PAN                   string
	GSTCategory           gstin.Category
	DefaultPaymentTermsID int64 // 0 = none
	Disabled              bool
	Version               int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name            string
	IsVendor, IsCustomer  bool
	PAN                   string
	GSTCategory           gstin.Category // blank: Unregistered on create; on update guessed from the primary billing address, else kept (M5, CD-166)
	DefaultPaymentTermsID int64
}

// Validate normalises code, name and PAN and checks the role flags and the
// category; a blank category stays blank.
func Validate(in Input) (Input, error) {
	var es mstkit.Errors
	add := func(err error) {
		var ve *mstkit.ValidationError
		if errors.As(err, &ve) {
			for _, f := range ve.Fields {
				es.Add(f.Field, f.Message)
			}
		}
	}
	code, err := mstkit.NormalizeCode(in.Code)
	add(err)
	name, err := mstkit.NormalizeName(in.Name)
	add(err)
	if !in.IsVendor && !in.IsCustomer {
		es.Add("is_vendor", msgRole)
	}
	pan, err := gstin.ValidatePAN(in.PAN)
	if err != nil {
		es.Add("pan", err.Error())
	}
	if in.GSTCategory != "" && !validCategory(in.GSTCategory) {
		es.Add("gst_category", msgCategory) // CD-166; blank is resolved by CreateTx and UpdateTx
	}
	if err := es.Err(); err != nil {
		return Input{}, err
	}
	in.Code, in.Name, in.PAN = code, name, pan
	return in, nil
}

func validCategory(c gstin.Category) bool {
	for _, v := range gstin.Categories() {
		if c == v {
			return true
		}
	}
	return false
}

// Service writes and reads parties.
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

func termsID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Create adds a party.
func (s *Service) Create(ctx context.Context, p auth.Principal, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// CreateTx is Create inside tx. The importer's RowFunc must call it (Task 20).
func (s *Service) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.CanTx(ctx, tx, p, DocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if in.GSTCategory == "" {
		if in.GSTCategory, err = gstin.GuessCategory("", "India", ""); err != nil {
			return mstkit.Saved{}, err
		}
	}
	if err := s.checkTerms(ctx, tx, p, in.DefaultPaymentTermsID); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertParty(ctx, dbq.InsertPartyParams{
		Code: in.Code, Name: in.Name, IsVendor: flag(in.IsVendor), IsCustomer: flag(in.IsCustomer),
		Pan: in.PAN, GstCategory: string(in.GSTCategory), DefaultPaymentTermsID: termsID(in.DefaultPaymentTermsID),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.CheckGST(ctx, tx, id, "gst_category"); err != nil {
		return mstkit.Saved{}, err
	}
	warnings, err := s.saveWarnings(ctx, tx, id, in, Party{})
	if err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1, Warnings: warnings}, nil
}

// Update replaces the party's fields.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx. The importer's RowFunc must call it (Task 20).
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
	if in, err = s.fromAddresses(ctx, tx, id, in, old.GSTCategory); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkTerms(ctx, tx, p, in.DefaultPaymentTermsID); err != nil { // CD-180 D4: after the version
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, tx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkRolesInUse(ctx, tx, old, in); err != nil {
		return mstkit.Saved{}, err
	}
	before, after := auditFields(old), auditFields(fromInput(in))
	if mstkit.Unchanged(before, after) {
		if err := s.CheckGST(ctx, tx, id, "gst_category"); err != nil {
			return mstkit.Saved{}, err
		}
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateParty(ctx, dbq.UpdatePartyParams{
		Code: in.Code, Name: in.Name, IsVendor: flag(in.IsVendor), IsCustomer: flag(in.IsCustomer),
		Pan: in.PAN, GstCategory: string(in.GSTCategory), DefaultPaymentTermsID: termsID(in.DefaultPaymentTermsID),
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
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, after); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.CheckGST(ctx, tx, id, "gst_category"); err != nil {
		return mstkit.Saved{}, err
	}
	warnings, err := s.saveWarnings(ctx, tx, id, in, old)
	if err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: old.Version + 1, Warnings: warnings}, nil
}

// SetDisabled disables or enables the party.
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

// Delete deletes the party with its own addresses and contacts unless any of
// them is referenced (MST-046).
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
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version, owned...)
}

// Get reads the party; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Party, error) {
	r, err := dbq.New(tx.SQL()).GetParty(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Party{}, doctype.ErrNotFound
	}
	if err != nil {
		return Party{}, err
	}
	return fromRow(r), nil
}

// ByCode reads the party with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Party, bool, error) {
	r, err := dbq.New(tx.SQL()).GetPartyByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Party{}, false, nil
	}
	if err != nil {
		return Party{}, false, err
	}
	return fromRow(dbq.GetPartyRow(r)), true, nil
}

// CheckRole refuses a party that isn't marked for role r (MST-042).
func (s *Service) CheckRole(ctx context.Context, tx *db.Tx, partyID int64, r Role) error {
	pt, err := s.Get(ctx, tx, partyID)
	if err != nil {
		return err
	}
	var es mstkit.Errors
	switch {
	case r == Vendor && !pt.IsVendor:
		es.Add("", pt.Name+" is not marked as a vendor. Tick Vendor on the party first.")
	case r == Customer && !pt.IsCustomer:
		es.Add("", pt.Name+" is not marked as a customer.")
	}
	return es.Err()
}

func fromRow(r dbq.GetPartyRow) Party {
	return Party{
		ID: r.ID, Code: r.Code, Name: r.Name, IsVendor: r.IsVendor != 0, IsCustomer: r.IsCustomer != 0,
		PAN: r.Pan, GSTCategory: gstin.Category(r.GstCategory), DefaultPaymentTermsID: r.DefaultPaymentTermsID.Int64,
		Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

func fromInput(in Input) Party {
	return Party{
		Code: in.Code, Name: in.Name, IsVendor: in.IsVendor, IsCustomer: in.IsCustomer,
		PAN: in.PAN, GSTCategory: in.GSTCategory, DefaultPaymentTermsID: in.DefaultPaymentTermsID,
	}
}

// auditFields is the party as audited.
func auditFields(p Party) map[string]string {
	terms := ""
	if p.DefaultPaymentTermsID != 0 {
		terms = strconv.FormatInt(p.DefaultPaymentTermsID, 10)
	}
	return map[string]string{
		"code": p.Code, "name": p.Name,
		"is_vendor": strconv.FormatBool(p.IsVendor), "is_customer": strconv.FormatBool(p.IsCustomer),
		"pan": p.PAN, "gst_category": string(p.GSTCategory), "default_payment_terms_id": terms,
	}
}
