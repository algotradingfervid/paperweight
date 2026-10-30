// Package employee is the Employee master (MST Task 14): a code, a branch, an
// optional user and reporting manager, dates and a status, Active or Left.
// disabled mirrors status = 'Left'. Leaving disables the linked user and stores
// its version; re-joining enables it only if that version still stands
// (MST-119, CD-189). Directory implements auth.Directory (M10).
package employee

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee/dbq"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Employee"

// Kind is the Employee master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Employee", Table: "employee", HasCode: true}

// Status is an employee's status.
type Status string

// The two statuses, in display order (M13).
const (
	Active Status = "Active"
	Left   Status = "Left"
)

var statuses = []Status{Active, Left}

const (
	msgStatus          = "Status must be Active or Left." // CD-166
	msgBranch          = "Branch is required."            // MST-109
	msgJoining         = "Date of joining is required."   // MST-109
	msgBirthFuture     = "Date of birth can't be in the future."
	msgRelievingBefore = "Relieving date can't be before date of joining."
	msgEnterRelieving  = "Enter the relieving date."                            // MST-118
	msgSelfManager     = "An employee can't report to themselves."              // MST-113
	msgNoBranch        = "Branch not found. Pick one from the list."            // CD-170
	msgNoManager       = "Reporting manager not found. Pick one from the list." // CD-170
	msgNoUser          = "Pick an existing, enabled user."                      // MST-112
	msgSeparateSaves   = "Change the user and the status in separate saves."    // MST-119
)

func init() {
	options := make([]string, 0, len(statuses))
	for _, s := range statuses {
		options = append(options, string(s))
	}
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Employee",
		PluralLabel: "Employees",
		Table:       Kind.Table,
		Importable:  true,
		// Step slugs follow the create wizard: details ("", the first step), then reporting (M13, CD-82).
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, ListPriority: 1},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Filterable: true},
			{Name: "joining_date", Label: "Date of joining", Column: "joining_date", Type: doctype.Date, Required: true},
			{Name: "birth_date", Label: "Date of birth", Column: "birth_date", Type: doctype.Date},
			{Name: "relieving_date", Label: "Relieving date", Column: "relieving_date", Type: doctype.Date},
			{Name: "status", Label: "Status", Column: "status", Type: doctype.Select, Required: true, Filterable: true, ListPriority: 3, Options: options},
			{Name: "branch_id", Label: "Branch", Column: "branch_id", Type: doctype.Link, Link: company.DocType, Required: true, ListPriority: 5, Step: "reporting"},
			{Name: "manager_id", Label: "Reporting manager", Column: "manager_id", Type: doctype.Link, Link: DocType, ListPriority: 6, Step: "reporting"},
			{Name: "user_id", Label: "User", Column: "user_id", Type: doctype.Int, Step: "reporting"}, // a user id; users are not a DocType
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
	}, mstkit.LookupFunc(lookup))
}

// lookup is the Master lookup: a Left employee blocks new documents.
func lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	r, err := dbq.New(tx.SQL()).GetEmployee(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	info := doctype.MasterInfo{Code: r.Code, Name: r.Name}
	if Status(r.Status) == Left { // MST-120: the left message, not the disabled one
		info.Blocked = "Employee " + r.Code + " has left. Pick an active employee."
	}
	return info, true, nil
}

// Employee is one Employee record.
type Employee struct {
	ID         int64
	Code, Name string
	BranchID   int64
	UserID     auth.UserID // 0 = none
	ManagerID  int64       // 0 = none
	Joining    types.Date
	Birth      types.Date // zero = blank
	Relieving  types.Date // zero = blank
	Status     Status
	Disabled   bool
	Version    int

	leaveVersion sql.NullInt64 // user_version_on_leave: the user's version right after this employee's leave disabled it (CD-189)
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name string
	BranchID   int64
	UserID     auth.UserID
	ManagerID  int64
	Joining    types.Date
	Birth      types.Date
	Relieving  types.Date
	Status     Status
}

