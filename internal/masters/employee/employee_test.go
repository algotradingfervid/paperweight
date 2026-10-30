package employee_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// Messages copied verbatim from docs/tests/core/06-masters.md.
const (
	msgNameRequired    = "Name is required."                                               // MST-109
	msgBranchRequired  = "Branch is required."                                             // MST-109
	msgJoiningRequired = "Date of joining is required."                                    // MST-109
	msgBirthFuture     = "Date of birth can't be in the future."                           // MST-110
	msgRelievingBefore = "Relieving date can't be before date of joining."                 // MST-110
	msgEnterRelieving  = "Enter the relieving date."                                       // MST-118
	msgUserLinked      = "User deepak@vardhan.test is already linked to employee EMP-007." // MST-111
)

func date(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// joining is every fixture employee's date of joining (M9).
var joining = date(2024, time.April, 1)

// validInput is an active employee with every field Validate needs (MST-109
// literals: EMP-007 Deepak Nair); tests vary one.
func validInput() employee.Input {
	return employee.Input{Code: "EMP-007", Name: "Deepak Nair", BranchID: 1, Joining: joining, Status: employee.Active}
}

// wantFieldError checks err is a 422 *mstkit.ValidationError carrying exactly
// one header field error with msg, on a named field.
func wantFieldError(t *testing.T, err error, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	got := ve.FieldErrors()
	if len(got) != 1 {
		t.Fatalf("FieldErrors() = %+v; want exactly one with %q", got, msg)
	}
	if got[0].Message != msg || got[0].Line != 0 {
		t.Errorf("FieldErrors()[0] = %+v; want header error %q", got[0], msg)
	}
	if got[0].Field == "" {
		t.Errorf("FieldErrors()[0].Field is empty; want the field the error belongs to")
	}
}

func TestEmployee_RequiredFields(t *testing.T) { // MST-109
	got, err := employee.Validate(validInput())
	if err != nil {
		t.Fatalf("Validate(valid) = %v; want nil", err)
	}
	if got.Code != "EMP-007" || got.Name != "Deepak Nair" || got.BranchID != 1 || got.Joining != joining || got.Status != employee.Active {
		t.Errorf("Validate(valid) = %+v; want the input unchanged", got)
	}

	cases := []struct {
		name string
		edit func(*employee.Input)
		msg  string
	}{
		{"no name", func(in *employee.Input) { in.Name = "" }, msgNameRequired},
		{"blank name", func(in *employee.Input) { in.Name = "   " }, msgNameRequired},
		{"no branch", func(in *employee.Input) { in.BranchID = 0 }, msgBranchRequired},
		{"no date of joining", func(in *employee.Input) { in.Joining = types.Date{} }, msgJoiningRequired},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			c.edit(&in)
			_, err := employee.Validate(in)
			wantFieldError(t, err, c.msg)
		})
	}
}

func TestEmployee_Dates(t *testing.T) { // MST-110
	// Relieving before joining is a pure rule.
	in := validInput()
	in.Status, in.Relieving = employee.Left, date(2024, time.March, 31)
	_, err := employee.Validate(in)
	wantFieldError(t, err, msgRelievingBefore)

	in.Relieving = joining // relieving on the joining day is fine
	if _, err := employee.Validate(in); err != nil {
		t.Errorf("Validate(relieving = joining) = %v; want nil", err)
	}

	// Date of birth is checked against env.Today(): 2026-10-24 in Asia/Kolkata.
	w := newWorld(t)
	if today := w.env.Kit.Today(); today != date(2026, time.October, 24) {
		t.Fatalf("env.Today() = %v; want 2026-10-24", today)
	}
	in = w.input("EMP-009", "Asha Menon")
	in.Birth = date(2026, time.November, 1)
	_, err = w.svc.Create(context.Background(), w.env.Kavya, in)
	wantFieldError(t, err, msgBirthFuture)
	w.wantNoCode(t, "EMP-009")

	in.Birth = date(1990, time.May, 14)
	saved := w.create(t, in)
	if e := w.get(t, saved.ID); e.Birth != in.Birth {
		t.Errorf("saved Birth = %v; want %v", e.Birth, in.Birth)
	}
}

// TestEmployee_DOBTodayIST is Review Focus 4: just after midnight IST the
// business date is already 2026-10-24 while UTC is still the 23rd.
func TestEmployee_DOBTodayIST(t *testing.T) {
	w := newWorld(t)
	w.env.Clock.Set(time.Date(2026, time.October, 23, 19, 0, 0, 0, time.UTC)) // 24-10-2026 00:30 IST

	tomorrow := w.input("EMP-010", "Baby Tomorrow")
	tomorrow.Birth = date(2026, time.October, 25)
	_, err := w.svc.Create(context.Background(), w.env.Kavya, tomorrow)
	wantFieldError(t, err, msgBirthFuture)

	in := w.input("EMP-009", "Baby Today")
	in.Birth = date(2026, time.October, 24)
	saved, err := w.svc.Create(context.Background(), w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(birth 2026-10-24 at 00:30 IST) = %v; want saved", err)
	}
	if e := w.get(t, saved.ID); e.Birth != in.Birth {
		t.Errorf("saved Birth = %v; want %v", e.Birth, in.Birth)
	}
}

