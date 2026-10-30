package project_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
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

// restrictPriya gives Purchase User (Priya) rights on Project through the AUTH
// matrix and restricts Priya to exactly projectID (the id the project service
// gave PRJ-METRO), through AUTH as Admin.
func (w *world) restrictPriya(t *testing.T, rights map[auth.Right]auth.Scope, projectID int64) {
	t.Helper()
	ctx := context.Background()
	a, ok := w.env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("msttest Kit.Auth is %T; want *auth.Auth", w.env.Kit.Auth)
	}
	if err := a.SetRights(ctx, w.env.Admin, "Purchase User", project.DocType, rights); err != nil {
		t.Fatalf("SetRights(Purchase User, Project): %v", err)
	}
	before, err := a.UserRestrictions(ctx, w.env.Priya.ID)
	if err != nil {
		t.Fatalf("UserRestrictions(Priya): %v", err)
	}
	if err := a.ChangeRestrictions(ctx, w.env.Admin, w.env.Priya.ID, auth.DimProject, []int64{projectID}, before[auth.DimProject], nil); err != nil {
		t.Fatalf("ChangeRestrictions(Priya, project %d): %v", projectID, err)
	}
	got, err := a.UserRestrictions(ctx, w.env.Priya.ID)
	if err != nil {
		t.Fatalf("UserRestrictions(Priya): %v", err)
	}
	if ids := got[auth.DimProject]; len(ids) != 1 || ids[0] != projectID {
		t.Fatalf("Priya's project restriction = %v; want [%d]", ids, projectID)
	}
}

// TestProject_DisabledCustomer has no case ID: a disabled customer is refused
// on create and on update, as a disabled branch is, and can be cleared (CD-170).
func TestProject_DisabledCustomer(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	kavya := w.env.Kavya
	parties := party.New(w.env.Kit)
	c, err := parties.Create(ctx, kavya, party.Input{Code: "SHREE", Name: "Shree Steel Traders", IsCustomer: true})
	if err != nil {
		t.Fatal(err)
	}
	in := validInput()
	in.CustomerID = c.ID
	saved := w.create(t, in)
	if err := parties.SetDisabled(ctx, kavya, c.ID, c.Version, true); err != nil {
		t.Fatal(err)
	}
	const msg = "Customer SHREE is disabled. Pick another customer."

	in.BranchID = w.branchID
	in.Code = "PRJ-NEW"
	_, err = w.svc.Create(ctx, kavya, in)
	wantOneFieldError(t, err, "customer_id", msg)

	in.Code = prjCode
	_, err = w.svc.Update(ctx, kavya, saved.ID, saved.Version, in)
	wantOneFieldError(t, err, "customer_id", msg)

	in.CustomerID = 0
	if _, err := w.svc.Update(ctx, kavya, saved.ID, saved.Version, in); err != nil {
		t.Errorf("Update clearing the disabled customer = %v; want nil", err)
	}
}

// TestProject_RestrictionAppliesToWrites: a user restricted to PRJ-METRO with
// write on Project can update and disable PRJ-METRO itself, and another
// project is not found to them (CD-64, ProjectFields {"id"}).
func TestProject_RestrictionAppliesToWrites(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	metro := w.create(t, validInput())
	tech := w.create(t, project.Input{Code: "PRJ-TECHPARK", Name: "Whitefield Tech Park, Bengaluru"})
	w.restrictPriya(t, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}, metro.ID)
	priya := w.env.Priya

	in := validInput()
	in.BranchID = w.branchID
	in.Name = "Pune Metro Depot Phase 2"
	saved, err := w.svc.Update(ctx, priya, metro.ID, metro.Version, in)
	if err != nil {
		t.Fatalf("Priya Update(PRJ-METRO, her own project) = %v; want nil", err)
	}
	if err := w.svc.SetDisabled(ctx, priya, metro.ID, saved.Version, true); err != nil {
		t.Fatalf("Priya SetDisabled(PRJ-METRO, her own project) = %v; want nil", err)
	}
	if p := w.get(t, metro.ID); p.Name != in.Name || !p.Disabled {
		t.Errorf("PRJ-METRO after Priya's writes = {Name %q, Disabled %v}; want {%q, true}", p.Name, p.Disabled, in.Name)
	}

	other := project.Input{Code: "PRJ-TECHPARK", Name: "Tech Park renamed", BranchID: w.branchID}
	if _, err := w.svc.Update(ctx, priya, tech.ID, tech.Version, other); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Priya Update(PRJ-TECHPARK) = %v; want doctype.ErrNotFound", err)
	}
	if err := w.svc.SetDisabled(ctx, priya, tech.ID, tech.Version, true); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Priya SetDisabled(PRJ-TECHPARK) = %v; want doctype.ErrNotFound", err)
	}
	if p := w.get(t, tech.ID); p.Name != "Whitefield Tech Park, Bengaluru" || p.Disabled {
		t.Errorf("PRJ-TECHPARK after Priya's refused writes = {Name %q, Disabled %v}; want unchanged", p.Name, p.Disabled)
	}
}

