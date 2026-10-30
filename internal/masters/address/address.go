// Package address is the Address master (MST Task 6): addresses of a party
// or a branch, holding the GSTIN (D-MST-06), with one primary billing and one
// primary shipping address per owner (MST-059, MST-060).
package address

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address/dbq"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Address"

// Kind is the Address master. Its table's name column holds the title, or
// the city when the title is blank, which is what messages show.
var Kind = mstkit.Kind{DocType: DocType, Label: "Address", Table: "address"}

const india = "India"

const (
	msgOwner    = "An address must belong to one party or one branch."
	msgLine1    = "Address line 1 is required."
	msgCity     = "City is required."
	msgState    = "State is required for an Indian address."
	msgType     = "Tick Billing, Shipping or both."
	msgOverseas = "Addresses outside India must have GST category Overseas."
	msgMoved    = "The owner of an address can't be changed."
	// CD-177 (1) and (3).
	msgKeepPrimaryBilling = "This is the party's primary billing address. Make another billing address primary first."
	msgEnableFirst        = "Enable this address before making it primary."
)

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Address",
		PluralLabel: "Addresses",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "party_id", Label: "Party", Column: "party_id", Type: doctype.Link, Link: party.DocType, Importable: true},
			{Name: "branch_id", Label: "Branch", Column: "branch_id", Type: doctype.Link, Link: company.DocType},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, ReadOnly: true, ListPriority: 1}, // title, else city (CD-79)
			{Name: "title", Label: "Title", Column: "title", Type: doctype.Text, Importable: true},
			{Name: "line1", Label: "Line 1", Column: "line1", Type: doctype.Text, Required: true, Importable: true},
			{Name: "line2", Label: "Line 2", Column: "line2", Type: doctype.Text, Importable: true},
			{Name: "city", Label: "City", Column: "city", Type: doctype.Text, Required: true, Importable: true, ListPriority: 5},
			{Name: "state_code", Label: "State", Column: "state_code", Type: doctype.Text, Importable: true},
			{Name: "country", Label: "Country", Column: "country", Type: doctype.Text, Importable: true},
			{Name: "pincode", Label: "Pincode", Column: "pincode", Type: doctype.Text, Importable: true},
			{Name: "gstin", Label: "GSTIN", Column: "gstin", Type: doctype.Text, Importable: true, MaxLen: 15},
			{Name: "is_billing", Label: "Billing", Column: "is_billing", Type: doctype.Check, Importable: true},
			{Name: "is_shipping", Label: "Shipping", Column: "is_shipping", Type: doctype.Check, Importable: true},
			{Name: "is_primary_billing", Label: "Primary billing", Column: "is_primary_billing", Type: doctype.Check},
			{Name: "is_primary_shipping", Label: "Primary shipping", Column: "is_primary_shipping", Type: doctype.Check},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, Filterable: true, ListPriority: 3},
		},
		SearchFields: []string{"title", "city"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
	}, mstkit.StandardLookup(Kind))
}

// Address is one Address record.
type Address struct {
	ID                                 int64
	PartyID, BranchID                  int64 // exactly one is set
	Title, Line1, Line2, City          string
	StateCode, Country, Pincode, GSTIN string // StateCode is "" outside India
	IsBilling, IsShipping              bool
	PrimaryBilling, PrimaryShipping    bool
	Disabled                           bool
	Version                            int
}

// Input is what Create and Update accept. PrimaryBilling and PrimaryShipping
// promote the address; the first address of a type becomes primary anyway.
type Input struct {
	PartyID, BranchID                                             int64
	Title, Line1, Line2, City, StateCode, Country, Pincode, GSTIN string
	IsBilling, IsShipping, PrimaryBilling, PrimaryShipping        bool
}

