// Package project is the Project master (MST Task 12): a code, a branch, an
// optional customer, optional start and end dates and a status. Completed and
// Cancelled projects block new documents; On Hold ones warn (MST-102).
package project

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Project"

// Kind is the Project master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Project", Table: "project", HasCode: true}

// Status is a project's state (MST-101).
type Status string

// The four statuses, in display order (M13).
const (
	Open      Status = "Open"
	OnHold    Status = "On Hold"
	Completed Status = "Completed"
	Cancelled Status = "Cancelled"
)

var statuses = []Status{Open, OnHold, Completed, Cancelled}

const (
	msgDates    = "End date can't be before start date."
	msgStatus   = "Status must be Open, On Hold, Completed or Cancelled."
	msgBranch   = "Branch is required."
	msgNoBranch = "Branch not found. Pick one from the list."                                                  // CD-170
	msgRestrict = "You can only work with your assigned projects. Ask your administrator to add this project." // CD-172
	msgCustomer = "Customer not found. Pick one from the list."                                                // DOC-032's wording; the role check is Task 27
)

func init() {
	options := make([]string, 0, len(statuses))
	for _, s := range statuses {
		options = append(options, string(s))
	}
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Project",
		PluralLabel: "Projects",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "branch_id", Label: "Branch", Column: "branch_id", Type: doctype.Link, Link: company.DocType, Required: true, Importable: true, ListPriority: 5},
			{Name: "customer_id", Label: "Customer", Column: "customer_id", Type: doctype.Link, Link: party.DocType, Importable: true},
			{Name: "start_date", Label: "Start date", Column: "start_date", Type: doctype.Date, Importable: true},
			{Name: "end_date", Label: "End date", Column: "end_date", Type: doctype.Date, Importable: true},
			{Name: "status", Label: "Status", Column: "status", Type: doctype.Select, Importable: true, Filterable: true, ListPriority: 3, Options: options},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, Filterable: true},
		},
		SearchFields:  []string{"code", "name"},
		DefaultSort:   []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
		ProjectFields: []string{"id"},                     // CD-09, CD-64
	}, mstkit.LookupFunc(lookup))
}

// lookup is the Master lookup: Completed and Cancelled block, On Hold warns.
func lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	r, err := dbq.New(tx.SQL()).GetProject(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	info := doctype.MasterInfo{Code: r.Code, Name: r.Name, Disabled: r.Disabled != 0}
	switch Status(r.Status) {
	case Completed, Cancelled:
		info.Blocked = "Project " + r.Code + " is " + r.Status + ". Reopen it or pick another project."
	case OnHold:
		info.Warning = "Project " + r.Code + " is On Hold."
	}
	return info, true, nil
}

// Project is one Project record.
type Project struct {
	ID         int64
	Code, Name string
	BranchID   int64
	CustomerID int64      // 0 = none
	Start, End types.Date // zero = blank
	Status     Status
	Disabled   bool
	Version    int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name string
	BranchID   int64
	CustomerID int64
	Start, End types.Date
	Status     Status // blank = Open
}

// Validate normalises code and name, defaults the status to Open and checks
// branch presence, status and dates.
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
	if in.BranchID == 0 {
		es.Add("branch_id", msgBranch)
	}
	if in.Status == "" {
		in.Status = Open
	}
	if !validStatus(in.Status) {
		es.Add("status", msgStatus) // CD-166
	}
	if in.Start != (types.Date{}) && in.End != (types.Date{}) && in.End.Compare(in.Start) < 0 {
		es.Add("end_date", msgDates)
	}
	if err := es.Err(); err != nil {
		return Input{}, err
	}
	in.Code, in.Name = code, name
	return in, nil
}

func validStatus(s Status) bool {
	for _, v := range statuses {
		if s == v {
			return true
		}
	}
	return false
}

// Service writes and reads projects.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

// ref carries the project's own id as its project value (ProjectFields {"id"}, CD-64).
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: DocType, ID: id, ProjectIDs: []int64{id}}
}