// TestProject_RestrictedUserCannotCreate: a user restricted on Project can't
// create a project, even with the create right (CD-172); an unrestricted
// user with create (Kavya) still can.
func TestProject_RestrictedUserCannotCreate(t *testing.T) {
	const msg = "You can only work with your assigned projects. Ask your administrator to add this project."
	ctx := context.Background()
	w := newWorld(t)
	metro := w.create(t, validInput())
	w.restrictPriya(t, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll}, metro.ID)

	in := project.Input{Code: "PRJ-NEW", Name: "New Project", BranchID: w.branchID}
	_, err := w.svc.Create(ctx, w.env.Priya, in)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Priya Create(PRJ-NEW) = %v; want auth.ErrForbidden", err)
	}
	if err.Error() != msg {
		t.Errorf("Priya Create(PRJ-NEW) message = %q; want %q", err.Error(), msg)
	}
	var st interface{ Status() int }
	if !errors.As(err, &st) || st.Status() != 403 {
		t.Errorf("Priya Create(PRJ-NEW) = %v (%T); want status 403", err, err)
	}
	var found bool
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		_, found, err = w.svc.ByCode(ctx, tx, "PRJ-NEW")
		return err
	}); err != nil {
		t.Fatalf("ByCode(PRJ-NEW): %v", err)
	}
	if found {
		t.Errorf("PRJ-NEW exists after Priya's refused Create; want no row")
	}

	if _, err := w.svc.Create(ctx, w.env.Kavya, in); err != nil {
		t.Errorf("Kavya Create(PRJ-NEW) = %v; want nil", err)
	}
}

// TestProject_BranchLinkMessages: the branch link is checked in the write tx
// with CD-170's texts: blank is MST-104's "Branch is required.", an unknown id
// is DOC-032's pattern, a disabled branch names its code.
func TestProject_BranchLinkMessages(t *testing.T) {
	const (
		requiredMsg = "Branch is required."
		unknownMsg  = "Branch not found. Pick one from the list."
		disabledMsg = "Branch BLR is disabled. Pick another branch."
	)
	ctx := context.Background()
	w := newWorld(t)
	co := company.New(w.env.Kit)
	blr, err := co.Create(ctx, w.env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: "29AAFCV2449G1Z3"})
	if err != nil {
		t.Fatalf("create branch BLR: %v", err)
	}
	if err := co.SetDisabled(ctx, w.env.Kavya, blr.ID, blr.Version, true); err != nil {
		t.Fatalf("disable branch BLR: %v", err)
	}

	withBranch := func(id int64) project.Input {
		in := validInput()
		in.BranchID = id
		return in
	}
	t.Run("blank", func(t *testing.T) {
		_, err := w.svc.Create(ctx, w.env.Kavya, withBranch(0))
		wantOneFieldError(t, err, "branch_id", requiredMsg)
	})
	t.Run("unknown", func(t *testing.T) {
		_, err := w.svc.Create(ctx, w.env.Kavya, withBranch(blr.ID+1000))
		wantOneFieldError(t, err, "branch_id", unknownMsg)
	})
	t.Run("disabled", func(t *testing.T) {
		_, err := w.svc.Create(ctx, w.env.Kavya, withBranch(blr.ID))
		wantOneFieldError(t, err, "branch_id", disabledMsg)
	})

	// The same checks on Update of a saved project.
	tech := project.Input{Code: "PRJ-TECHPARK", Name: "Whitefield Tech Park, Bengaluru"}
	saved := w.create(t, tech)
	techOn := func(id int64) project.Input {
		in := tech
		in.BranchID = id
		return in
	}
	t.Run("update unknown", func(t *testing.T) {
		_, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, saved.Version, techOn(blr.ID+1000))
		wantOneFieldError(t, err, "branch_id", unknownMsg)
	})
	t.Run("update disabled", func(t *testing.T) {
		_, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, saved.Version, techOn(blr.ID))
		wantOneFieldError(t, err, "branch_id", disabledMsg)
	})
	if p := w.get(t, saved.ID); p.BranchID != w.branchID || p.Version != saved.Version {
		t.Errorf("project after refused updates = {BranchID %d, Version %d}; want {%d, %d}", p.BranchID, p.Version, w.branchID, saved.Version)
	}
}