// Validate normalises in and checks the rules that need no database: one
// owner (MST-067), required fields (MST-053), type (MST-054), pincode format
// and state (MST-055, MST-056) and the GSTIN with the state it gives
// (MST-037). A blank country is India; outside India the state is blank and
// the pincode is free text (MST-057).
func Validate(in Input) (Input, error) {
	var es mstkit.Errors
	if (in.PartyID == 0) == (in.BranchID == 0) {
		es.Add("party_id", msgOwner)
	}
	in.Title, in.Line1, in.Line2 = strings.TrimSpace(in.Title), strings.TrimSpace(in.Line1), strings.TrimSpace(in.Line2)
	in.City, in.StateCode, in.Country = strings.TrimSpace(in.City), strings.TrimSpace(in.StateCode), strings.TrimSpace(in.Country)
	if in.Country == "" || strings.EqualFold(in.Country, india) {
		in.Country = india
	}
	if in.Line1 == "" {
		es.Add("line1", msgLine1)
	}
	if in.City == "" {
		es.Add("city", msgCity)
	}
	es.CheckLen("title", "Title", in.Title)
	es.CheckLen("line1", "Line 1", in.Line1)
	es.CheckLen("line2", "Line 2", in.Line2)
	es.CheckLen("city", "City", in.City)
	es.CheckLen("country", "Country", in.Country)
	in.GSTIN = gstin.Normalize(in.GSTIN)
	gstinOK := true
	if err := gstin.ValidateGSTIN(in.GSTIN); err != nil {
		es.Add("gstin", err.Error())
		gstinOK = false
	}
	if in.Country == india {
		if gstinOK {
			state, err := gstin.ResolveState(in.GSTIN, in.StateCode, "address")
			if err != nil {
				es.Add("gstin", err.Error())
			} else {
				in.StateCode = state
			}
		}
		if (gstinOK || in.StateCode == "") && !gstin.IsIndianState(in.StateCode) {
			es.Add("state_code", msgState)
		}
		pin, err := gstin.ValidatePincode(in.Pincode)
		if err == nil {
			err = gstin.PincodeInState(pin, in.StateCode) // MST-056
		}
		if err != nil {
			es.Add("pincode", err.Error())
		}
		in.Pincode = pin
	} else {
		in.StateCode = ""
		in.Pincode = strings.TrimSpace(in.Pincode)
		es.CheckLen("pincode", "Pincode", in.Pincode)
	}
	if !in.IsBilling && !in.IsShipping {
		es.Add("is_billing", msgType)
	}
	in.PrimaryBilling = in.PrimaryBilling && in.IsBilling
	in.PrimaryShipping = in.PrimaryShipping && in.IsShipping
	if err := es.Err(); err != nil {
		return Input{}, err
	}
	return in, nil
}

// Service writes and reads addresses.
type Service struct {
	env     mstkit.Env
	parties *party.Service
	company *company.Service
}