// Validate normalises code and name and checks required fields, status and
// the dates that need no clock (MST-109, MST-110, MST-118).
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
	if in.Joining == (types.Date{}) {
		es.Add("joining_date", msgJoining)
	}
	if !validStatus(in.Status) {
		es.Add("status", msgStatus) // CD-166
	}
	if in.Status == Left && in.Relieving == (types.Date{}) {
		es.Add("relieving_date", msgEnterRelieving)
	}
	if in.Joining != (types.Date{}) && in.Relieving != (types.Date{}) && in.Relieving.Compare(in.Joining) < 0 {
		es.Add("relieving_date", msgRelievingBefore)
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

// Service writes and reads employees.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

func ref(id int64) *auth.RecordRef { return &auth.RecordRef{DocType: DocType, ID: id} }

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

func flag(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// check runs the rules that read other rows, inside tx (CD-94): the link
// fields first (CD-170), then date of birth against today in IST (Review
// Focus 4), self-manager (MST-113), the manager's status (MST-116), loops
// (MST-114), the user's other active employee (MST-111) and reportees of
// someone leaving (MST-117). A leave skips the branch and loop checks of links
// it keeps, so neither can hold a leaver's user enabled, and MST-116 never
// refuses a leave (CD-204). old is nil on create.
func (s *Service) check(ctx context.Context, tx *db.Tx, old *Employee, in Input) error {
	var id int64
	if old != nil {
		id = old.ID
	}
	leaving := old != nil && old.Status == Active && in.Status == Left
	keepsBranch := leaving && in.BranchID == old.BranchID
	keepsManager := leaving && in.ManagerID == old.ManagerID
	if in.ManagerID != 0 && in.ManagerID == id && !keepsManager {
		return fieldErr("manager_id", msgSelfManager)
	}
	var es mstkit.Errors
	if !keepsBranch {
		b, err := company.New(s.env).Get(ctx, tx, in.BranchID)
		switch {
		case errors.Is(err, doctype.ErrNotFound):
			es.Add("branch_id", msgNoBranch)
		case err != nil:
			return err
		case b.Disabled:
			es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
		}
	}
	if in.ManagerID != 0 {
		m, err := s.Get(ctx, tx, in.ManagerID)
		switch {
		case errors.Is(err, doctype.ErrNotFound):
			es.Add("manager_id", msgNoManager)
		case err != nil:
			return err
		case m.Status == Left && in.Status == Active: // MST-116 applies to active employees
			es.Add("manager_id", "Reporting manager "+m.Code+" has left. Pick an active manager.")
		}
	}
	var user auth.UserInfo
	if in.UserID != 0 {
		var ok bool
		var err error
		if user, ok, err = s.env.Auth.User(ctx, tx, in.UserID); err != nil {
			return err
		}
		// MST-112 on an Active save that sets or changes the link, and on a
		// re-join whose stored version isn't the user's (CD-181, CD-189).
		linking := in.Status == Active && (old == nil || old.UserID != in.UserID)
		staleRejoin := old != nil && old.Status == Left && in.Status == Active &&
			old.leaveVersion != sql.NullInt64{Int64: int64(user.Version), Valid: true}
		if !ok || ((linking || staleRejoin) && !user.Enabled) {
			es.Add("user_id", msgNoUser)
		}
	}
	if in.Birth != (types.Date{}) && in.Birth.Compare(s.env.Today()) > 0 {
		es.Add("birth_date", msgBirthFuture)
	}
	if err := es.Err(); err != nil {
		return err
	}
	q := dbq.New(tx.SQL())
	if in.ManagerID != 0 && id != 0 && !keepsManager {
		if err := s.checkLoop(ctx, tx, id, in.Code, in.ManagerID); err != nil {
			return err
		}
	}
	if in.UserID != 0 && (in.Status == Active || old == nil) { // MST-111; a Left create too (CD-181)
		if err := checkUnlinked(ctx, tx, id, in.UserID, user); err != nil {
			return err
		}
	}
	if in.Status == Left && id != 0 {
		reps, err := q.ActiveReports(ctx, nullID(id))
		if err != nil {
			return err
		}
		for _, r := range reps {
			if r.ID != id { // a stored manager_id = id isn't a report (CD-204)
				return fieldErr("status", r.Code+" ("+r.Name+") still reports to "+in.Code+". Move them to another manager first.")
			}
		}
	}
	return nil
}

// checkUnlinked refuses u when an Active employee other than id has it (MST-111).
func checkUnlinked(ctx context.Context, tx *db.Tx, id int64, u auth.UserID, user auth.UserInfo) error {
	other, err := dbq.New(tx.SQL()).ActiveEmployeeForUser(ctx, dbq.ActiveEmployeeForUserParams{UserID: nullID(int64(u)), ID: id})
	switch {
	case err == nil:
		return fieldErr("user_id", "User "+user.Email+" is already linked to employee "+other.Code+".")
	case !errors.Is(err, sql.ErrNoRows):
		return err
	}
	return nil
}

func fieldErr(field, msg string) error {
	var es mstkit.Errors
	es.Add(field, msg)
	return es.Err()
}

// syncUser disables the user on a leave that finds it enabled and enables it on
// a re-join that finds it disabled, which check allowed only with the user's
// version stored (MST-119, CD-189). It reads the user in tx and returns
// user_version_on_leave after the save: the version right after DisableUser, or
// NULL after any other status change; other saves keep it. user_id can't change
// in either save (CD-181).
func (s *Service) syncUser(ctx context.Context, tx *db.Tx, old Employee, in Input) (sql.NullInt64, error) {
	if old.Status == in.Status {
		return old.leaveVersion, nil
	}
	if old.UserID == 0 {
		return sql.NullInt64{}, nil
	}
	user, _, err := s.env.Auth.User(ctx, tx, old.UserID)
	if err != nil {
		return sql.NullInt64{}, err
	}
	switch {
	case in.Status == Active && !user.Enabled:
		return sql.NullInt64{}, s.env.Auth.EnableUser(ctx, tx, old.UserID)
	case in.Status == Left && user.Enabled:
		if err := s.env.Auth.DisableUser(ctx, tx, old.UserID, "Employee "+in.Code+" has left."); err != nil {
			return sql.NullInt64{}, err
		}
		if user, _, err = s.env.Auth.User(ctx, tx, old.UserID); err != nil {
			return sql.NullInt64{}, err
		}
		return sql.NullInt64{Int64: int64(user.Version), Valid: true}, nil
	}
	return sql.NullInt64{}, nil
}

// Create adds an employee.
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
	if err := s.check(ctx, tx, nil, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertEmployee(ctx, dbq.InsertEmployeeParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, UserID: nullID(int64(in.UserID)), ManagerID: nullID(in.ManagerID),
		JoiningDate: in.Joining.ISO(), BirthDate: nullDate(in.Birth), RelievingDate: nullDate(in.Relieving),
		Status: string(in.Status), Disabled: flag(in.Status == Left),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	// A create never enables or disables a user nor stores its version (CD-181, CD-189).
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the employee's fields.
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
	if in.UserID != old.UserID && (old.Status != Active || in.Status != Active) {
		return mstkit.Saved{}, fieldErr("user_id", msgSeparateSaves) // CD-181: the user changes only while Active
	}
	if err := s.check(ctx, tx, &old, in); err != nil {
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
	leaveVersion, err := s.syncUser(ctx, tx, old, in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	// CheckVersion above ran in this tx, so the version condition always matches.
	if err := dbq.New(tx.SQL()).UpdateEmployee(ctx, dbq.UpdateEmployeeParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, UserID: nullID(int64(in.UserID)), ManagerID: nullID(in.ManagerID),
		JoiningDate: in.Joining.ISO(), BirthDate: nullDate(in.Birth), RelievingDate: nullDate(in.Relieving),
		Status: string(in.Status), Disabled: flag(in.Status == Left), UserVersionOnLeave: leaveVersion,
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	}); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", auditFields(old), auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: old.Version + 1}, nil
}

// SetDisabled disables or enables the employee.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx. disabled mirrors status, so it is a
// status change to Left (disabled) or Active through UpdateTx, with every
// status rule (MST-111, MST-112, MST-117, MST-118, MST-119, CD-189). A row
// already in the requested state is left alone, after the rights and version
// checks (CD-181). Re-joining clears the relieving date.
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err
	}
	old, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Disabled == disabled {
		return nil
	}
	in := Input{
		Code: old.Code, Name: old.Name, BranchID: old.BranchID, UserID: old.UserID, ManagerID: old.ManagerID,
		Joining: old.Joining, Birth: old.Birth, Relieving: old.Relieving, Status: Active,
	}
	if disabled {
		in.Status = Left
	} else {
		in.Relieving = types.Date{}
	}
	_, err = s.UpdateTx(ctx, tx, p, id, version, in)
	return err
}

// Delete deletes the employee unless it is referenced.
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

// Get reads the employee; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Employee, error) {
	r, err := dbq.New(tx.SQL()).GetEmployee(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Employee{}, doctype.ErrNotFound
	}
	if err != nil {
		return Employee{}, err
	}
	return fromRow(r)
}

// ByCode reads the employee with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Employee, bool, error) {
	r, err := dbq.New(tx.SQL()).GetEmployeeByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Employee{}, false, nil
	}
	if err != nil {
		return Employee{}, false, err
	}
	e, err := fromRow(dbq.GetEmployeeRow(r))
	return e, err == nil, err
}