// checkUnrestricted refuses a principal restricted on Project (CD-172),
// reading inside tx (CD-174).
func (s *Service) checkUnrestricted(ctx context.Context, tx *db.Tx, p auth.Principal) error {
	restricted, err := s.env.Auth.Restricted(ctx, tx, p, string(auth.DimProject))
	if err != nil {
		return err
	}
	if restricted {
		return auth.Forbidden(msgRestrict)
	}
	return nil
}

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

func nullDate(d types.Date) sql.NullString {
	if d == (types.Date{}) {
		return sql.NullString{}
	}
	return sql.NullString{String: d.ISO(), Valid: true}
}

// checkLinks refuses an unknown or disabled branch and an unknown customer,
// reading inside tx (CD-94, CD-170).
func (s *Service) checkLinks(ctx context.Context, tx *db.Tx, in Input) error {
	var es mstkit.Errors
	b, err := company.New(s.env).Get(ctx, tx, in.BranchID)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("branch_id", msgNoBranch)
	case err != nil:
		return err
	case b.Disabled:
		es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
	}
	if in.CustomerID != 0 {
		if _, err := party.New(s.env).Get(ctx, tx, in.CustomerID); errors.Is(err, doctype.ErrNotFound) {
			es.Add("customer_id", msgCustomer)
		} else if err != nil {
			return err
		}
	}
	return es.Err()
}

// Create adds a project.
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
	if err := s.checkUnrestricted(ctx, tx, p); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLinks(ctx, tx, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertProject(ctx, dbq.InsertProjectParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, CustomerID: nullID(in.CustomerID),
		StartDate: nullDate(in.Start), EndDate: nullDate(in.End), Status: string(in.Status),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the project's fields.
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
	if err := s.checkLinks(ctx, tx, in); err != nil {
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
	before, after := auditFields(old), auditFields(fromInput(in))
	if mstkit.Unchanged(before, after) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateProject(ctx, dbq.UpdateProjectParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, CustomerID: nullID(in.CustomerID),
		StartDate: nullDate(in.Start), EndDate: nullDate(in.End), Status: string(in.Status),
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
	return mstkit.Saved{ID: id, Version: old.Version + 1}, nil
}

// SetDisabled disables or enables the project.
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

// Delete deletes the project unless it is referenced.
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
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version)
}

// Get reads the project; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Project, error) {
	r, err := dbq.New(tx.SQL()).GetProject(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, doctype.ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	return fromRow(r)
}

// Codes maps each of ids to its project's code; ids with no project are left out
// (auth.Directory, M10).
func (s *Service) Codes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	codes := make(map[int64]string, len(ids))
	for _, id := range ids {
		r, err := s.Get(ctx, tx, id)
		if errors.Is(err, doctype.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		codes[id] = r.Code
	}
	return codes, nil
}

// ByCode reads the project with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Project, bool, error) {
	r, err := dbq.New(tx.SQL()).GetProjectByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, false, nil
	}
	if err != nil {
		return Project{}, false, err
	}
	pr, err := fromRow(dbq.GetProjectRow(r))
	return pr, err == nil, err
}

func fromRow(r dbq.GetProjectRow) (Project, error) {
	pr := Project{
		ID: r.ID, Code: r.Code, Name: r.Name, BranchID: r.BranchID, CustomerID: r.CustomerID.Int64,
		Status: Status(r.Status), Disabled: r.Disabled != 0, Version: int(r.Version),
	}
	var err error
	if r.StartDate.Valid {
		if pr.Start, err = types.DateFromISO(r.StartDate.String); err != nil {
			return Project{}, err
		}
	}
	if r.EndDate.Valid {
		if pr.End, err = types.DateFromISO(r.EndDate.String); err != nil {
			return Project{}, err
		}
	}
	return pr, nil
}

func fromInput(in Input) Project {
	return Project{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, CustomerID: in.CustomerID,
		Start: in.Start, End: in.End, Status: in.Status,
	}
}

// auditFields is the project as audited.
func auditFields(p Project) map[string]string {
	id := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	}
	return map[string]string{
		"code": p.Code, "name": p.Name, "branch_id": id(p.BranchID), "customer_id": id(p.CustomerID),
		"start_date": nullDate(p.Start).String, "end_date": nullDate(p.End).String, "status": string(p.Status),
	}
}
