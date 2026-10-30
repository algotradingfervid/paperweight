package company

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company/dbq"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the Branch's registry name.
const DocType = "Branch"

// Kind is the Branch master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Branch", Table: "branch", HasCode: true}

const (
	msgStateRequired = "State is required. Pick the state where this branch is located."
	msgLastBranch    = "At least one branch must stay enabled."
)

// owned are the branch's own rows, deleted with it. The kinds are literal
// because company must not import address (M4).
var owned = []mstkit.Child{
	{Kind: mstkit.Kind{DocType: "Address", Label: "Address", Table: "address"}, FKColumn: "branch_id"},
	{Kind: mstkit.Kind{DocType: "Contact", Label: "Contact", Table: "contact"}, FKColumn: "branch_id"},
}

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name:         DocType,
		Label:        "Branch",
		PluralLabel:  "Branches",
		Table:        "branch",
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}},
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
			{Name: "state_code", Label: "State", Column: "state_code", Type: doctype.Text, Required: true},
			{Name: "gstin", Label: "GSTIN", Column: "gstin", Type: doctype.Text, MaxLen: 15},
			{Name: "is_head_office", Label: "Head office", Column: "is_head_office", Type: doctype.Check},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, ListPriority: 3},
		},
	}, mstkit.StandardLookup(Kind))
}

// Branch is one of the company's branches.
type Branch struct {
	ID                     int64
	Code, Name, StateCode  string
	GSTIN                  string
	IsHeadOffice, Disabled bool
	Version                int
}

// BranchInput is what a branch save posts.
type BranchInput struct {
	Code, Name, StateCode, GSTIN string
	IsHeadOffice                 bool
}

// ValidateBranch normalises in and checks code, name, state and GSTIN
// (MST-021…MST-024, MST-026). The GSTIN is checked in full first, then
// against the state, then against companyPAN (skipped when "").
func ValidateBranch(in BranchInput, companyPAN string) (BranchInput, error) {
	var es mstkit.Errors
	code, err := mstkit.NormalizeCode(in.Code)
	addErr(&es, err)
	name, err := mstkit.NormalizeName(in.Name)
	addErr(&es, err)
	out := BranchInput{Code: code, Name: name, StateCode: strings.TrimSpace(in.StateCode), GSTIN: gstin.Normalize(in.GSTIN), IsHeadOffice: in.IsHeadOffice}
	if err := gstin.ValidateGSTIN(out.GSTIN); err != nil {
		es.Add("gstin", err.Error())
	} else if state, err := gstin.ResolveState(out.GSTIN, out.StateCode, "branch"); err != nil {
		es.Add("gstin", err.Error())
	} else {
		out.StateCode = state
		if pan := gstin.PANFromGSTIN(out.GSTIN); out.GSTIN != "" && companyPAN != "" && pan != companyPAN {
			es.Add("gstin", "GSTIN "+out.GSTIN+" belongs to PAN "+pan+", not the company PAN "+companyPAN+".")
		}
	}
	if !gstin.IsIndianState(out.StateCode) {
		es.Add("state_code", msgStateRequired)
	}
	return out, es.Err()
}

// Create wraps CreateTx in env.Write.
func (s *Service) Create(ctx context.Context, p auth.Principal, in BranchInput) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// Update wraps UpdateTx in env.Write.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in BranchInput) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// SetDisabled wraps SetDisabledTx in env.Write.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// Delete wraps DeleteTx in env.Write.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// CreateTx creates a branch inside tx.
func (s *Service) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in BranchInput) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := s.validate(ctx, tx, in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.moveHeadOffice(ctx, tx, p, in, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	by := userID(p)
	id, err := dbq.New(tx.SQL()).InsertBranch(ctx, dbq.InsertBranchParams{
		Code: in.Code, Name: in.Name, StateCode: in.StateCode, Gstin: in.GSTIN, IsHeadOffice: flag(in.IsHeadOffice),
		CreatedAt: now, CreatedBy: by, UpdatedAt: now, UpdatedBy: by,
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, inputFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// UpdateTx saves a branch at the version the page posted (CD-94).
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in BranchInput) (mstkit.Saved, error) {
	cur, err := s.can(ctx, tx, p, id, auth.RightWrite)
	if err != nil {
		return mstkit.Saved{}, err
	}
	in, err = s.validate(ctx, tx, in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, p, cur.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	before := inputFields(BranchInput{Code: cur.Code, Name: cur.Name, StateCode: cur.StateCode, GSTIN: cur.GSTIN, IsHeadOffice: cur.IsHeadOffice})
	if mstkit.Unchanged(before, inputFields(in)) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	if err := s.moveHeadOffice(ctx, tx, p, in, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := dbq.New(tx.SQL()).UpdateBranch(ctx, dbq.UpdateBranchParams{
		Code: in.Code, Name: in.Name, StateCode: in.StateCode, Gstin: in.GSTIN, IsHeadOffice: flag(in.IsHeadOffice),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	}); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, inputFields(in)); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: version + 1}, nil
}

// SetDisabledTx disables or enables a branch; the last enabled branch can't
// be disabled (MST-027), nor one with enabled sites (MST-108).
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	cur, err := s.can(ctx, tx, p, id, auth.RightWrite)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err
	}
	if disabled && !cur.Disabled {
		if err := s.checkNotLastEnabled(ctx, tx, id); err != nil {
			return err
		}
		codes, err := dbq.New(tx.SQL()).EnabledSiteCodes(ctx, id)
		if err != nil {
			return err
		}
		if len(codes) > 0 {
			return refused("Disable the branch's sites first: " + strings.Join(codes, ", ") + ".")
		}
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// DeleteTx deletes an unreferenced branch and its own addresses and contacts;
// the last enabled branch can't go (MST-027).
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	cur, err := s.can(ctx, tx, p, id, auth.RightDelete)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err
	}
	if !cur.Disabled {
		if err := s.checkNotLastEnabled(ctx, tx, id); err != nil {
			return err
		}
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version, owned...)
}

// Get returns the branch with id, or doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Branch, error) {
	row, err := dbq.New(tx.SQL()).GetBranch(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Branch{}, doctype.ErrNotFound
	}
	if err != nil {
		return Branch{}, err
	}
	return Branch{
		ID: row.ID, Code: row.Code, Name: row.Name, StateCode: row.StateCode, GSTIN: row.Gstin,
		IsHeadOffice: row.IsHeadOffice == 1, Disabled: row.Disabled == 1, Version: int(row.Version),
	}, nil
}

