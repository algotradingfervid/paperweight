package project_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Unit tests use the literal 06-masters.md values (MST-100, MST-101); the
// lookup test uses the canonical fixtures (M7: PRJ-METRO, branch PUNE).
const (
	prjCode = "PRJ-METRO"
	prjName = "Pune Metro Depot"

	datesMsg  = "End date can't be before start date."
	statusMsg = "Status must be Open, On Hold, Completed or Cancelled."
)

// validInput is PRJ-METRO with every field Validate needs; tests vary one.
func validInput() project.Input {
	return project.Input{Code: prjCode, Name: prjName, BranchID: 1}
}

// wantOneFieldError checks err is a 422 *mstkit.ValidationError carrying
// exactly one header field error with msg, on field when field != "" (else
// on any named field: the plan doesn't name the date column).
func wantOneFieldError(t *testing.T, err error, field, msg string) {
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
	if field != "" && got[0].Field != field {
		t.Errorf("FieldErrors()[0].Field = %q; want %q", got[0].Field, field)
	}
	if got[0].Field == "" {
		t.Errorf("FieldErrors()[0].Field is empty; want the field the error belongs to")
	}
}

func date(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

func TestProject_Dates(t *testing.T) { // MST-100
	in := validInput()
	in.Start, in.End = date(2026, time.July, 1), date(2026, time.June, 30)
	_, err := project.Validate(in)
	wantOneFieldError(t, err, "", datesMsg)

	ok := []struct {
		name       string
		start, end types.Date
	}{
		{"end after start", date(2026, time.July, 1), date(2027, time.March, 31)},
		{"end on start", date(2026, time.July, 1), date(2026, time.July, 1)},
		{"no end", date(2026, time.July, 1), types.Date{}},
		{"no start", types.Date{}, date(2026, time.June, 30)},
		{"no dates", types.Date{}, types.Date{}},
	}
	for _, c := range ok {
		t.Run(c.name, func(t *testing.T) {
			in := validInput()
			in.Start, in.End = c.start, c.end
			got, err := project.Validate(in)
			if err != nil {
				t.Fatalf("Validate(start %v, end %v) = %v; want nil", c.start, c.end, err)
			}
			if got.Start != c.start || got.End != c.end {
				t.Errorf("Validate dates = (%v, %v); want (%v, %v)", got.Start, got.End, c.start, c.end)
			}
		})
	}
}

func TestProject_Status(t *testing.T) { // MST-101
	// The four values and their display text (M13 Options order).
	want := map[project.Status]string{
		project.Open: "Open", project.OnHold: "On Hold",
		project.Completed: "Completed", project.Cancelled: "Cancelled",
	}
	for s, text := range want {
		if string(s) != text {
			t.Errorf("Status constant = %q; want %q", s, text)
		}
	}

	// A new project (no status given) is Open.
	got, err := project.Validate(validInput())
	if err != nil {
		t.Fatalf("Validate(no status) = %v; want nil", err)
	}
	if got.Status != project.Open {
		t.Errorf("Validate(no status).Status = %q; want %q", got.Status, project.Open)
	}

	for _, s := range []project.Status{project.Open, project.OnHold, project.Completed, project.Cancelled} {
		in := validInput()
		in.Status = s
		got, err := project.Validate(in)
		if err != nil {
			t.Errorf("Validate(status %q) = %v; want nil", s, err)
		} else if got.Status != s {
			t.Errorf("Validate(status %q).Status = %q; want unchanged", s, got.Status)
		}
	}

	in := validInput()
	in.Status = "Done"
	_, err = project.Validate(in)
	wantOneFieldError(t, err, "status", statusMsg)

	// Saved through the service, a new project reads back as Open.
	w := newWorld(t)
	saved := w.create(t, validInput())
	if p := w.get(t, saved.ID); p.Status != project.Open {
		t.Errorf("created project Status = %q; want %q", p.Status, project.Open)
	}
}

// TestProject_LookupStatusMessages pins the Master lookup texts (Task 12
// Produces; MST-102 relies on them): Completed and Cancelled block a new
// document, On Hold warns, Open does neither.
func TestProject_LookupStatusMessages(t *testing.T) {
	w := newWorld(t)
	dt, ok := doctype.Lookup(project.DocType)
	if !ok || dt.Master == nil {
		t.Fatalf("doctype.Lookup(%q) = (%v, %v); want a registered master", project.DocType, dt, ok)
	}
	if project.DocType != "Project" {
		t.Errorf("project.DocType = %q; want Project", project.DocType)
	}

	in := validInput()
	in.BranchID = w.branchID
	saved := w.create(t, in)
	id, version := saved.ID, saved.Version

	cases := []struct {
		status           project.Status
		blocked, warning string
	}{
		{project.Open, "", ""},
		{project.OnHold, "", "Project PRJ-METRO is On Hold."},
		{project.Completed, "Project PRJ-METRO is Completed. Reopen it or pick another project.", ""},
		{project.Cancelled, "Project PRJ-METRO is Cancelled. Reopen it or pick another project.", ""},
		{project.Open, "", ""}, // reopened
	}
	for i, c := range cases {
		if i > 0 {
			in.Status = c.status
			s, err := w.svc.Update(context.Background(), w.env.Kavya, id, version, in)
			if err != nil {
				t.Fatalf("Update(status %q): %v", c.status, err)
			}
			version = s.Version
		}
		var info doctype.MasterInfo
		var found bool
		if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
			var err error
			info, found, err = dt.Master.Lookup(context.Background(), tx, id)
			return err
		}); err != nil {
			t.Fatalf("Lookup(status %q): %v", c.status, err)
		}
		want := doctype.MasterInfo{Code: prjCode, Name: prjName, Blocked: c.blocked, Warning: c.warning}
		if !found || info != want {
			t.Errorf("Lookup(status %q) = (%+v, %v); want (%+v, true)", c.status, info, found, want)
		}
	}
}

// world is msttest with the company and branch PUNE created through the
// company service, and a project Service over it.
type world struct {
	env      *msttest.Env
	svc      *project.Service
	branchID int64
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
	return &world{env: env, svc: project.New(env.Kit), branchID: b.ID}
}

func (w *world) create(t *testing.T, in project.Input) mstkit.Saved {
	t.Helper()
	in.BranchID = w.branchID
	s, err := w.svc.Create(context.Background(), w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(%s): %v", in.Code, err)
	}
	return s
}

func (w *world) get(t *testing.T, id int64) project.Project {
	t.Helper()
	var p project.Project
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		p, err = w.svc.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return p
}