func TestEmployee_LeftNeedsRelievingDate(t *testing.T) { // MST-118
	in := validInput()
	in.Status = employee.Left
	_, err := employee.Validate(in)
	wantFieldError(t, err, msgEnterRelieving)

	in.Relieving = date(2026, time.March, 31)
	got, err := employee.Validate(in)
	if err != nil {
		t.Fatalf("Validate(Left, relieving 2026-03-31) = %v; want nil", err)
	}
	if got.Status != employee.Left || got.Relieving != in.Relieving {
		t.Errorf("Validate(Left) = {Status %q, Relieving %v}; want {Left, %v}", got.Status, got.Relieving, in.Relieving)
	}
}

func TestEmployee_UserUniqueAmongActive(t *testing.T) { // MST-111
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	ctx := context.Background()
	deepak := userID(t, "deepak@vardhan.test")

	in := w.input("EMP-009", "Deepak Nair")
	in.UserID = deepak
	_, err := w.svc.Create(ctx, w.env.Kavya, in)
	wantFieldError(t, err, msgUserLinked)
	w.wantNoCode(t, "EMP-009")

	// EMP-007 leaves (disabling the user, MST-119); an Administrator
	// re-enables deepak; now EMP-009 may take the user.
	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Fatalf("deepak enabled after EMP-007 Left; want disabled (MST-119)")
	}
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		return w.env.Kit.Auth.EnableUser(ctx, tx, deepak)
	}); err != nil {
		t.Fatalf("EnableUser(deepak): %v", err)
	}
	saved := w.create(t, in)
	if e := w.get(t, saved.ID); e.UserID != deepak || e.Status != employee.Active {
		t.Errorf("EMP-009 = {UserID %d, Status %q}; want {%d, Active}", e.UserID, e.Status, deepak)
	}

	// The partial unique index (CD-39) refuses a second active row for the
	// same user even when the service is bypassed: copy EMP-009's row with a
	// new id and code.
	err = w.env.DB.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		for _, stmt := range []string{
			`CREATE TEMP TABLE mst14_copy AS SELECT * FROM employee WHERE code = 'EMP-009'`,
			`UPDATE mst14_copy SET id = NULL, code = 'EMP-010'`,
			`INSERT INTO employee SELECT * FROM mst14_copy`,
		} {
			if _, err := q.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE") || !strings.Contains(err.Error(), "user_id") {
		t.Errorf("direct insert of a second active employee for deepak: err = %v; want a UNIQUE failure on employee.user_id", err)
	}
	w.wantNoCode(t, "EMP-010")
}

func TestEmployee_LeftDisablesUser(t *testing.T) { // MST-119
	w := newWorld(t)
	w.seedFixtureEmployees(t)
	deepak := userID(t, "deepak@vardhan.test")
	id := w.ids["EMP-007"]
	if !w.userEnabled(t, deepak) {
		t.Fatalf("deepak disabled before the test; want enabled")
	}

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Left, date(2026, time.October, 24)
	}); err != nil {
		t.Fatalf("mark EMP-007 Left: %v", err)
	}
	if w.userEnabled(t, deepak) {
		t.Errorf("deepak enabled after EMP-007 Left; want disabled")
	}
	if e := w.get(t, id); e.Status != employee.Left || !e.Disabled {
		t.Errorf("EMP-007 after Left = {Status %q, Disabled %v}; want {Left, true}", e.Status, e.Disabled)
	}
	if !hasChange(w.entries(t, employee.DocType, id), "status", "Active", "Left") {
		t.Errorf("Employee EMP-007 audit has no status Active → Left change")
	}
	if !hasAction(w.entries(t, "User", int64(deepak)), "disable") {
		t.Errorf("User deepak audit has no disable entry")
	}

	if err := w.update(t, "EMP-007", func(in *employee.Input) {
		in.Status, in.Relieving = employee.Active, types.Date{}
	}); err != nil {
		t.Fatalf("mark EMP-007 Active again: %v", err)
	}
	if !w.userEnabled(t, deepak) {
		t.Errorf("deepak disabled after EMP-007 Active again; want enabled")
	}
	if e := w.get(t, id); e.Status != employee.Active || e.Disabled {
		t.Errorf("EMP-007 after Active = {Status %q, Disabled %v}; want {Active, false}", e.Status, e.Disabled)
	}
	if !hasChange(w.entries(t, employee.DocType, id), "status", "Left", "Active") {
		t.Errorf("Employee EMP-007 audit has no status Left → Active change")
	}
	if !hasAction(w.entries(t, "User", int64(deepak)), "enable") {
		t.Errorf("User deepak audit has no enable entry")
	}
}