// New returns a Service over env.
func New(env mstkit.Env) *Service {
	return &Service{env: env, parties: party.New(env), company: company.New(env)}
}

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

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// Create adds an address.
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
	billingOthers, shippingOthers, err := s.primaries(ctx, tx, &in, 0, false)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkOwner(ctx, tx, p, in, true); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.clearPrimaries(ctx, tx, p, billingOthers, shippingOthers); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertAddress(ctx, dbq.InsertAddressParams{
		PartyID: nullID(in.PartyID), BranchID: nullID(in.BranchID), Name: displayName(in.Title, in.City),
		Title: in.Title, Line1: in.Line1, Line2: in.Line2, City: in.City, StateCode: stateCode(in.StateCode),
		Country: in.Country, Pincode: in.Pincode, Gstin: in.GSTIN,
		IsBilling: flag(in.IsBilling), IsShipping: flag(in.IsShipping),
		IsPrimaryBilling: flag(in.PrimaryBilling), IsPrimaryShipping: flag(in.PrimaryShipping),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkGST(ctx, tx, in.PartyID); err != nil {
		return mstkit.Saved{}, err
	}
	warnings, err := s.panWarnings(ctx, tx, in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1, Warnings: warnings}, nil
}

// Update replaces the address's fields.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx, at the version the page posted (CD-94). The
// importer's RowFunc must call it (Task 20).
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightWrite, ref(id)); err != nil {
		return mstkit.Saved{}, err
	}
	old, err := s.Get(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	// The owner is fixed once saved: an address moves by being re-created
	// (CD-176). A blank owner keeps the stored one.
	switch {
	case in.PartyID == 0 && in.BranchID == 0:
		in.PartyID, in.BranchID = old.PartyID, old.BranchID
	case in.PartyID != old.PartyID || in.BranchID != old.BranchID:
		var es mstkit.Errors
		field := "party_id"
		if old.BranchID != 0 {
			field = "branch_id"
		}
		es.Add(field, msgMoved)
		return mstkit.Saved{}, es.Err()
	}
	in, err = Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if old.Disabled && (in.PrimaryBilling || in.PrimaryShipping) {
		var es mstkit.Errors
		es.Add("disabled", msgEnableFirst) // CD-180 D2
		return mstkit.Saved{}, es.Err()
	}
	if !in.IsBilling {
		if err := s.keepPrimaryBilling(ctx, tx, old, "is_billing"); err != nil {
			return mstkit.Saved{}, err
		}
	}
	billingOthers, shippingOthers, err := s.primaries(ctx, tx, &in, id, old.Disabled)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkOwner(ctx, tx, p, in, false); err != nil {
		return mstkit.Saved{}, err
	}
	before, after := auditFields(old), auditFields(fromInput(in))
	if mstkit.Unchanged(before, after) {
		if err := s.checkGST(ctx, tx, in.PartyID); err != nil {
			return mstkit.Saved{}, err
		}
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	if err := s.clearPrimaries(ctx, tx, p, billingOthers, shippingOthers); err != nil {
		return mstkit.Saved{}, err
	}
	n, err := dbq.New(tx.SQL()).UpdateAddress(ctx, dbq.UpdateAddressParams{
		Name: displayName(in.Title, in.City), Title: in.Title, Line1: in.Line1, Line2: in.Line2, City: in.City,
		StateCode: stateCode(in.StateCode), Country: in.Country, Pincode: in.Pincode, Gstin: in.GSTIN,
		IsBilling: flag(in.IsBilling), IsShipping: flag(in.IsShipping),
		IsPrimaryBilling: flag(in.PrimaryBilling), IsPrimaryShipping: flag(in.PrimaryShipping),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
			return mstkit.Saved{}, err
		}
		return mstkit.Saved{}, doctype.ErrNotFound
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, after); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkGST(ctx, tx, in.PartyID); err != nil {
		return mstkit.Saved{}, err
	}
	var warnings []string
	if in.GSTIN != old.GSTIN {
		if warnings, err = s.panWarnings(ctx, tx, in); err != nil {
			return mstkit.Saved{}, err
		}
	}
	return mstkit.Saved{ID: id, Version: version + 1, Warnings: warnings}, nil
}

// SetDisabled disables or enables the address.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx. Disabling a primary address that
// may go (keepPrimaryBilling) clears its primary flags and promotes nothing
// (CD-180 D2, D3).
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	a, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err // CD-180 D4: before M5
	}
	if disabled {
		if err := s.keepPrimaryBilling(ctx, tx, a, ""); err != nil {
			return err
		}
	}
	if err := s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled); err != nil {
		return err
	}
	if disabled {
		// Disabling a primary bumps the version twice (SetDisabledTx, then
		// clearPrimaries) and writes two audit entries, on purpose.
		var billing, shipping []int64
		if a.PrimaryBilling {
			billing = []int64{id}
		}
		if a.PrimaryShipping {
			shipping = []int64{id}
		}
		if err := s.clearPrimaries(ctx, tx, p, billing, shipping); err != nil {
			return err
		}
	}
	return s.checkGST(ctx, tx, a.PartyID)
}

// Delete deletes an unreferenced address.
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
	a, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err // CD-180 D4: before M5
	}
	if err := s.keepPrimaryBilling(ctx, tx, a, ""); err != nil {
		return err
	}
	if err := s.env.DeleteTx(ctx, tx, p, Kind, id, version); err != nil {
		return err
	}
	return s.checkGST(ctx, tx, a.PartyID)
}

// panWarnings warns of other parties with the PAN in the GSTIN of party
// address in (MST-045).
func (s *Service) panWarnings(ctx context.Context, tx *db.Tx, in Input) ([]string, error) {
	if in.PartyID == 0 {
		return nil, nil
	}
	state, _ := gstin.StateByCode(in.StateCode)
	return s.parties.SamePANWarnings(ctx, tx, in.PartyID, gstin.PANFromGSTIN(in.GSTIN), state.Name)
}