func fromRow(r dbq.GetEmployeeRow) (Employee, error) {
	e := Employee{
		ID: r.ID, Code: r.Code, Name: r.Name, BranchID: r.BranchID,
		UserID: auth.UserID(r.UserID.Int64), ManagerID: r.ManagerID.Int64,
		Status: Status(r.Status), Disabled: r.Disabled != 0, Version: int(r.Version),
		leaveVersion: r.UserVersionOnLeave,
	}
	var err error
	if e.Joining, err = types.DateFromISO(r.JoiningDate); err != nil {
		return Employee{}, err
	}
	if r.BirthDate.Valid {
		if e.Birth, err = types.DateFromISO(r.BirthDate.String); err != nil {
			return Employee{}, err
		}
	}
	if r.RelievingDate.Valid {
		if e.Relieving, err = types.DateFromISO(r.RelievingDate.String); err != nil {
			return Employee{}, err
		}
	}
	return e, nil
}

func fromInput(in Input) Employee {
	return Employee{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, UserID: in.UserID, ManagerID: in.ManagerID,
		Joining: in.Joining, Birth: in.Birth, Relieving: in.Relieving, Status: in.Status, Disabled: in.Status == Left,
	}
}

// auditFields is the employee as audited.
func auditFields(e Employee) map[string]string {
	id := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	}
	return map[string]string{
		"code": e.Code, "name": e.Name, "branch_id": id(e.BranchID), "user_id": id(int64(e.UserID)),
		"manager_id": id(e.ManagerID), "joining_date": nullDate(e.Joining).String, "birth_date": nullDate(e.Birth).String,
		"relieving_date": nullDate(e.Relieving).String, "status": string(e.Status), "disabled": strconv.FormatBool(e.Disabled),
	}
}