// ByCode returns the branch with code, normalised as on save.
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Branch, bool, error) {
	code, err := mstkit.NormalizeCode(code)
	if err != nil {
		return Branch{}, false, nil
	}
	id, err := dbq.New(tx.SQL()).BranchIDByCode(ctx, code)
	if errors.Is(err, sql.ErrNoRows) {
		return Branch{}, false, nil
	}
	if err != nil {
		return Branch{}, false, err
	}
	b, err := s.Get(ctx, tx, id)
	return b, err == nil, err
}

// BranchStateCode is the branch's GST state: the GSTIN's first two digits,
// else its state (D-NUM-10).
func (s *Service) BranchStateCode(ctx context.Context, tx *db.Tx, branchID int64) (string, error) {
	b, err := s.Get(ctx, tx, branchID)
	if err != nil {
		return "", err
	}
	return b.gstState(), nil
}

func (b Branch) gstState() string {
	if b.GSTIN != "" {
		return gstin.StateCode(b.GSTIN)
	}
	return b.StateCode
}

// can loads the branch and checks p's right r on it.
func (s *Service) can(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, r auth.Right) (Branch, error) {
	b, err := s.Get(ctx, tx, id)
	if err != nil {
		return Branch{}, err
	}
	return b, s.env.Auth.Can(ctx, p, DocType, r, &auth.RecordRef{DocType: DocType, ID: id})
}

// validate runs ValidateBranch against the stored company PAN.
func (s *Service) validate(ctx context.Context, tx *db.Tx, in BranchInput) (BranchInput, error) {
	c, _, err := s.Company(ctx, tx)
	if err != nil {
		return BranchInput{}, err
	}
	return ValidateBranch(in, c.PAN)
}

// checkNotLastEnabled refuses when no branch but id is enabled (CD-94: read
// inside the write transaction).
func (s *Service) checkNotLastEnabled(ctx context.Context, tx *db.Tx, id int64) error {
	n, err := dbq.New(tx.SQL()).CountOtherEnabledBranches(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return refused(msgLastBranch)
	}
	return nil
}

// moveHeadOffice clears the flag on any other branch when in is the head
// office, so at most one remains.
func (s *Service) moveHeadOffice(ctx context.Context, tx *db.Tx, p auth.Principal, in BranchInput, id int64) error {
	if !in.IsHeadOffice {
		return nil
	}
	q := dbq.New(tx.SQL())
	others, err := q.OtherHeadOffice(ctx, id)
	if err != nil {
		return err
	}
	for _, other := range others {
		if err := q.ClearHeadOffice(ctx, dbq.ClearHeadOfficeParams{
			UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: other,
		}); err != nil {
			return err
		}
		if err := s.env.Audit(ctx, tx, p, Kind, other, "save",
			map[string]string{"is_head_office": "true"}, map[string]string{"is_head_office": "false"}); err != nil {
			return err
		}
	}
	return nil
}

// inputFields is a branch's audited fields.
func inputFields(in BranchInput) map[string]string {
	return map[string]string{
		"code": in.Code, "name": in.Name, "state_code": in.StateCode, "gstin": in.GSTIN,
		"is_head_office": strconv.FormatBool(in.IsHeadOffice),
	}
}

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