// checkGST is party.CheckGST for a party's address, with its refusal on
// is_billing (CD-180); a branch address has no party rules.
func (s *Service) checkGST(ctx context.Context, tx *db.Tx, partyID int64) error {
	if partyID == 0 {
		return nil
	}
	return s.parties.CheckGST(ctx, tx, partyID, "is_billing")
}

// keepPrimaryBilling refuses to take a away as the primary billing address
// of a party whose category needs a GSTIN (CD-177 (1), M5): unticking
// Billing, disabling or deleting it. Unregistered and Overseas parties may
// lose it.
func (s *Service) keepPrimaryBilling(ctx context.Context, tx *db.Tx, a Address, field string) error {
	if a.PartyID == 0 || !a.PrimaryBilling {
		return nil
	}
	pt, err := s.parties.Get(ctx, tx, a.PartyID)
	if err != nil {
		return err
	}
	if pt.GSTCategory == gstin.Unregistered || pt.GSTCategory == gstin.Overseas {
		return nil
	}
	var es mstkit.Errors
	es.Add(field, msgKeepPrimaryBilling)
	return es.Err()
}

// SetPrimaryTx makes the address its owner's primary billing (billing true)
// or shipping address, clearing the previous one first in the same
// transaction (MST-060). The partial unique indexes keep it to one per owner
// whatever the interleaving (Review Focus 3).
func (s *Service) SetPrimaryTx(ctx context.Context, tx *db.Tx, p auth.Principal, addressID int64, billing bool, version int) error {
	if err := s.env.CanRowTx(ctx, tx, p, Kind, auth.RightWrite, ref(addressID)); err != nil {
		return err
	}
	a, err := s.Get(ctx, tx, addressID)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, addressID, version); err != nil {
		return err
	}
	var es mstkit.Errors
	switch {
	case a.Disabled:
		es.Add("disabled", msgEnableFirst)
		return es.Err()
	case billing && !a.IsBilling:
		es.Add("is_billing", "Tick Billing on this address before making it the primary billing address.")
		return es.Err()
	case !billing && !a.IsShipping:
		es.Add("is_shipping", "Tick Shipping on this address before making it the primary shipping address.")
		return es.Err()
	case billing && a.PrimaryBilling, !billing && a.PrimaryShipping:
		return nil
	}
	if billing && a.PartyID != 0 {
		// M5, MST-034: the new primary billing address must fit the party's category.
		pt, err := s.parties.Get(ctx, tx, a.PartyID)
		if err != nil {
			return err
		}
		if err := gstin.ValidateCategory(pt.GSTCategory, a.GSTIN); err != nil {
			es.Add("gstin", err.Error())
			return es.Err()
		}
	}
	q := dbq.New(tx.SQL())
	others, err := s.otherPrimaries(ctx, q, a.PartyID, a.BranchID, addressID, billing)
	if err != nil {
		return err
	}
	var billingOthers, shippingOthers []int64
	if billing {
		billingOthers = others
	} else {
		shippingOthers = others
	}
	if err := s.clearPrimaries(ctx, tx, p, billingOthers, shippingOthers); err != nil {
		return err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	var n int64
	column := "is_primary_shipping"
	if billing {
		column = "is_primary_billing"
		n, err = q.SetPrimaryBilling(ctx, dbq.SetPrimaryBillingParams{UpdatedAt: now, UpdatedBy: userID(p), ID: addressID, Version: int64(version)})
	} else {
		n, err = q.SetPrimaryShipping(ctx, dbq.SetPrimaryShippingParams{UpdatedAt: now, UpdatedBy: userID(p), ID: addressID, Version: int64(version)})
	}
	if err != nil {
		return err
	}
	if n == 0 {
		if err := mstkit.CheckVersion(ctx, tx, Kind, addressID, version); err != nil {
			return err
		}
		return doctype.ErrNotFound
	}
	if err := s.env.Audit(ctx, tx, p, Kind, addressID, "save",
		map[string]string{column: "false"}, map[string]string{column: "true"}); err != nil {
		return err
	}
	return s.checkGST(ctx, tx, a.PartyID)
}

// Get reads the address; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Address, error) {
	r, err := dbq.New(tx.SQL()).GetAddress(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Address{}, doctype.ErrNotFound
	}
	if err != nil {
		return Address{}, err
	}
	return fromRow(r), nil
}