// world is msttest with the company and branch PUNE created through the
// company service, and an employee Service over it.
type world struct {
	env      *msttest.Env
	svc      *employee.Service
	branchID int64
	ids      map[string]int64
	ins      map[string]employee.Input
	versions map[string]int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	env := msttest.New(t)
	co := company.New(env.Kit)
	if err := co.SaveCompany(ctx, env.Kavya, company.CompanyInput{LegalName: "Vardhan Infra Projects Pvt Ltd", PAN: "AAFCV2449G"}, 0); err != nil {
		t.Fatalf("SaveCompany: %v", err)
	}
	b, err := co.Create(ctx, env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AAFCV2449G1Z7", IsHeadOffice: true})
	if err != nil {
		t.Fatalf("create branch PUNE: %v", err)
	}
	return &world{
		env: env, svc: employee.New(env.Kit), branchID: b.ID,
		ids: map[string]int64{}, ins: map[string]employee.Input{}, versions: map[string]int{},
	}
}

// input is an active employee at branch PUNE, joined 2024-04-01 (M9).
func (w *world) input(code, name string) employee.Input {
	return employee.Input{Code: code, Name: name, BranchID: w.branchID, Joining: joining, Status: employee.Active}
}

// create saves in and records its id, input and version under its code.
func (w *world) create(t *testing.T, in employee.Input) mstkit.Saved {
	t.Helper()
	s, err := w.svc.Create(context.Background(), w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(%s): %v", in.Code, err)
	}
	w.ids[in.Code], w.ins[in.Code], w.versions[in.Code] = s.ID, in, s.Version
	return s
}

// update saves code's last input changed by edit, at the version last saved.
// On success the new input and version are kept.
func (w *world) update(t *testing.T, code string, edit func(*employee.Input)) error {
	t.Helper()
	in := w.ins[code]
	edit(&in)
	s, err := w.svc.Update(context.Background(), w.env.Kavya, w.ids[code], w.versions[code], in)
	if err != nil {
		return err
	}
	w.ins[code], w.versions[code] = in, s.Version
	return nil
}

// seedFixtureEmployees creates EMP-001…EMP-008 as 00-fixtures.md lists them
// (users, reporting managers; EMP-008 Left, relieving 2026-03-31, M9).
func (w *world) seedFixtureEmployees(t *testing.T) {
	t.Helper()
	for _, f := range []struct{ code, name, email, manager string }{
		{"EMP-001", "Anil Kapoor", "admin@vardhan.test", ""},
		{"EMP-002", "Meena Iyer", "meena@vardhan.test", ""},
		{"EMP-003", "Ravi Kulkarni", "ravi@vardhan.test", "EMP-002"},
		{"EMP-004", "Priya Shah", "priya@vardhan.test", "EMP-003"},
		{"EMP-005", "Suresh Patil", "suresh@vardhan.test", "EMP-003"},
		{"EMP-006", "Kavya Rao", "kavya@vardhan.test", "EMP-002"},
		{"EMP-007", "Deepak Nair", "deepak@vardhan.test", "EMP-004"},
		{"EMP-008", "Former Staff", "old@vardhan.test", ""},
	} {
		in := w.input(f.code, f.name)
		in.UserID = userID(t, f.email)
		if f.manager != "" {
			in.ManagerID = w.ids[f.manager]
		}
		if f.code == "EMP-008" {
			in.Status, in.Relieving = employee.Left, date(2026, time.March, 31)
		}
		w.create(t, in)
	}
}

func (w *world) get(t *testing.T, id int64) employee.Employee {
	t.Helper()
	var e employee.Employee
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		e, err = w.svc.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return e
}

// wantNoCode fails when an employee with code exists.
func (w *world) wantNoCode(t *testing.T, code string) {
	t.Helper()
	var ok bool
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		_, ok, err = w.svc.ByCode(context.Background(), tx, code)
		return err
	}); err != nil {
		t.Fatalf("ByCode(%s): %v", code, err)
	}
	if ok {
		t.Errorf("employee %s exists; want none after the refused save", code)
	}
}

func (w *world) userEnabled(t *testing.T, u auth.UserID) bool {
	t.Helper()
	var info auth.UserInfo
	var ok bool
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		info, ok, err = w.env.Kit.Auth.User(context.Background(), tx, u)
		return err
	}); err != nil || !ok {
		t.Fatalf("User(%d) = (%+v, %v, %v); want the user", u, info, ok, err)
	}
	return info.Enabled
}

func (w *world) entries(t *testing.T, docType string, id int64) []audit.Entry {
	t.Helper()
	var es []audit.Entry
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		es, err = audit.ForDoc(context.Background(), tx, docType, id)
		return err
	}); err != nil {
		t.Fatalf("audit.ForDoc(%s, %d): %v", docType, id, err)
	}
	return es
}

func hasChange(es []audit.Entry, field, before, after string) bool {
	for _, e := range es {
		for _, c := range e.Changes {
			if c.Field == field && c.Before == before && c.After == after {
				return true
			}
		}
	}
	return false
}

func hasAction(es []audit.Entry, action string) bool {
	for _, e := range es {
		if e.Action == action {
			return true
		}
	}
	return false
}

func userID(t *testing.T, email string) auth.UserID {
	t.Helper()
	return testkit.Principal(t, email).ID
}