// ForParty lists the party's addresses in the order they were added.
func (s *Service) ForParty(ctx context.Context, tx *db.Tx, partyID int64, includeDisabled bool) ([]Address, error) {
	rows, err := dbq.New(tx.SQL()).PartyAddresses(ctx, dbq.PartyAddressesParams{PartyID: nullID(partyID), IncludeDisabled: flag(includeDisabled)})
	if err != nil {
		return nil, err
	}
	out := make([]Address, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// PrimaryGSTIN is the GSTIN of the party's primary billing address, "" when
// it has none.
func (s *Service) PrimaryGSTIN(ctx context.Context, tx *db.Tx, partyID int64) (string, error) {
	r, err := dbq.New(tx.SQL()).PrimaryBillingOfParty(ctx, nullID(partyID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return r.Gstin, nil
}

// primaries settles in's primary flags: an address asked to be primary, or
// the first of its type for the owner unless it is disabled (CD-180 D2), is
// primary. It returns the owner's other primaries that must be cleared first.
func (s *Service) primaries(ctx context.Context, tx *db.Tx, in *Input, id int64, disabled bool) (billingOthers, shippingOthers []int64, err error) {
	q := dbq.New(tx.SQL())
	if in.IsBilling {
		if billingOthers, err = s.otherPrimaries(ctx, q, in.PartyID, in.BranchID, id, true); err != nil {
			return nil, nil, err
		}
		if !in.PrimaryBilling {
			in.PrimaryBilling, billingOthers = len(billingOthers) == 0 && !disabled, nil
		}
	}
	if in.IsShipping {
		if shippingOthers, err = s.otherPrimaries(ctx, q, in.PartyID, in.BranchID, id, false); err != nil {
			return nil, nil, err
		}
		if !in.PrimaryShipping {
			in.PrimaryShipping, shippingOthers = len(shippingOthers) == 0 && !disabled, nil
		}
	}
	return billingOthers, shippingOthers, nil
}

func (s *Service) otherPrimaries(ctx context.Context, q *dbq.Queries, partyID, branchID, id int64, billing bool) ([]int64, error) {
	if billing {
		return q.OtherPrimaryBilling(ctx, dbq.OtherPrimaryBillingParams{PartyID: nullID(partyID), BranchID: nullID(branchID), ID: id})
	}
	return q.OtherPrimaryShipping(ctx, dbq.OtherPrimaryShippingParams{PartyID: nullID(partyID), BranchID: nullID(branchID), ID: id})
}

// clearPrimaries clears the primary flag on the given addresses and audits it.
func (s *Service) clearPrimaries(ctx context.Context, tx *db.Tx, p auth.Principal, billing, shipping []int64) error {
	q := dbq.New(tx.SQL())
	now := types.TimestampOf(s.env.Clock.Now()).String()
	for _, id := range billing {
		if err := q.ClearPrimaryBilling(ctx, dbq.ClearPrimaryBillingParams{UpdatedAt: now, UpdatedBy: userID(p), ID: id}); err != nil {
			return err
		}
		if err := s.env.Audit(ctx, tx, p, Kind, id, "save",
			map[string]string{"is_primary_billing": "true"}, map[string]string{"is_primary_billing": "false"}); err != nil {
			return err
		}
	}
	for _, id := range shipping {
		if err := q.ClearPrimaryShipping(ctx, dbq.ClearPrimaryShippingParams{UpdatedAt: now, UpdatedBy: userID(p), ID: id}); err != nil {
			return err
		}
		if err := s.env.Audit(ctx, tx, p, Kind, id, "save",
			map[string]string{"is_primary_shipping": "true"}, map[string]string{"is_primary_shipping": "false"}); err != nil {
			return err
		}
	}
	return nil
}

// checkOwner checks the owner link (CD-170; a disabled owner only when
// creating, since the owner never changes), for a branch its state (MST-029)
// and, for a party, the GST rules that need the party (M5): Overseas outside
// India (MST-047), the GSTIN fits the party's category (guessed for an
// Unregistered party only from its primary billing address, MST-049), a
// registered party's primary billing address has a GSTIN, the GSTIN is on no
// other party (MST-044), then party.ApplyGSTINTx (MST-048, MST-049).
func (s *Service) checkOwner(ctx context.Context, tx *db.Tx, p auth.Principal, in Input, create bool) error {
	var es mstkit.Errors
	if in.BranchID != 0 {
		b, err := s.company.Get(ctx, tx, in.BranchID)
		switch {
		case errors.Is(err, doctype.ErrNotFound):
			es.Add("branch_id", "Branch not found. Pick one from the list.")
		case err != nil:
			return err
		case create && b.Disabled:
			es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
		case in.StateCode != b.StateCode:
			es.Add("state_code", company.MsgAddressState)
		}
		return es.Err()
	}
	pt, err := s.parties.Get(ctx, tx, in.PartyID)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("party_id", "Party not found. Pick one from the list.")
		return es.Err()
	case err != nil:
		return err
	case create && pt.Disabled:
		es.Add("party_id", "Party "+pt.Code+" is disabled. Pick another party.")
		return es.Err()
	}
	cat := pt.GSTCategory
	if in.Country != india && cat != gstin.Overseas {
		es.Add("country", msgOverseas)
	}
	if in.GSTIN != "" && in.PrimaryBilling && cat == gstin.Unregistered {
		if cat, err = gstin.GuessCategory(in.GSTIN, in.Country, cat); err != nil {
			es.Add("gstin", err.Error())
			return es.Err()
		}
	}
	if in.GSTIN != "" || in.PrimaryBilling {
		if err := gstin.ValidateCategory(cat, in.GSTIN); err != nil {
			es.Add("gstin", err.Error())
		}
	}
	if err := es.Err(); err != nil {
		return err
	}
	if in.GSTIN == "" {
		return nil
	}
	other, err := dbq.New(tx.SQL()).GSTINOnOtherParty(ctx, dbq.GSTINOnOtherPartyParams{Gstin: in.GSTIN, PartyID: nullID(in.PartyID)})
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case other.Disabled != 0:
		es.Add("gstin", "GSTIN "+in.GSTIN+" is already used by party "+other.Code+" (disabled). Enable and use that party instead.")
		return es.Err()
	default:
		es.Add("gstin", "GSTIN "+in.GSTIN+" is already used by party "+other.Code+". Use that party instead.")
		return es.Err()
	}
	return s.parties.ApplyGSTINTx(ctx, tx, p, in.PartyID, in.GSTIN)
}

// displayName is the stored name: the title, else the city.
func displayName(title, city string) string {
	if title != "" {
		return title
	}
	return city
}

func stateCode(code string) sql.NullString { return sql.NullString{String: code, Valid: code != ""} }

func fromRow(r dbq.Address) Address {
	return Address{
		ID: r.ID, PartyID: r.PartyID.Int64, BranchID: r.BranchID.Int64,
		Title: r.Title, Line1: r.Line1, Line2: r.Line2, City: r.City, StateCode: r.StateCode.String,
		Country: r.Country, Pincode: r.Pincode, GSTIN: r.Gstin,
		IsBilling: r.IsBilling != 0, IsShipping: r.IsShipping != 0,
		PrimaryBilling: r.IsPrimaryBilling != 0, PrimaryShipping: r.IsPrimaryShipping != 0,
		Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

func fromInput(in Input) Address {
	return Address{
		PartyID: in.PartyID, BranchID: in.BranchID, Title: in.Title, Line1: in.Line1, Line2: in.Line2,
		City: in.City, StateCode: in.StateCode, Country: in.Country, Pincode: in.Pincode, GSTIN: in.GSTIN,
		IsBilling: in.IsBilling, IsShipping: in.IsShipping, PrimaryBilling: in.PrimaryBilling, PrimaryShipping: in.PrimaryShipping,
	}
}

// auditFields is the address as audited.
func auditFields(a Address) map[string]string {
	id := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	}
	return map[string]string{
		"party_id": id(a.PartyID), "branch_id": id(a.BranchID), "title": a.Title, "line1": a.Line1, "line2": a.Line2,
		"city": a.City, "state_code": a.StateCode, "country": a.Country, "pincode": a.Pincode, "gstin": a.GSTIN,
		"is_billing": strconv.FormatBool(a.IsBilling), "is_shipping": strconv.FormatBool(a.IsShipping),
		"is_primary_billing": strconv.FormatBool(a.PrimaryBilling), "is_primary_shipping": strconv.FormatBool(a.PrimaryShipping),
	}
}
