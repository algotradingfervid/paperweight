package site_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MST-105 literal values (06-masters.md): PUNE-WH, type warehouse, branch PUNE.
const (
	whCode = "PUNE-WH"
	whName = "Pune Warehouse"

	branchMsg = "Branch is required."
	// 06-masters.md MST-105 has the text, so it wins over CD-166's
	// "Site type must be …" (CD-166: "Where 06-masters has a text, 06 wins.").
	typeMsg = "Type must be Warehouse or Site."
)

// validInput is PUNE-WH with every field Validate needs; tests vary one.
func validInput() site.Input {
	return site.Input{Code: whCode, Name: whName, BranchID: 1, Type: site.Warehouse}
}

// wantOneFieldError checks err is a 422 *mstkit.ValidationError carrying
// exactly one header field error with msg on field.
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
	if got[0].Message != msg || got[0].Line != 0 || got[0].Field != field {
		t.Errorf("FieldErrors()[0] = %+v; want header error on %q: %q", got[0], field, msg)
	}
}

func TestSite_RequiredFields(t *testing.T) { // MST-105
	if site.DocType != "Site" {
		t.Errorf("site.DocType = %q; want Site", site.DocType)
	}
	if site.Warehouse != "warehouse" || site.SiteKind != "site" {
		t.Errorf("SiteType constants = (%q, %q); want (warehouse, site)", site.Warehouse, site.SiteKind)
	}

	// PUNE-WH, type warehouse, branch PUNE: valid. A site is valid too.
	for _, typ := range []site.SiteType{site.Warehouse, site.SiteKind} {
		in := validInput()
		in.Type = typ
		got, err := site.Validate(in)
		if err != nil {
			t.Errorf("Validate(type %q) = %v; want nil", typ, err)
			continue
		}
		if got.Code != whCode || got.Name != whName || got.BranchID != 1 || got.Type != typ {
			t.Errorf("Validate(type %q) = %+v; want the input unchanged", typ, got)
		}
	}

	// Missing branch.
	in := validInput()
	in.BranchID = 0
	_, err := site.Validate(in)
	wantOneFieldError(t, err, "branch_id", branchMsg)

	// Type other than warehouse/site, including none (CD-166: empty or not
	// one of the Options is refused in Validate, never by the table CHECK).
	for _, typ := range []site.SiteType{"depot", ""} {
		in := validInput()
		in.Type = typ
		_, err := site.Validate(in)
		t.Run("type "+string(typ), func(t *testing.T) { wantOneFieldError(t, err, "type", typeMsg) })
	}

	// Through the service: PUNE-WH at branch PUNE saves and reads back;
	// the same rules refuse a save without a branch or with type depot.
	w := newWorld(t)
	ctx := context.Background()
	in = validInput()
	in.BranchID = w.branchID
	saved, err := w.svc.Create(ctx, w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(%s warehouse at PUNE) = %v; want saved", whCode, err)
	}
	var got site.Site
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		got, err = w.svc.Get(ctx, tx, saved.ID)
		return err
	}); err != nil {
		t.Fatalf("Get(%d): %v", saved.ID, err)
	}
	if got.ID != saved.ID || got.Code != whCode || got.Name != whName || got.BranchID != w.branchID ||
		got.ProjectID != 0 || got.Type != site.Warehouse || got.Disabled || got.Version != saved.Version {
		t.Errorf("Get = %+v; want %s %q, branch %d, no project, type warehouse, enabled, version %d",
			got, whCode, whName, w.branchID, saved.Version)
	}

	noBranch := validInput()
	noBranch.Code, noBranch.BranchID = "PUNE-WH2", 0
	_, err = w.svc.Create(ctx, w.env.Kavya, noBranch)
	wantOneFieldError(t, err, "branch_id", branchMsg)

	depot := validInput()
	depot.Code, depot.BranchID, depot.Type = "PUNE-DEPOT", w.branchID, "depot"
	_, err = w.svc.Create(ctx, w.env.Kavya, depot)
	wantOneFieldError(t, err, "type", typeMsg)
}

// world is msttest with the company and branch PUNE created through the
// company service, and a site Service over it.
type world struct {
	env      *msttest.Env
	svc      *site.Service
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
	return &world{env: env, svc: site.New(env.Kit), branchID: b.ID}
}

func (w *world) create(t *testing.T, in site.Input) mstkit.Saved {
	t.Helper()
	s, err := w.svc.Create(context.Background(), w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Create(%s): %v", in.Code, err)
	}
	return s
}

// find reads the site with id; found is false when there is none.
func (w *world) find(t *testing.T, id int64) (site.Site, bool) {
	t.Helper()
	var st site.Site
	err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		st, err = w.svc.Get(context.Background(), tx, id)
		return err
	})
	if errors.Is(err, doctype.ErrNotFound) {
		return site.Site{}, false
	}
	if err != nil {
		t.Fatalf("Get(%d): %v", id, err)
	}
	return st, true
}

func (w *world) get(t *testing.T, id int64) site.Site {
	t.Helper()
	st, ok := w.find(t, id)
	if !ok {
		t.Fatalf("Get(%d) = doctype.ErrNotFound; want the site", id)
	}
	return st
}

// restrictPriya gives Purchase User (Priya) rights on Site through the AUTH
// matrix and restricts Priya to exactly siteID (the id the site service gave
// the site), through AUTH as Admin.
func (w *world) restrictPriya(t *testing.T, rights map[auth.Right]auth.Scope, siteID int64) {
	t.Helper()
	ctx := context.Background()
	a, ok := w.env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("msttest Kit.Auth is %T; want *auth.Auth", w.env.Kit.Auth)
	}
	if err := a.SetRights(ctx, w.env.Admin, "Purchase User", site.DocType, rights); err != nil {
		t.Fatalf("SetRights(Purchase User, Site): %v", err)
	}
	before, err := a.UserRestrictions(ctx, w.env.Priya.ID)
	if err != nil {
		t.Fatalf("UserRestrictions(Priya): %v", err)
	}
	if err := a.ChangeRestrictions(ctx, w.env.Admin, w.env.Priya.ID, auth.DimSite, []int64{siteID}, before[auth.DimSite], nil); err != nil {
		t.Fatalf("ChangeRestrictions(Priya, site %d): %v", siteID, err)
	}
	got, err := a.UserRestrictions(ctx, w.env.Priya.ID)
	if err != nil {
		t.Fatalf("UserRestrictions(Priya): %v", err)
	}
	if ids := got[auth.DimSite]; len(ids) != 1 || ids[0] != siteID {
		t.Fatalf("Priya's site restriction = %v; want [%d]", ids, siteID)
	}
}

// TestSite_RestrictionAppliesToWrites: a user restricted to PUNE-WH with
// write (and delete) on Site can update, disable and delete PUNE-WH itself,
// and another site is not found to them and stays unchanged (CD-64,
// SiteFields {"id"}).
func TestSite_RestrictionAppliesToWrites(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	wh := validInput()
	wh.BranchID = w.branchID
	own := w.create(t, wh)
	yardIn := site.Input{Code: "PUNE-YARD", Name: "Pune Yard", BranchID: w.branchID, Type: site.SiteKind}
	yard := w.create(t, yardIn)
	w.restrictPriya(t, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll, auth.RightDelete: auth.ScopeAll,
	}, own.ID)
	priya := w.env.Priya

	// Her own site: update, disable, then (a separate write) delete.
	in := wh
	in.Name = "Pune Warehouse 2"
	saved, err := w.svc.Update(ctx, priya, own.ID, own.Version, in)
	if err != nil {
		t.Fatalf("Priya Update(PUNE-WH, her own site) = %v; want nil", err)
	}
	if err := w.svc.SetDisabled(ctx, priya, own.ID, saved.Version, true); err != nil {
		t.Fatalf("Priya SetDisabled(PUNE-WH, her own site) = %v; want nil", err)
	}
	got := w.get(t, own.ID)
	if got.Name != in.Name || !got.Disabled {
		t.Errorf("PUNE-WH after Priya's writes = {Name %q, Disabled %v}; want {%q, true}", got.Name, got.Disabled, in.Name)
	}

	// Another site: every write is not found, and the row is unchanged.
	other := yardIn
	other.Name = "Pune Yard renamed"
	if _, err := w.svc.Update(ctx, priya, yard.ID, yard.Version, other); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Priya Update(PUNE-YARD) = %v; want doctype.ErrNotFound", err)
	}
	if err := w.svc.SetDisabled(ctx, priya, yard.ID, yard.Version, true); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Priya SetDisabled(PUNE-YARD) = %v; want doctype.ErrNotFound", err)
	}
	if err := w.svc.Delete(ctx, priya, yard.ID, yard.Version); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Priya Delete(PUNE-YARD) = %v; want doctype.ErrNotFound", err)
	}
	if st, ok := w.find(t, yard.ID); !ok || st.Name != yardIn.Name || st.Disabled || st.Version != yard.Version {
		t.Errorf("PUNE-YARD after Priya's refused writes = (%+v, found %v); want {Name %q, enabled, Version %d}, found",
			st, ok, yardIn.Name, yard.Version)
	}

	if err := w.svc.Delete(ctx, priya, own.ID, got.Version); err != nil {
		t.Fatalf("Priya Delete(PUNE-WH, her own site) = %v; want nil", err)
	}
	if _, ok := w.find(t, own.ID); ok {
		t.Errorf("PUNE-WH exists after Priya's Delete; want no row")
	}
}

// TestSite_RestrictedUserCannotCreate: a user restricted on Site can't create
// a site, even with the create right (CD-172); an unrestricted user with
// create (Kavya) still can.
func TestSite_RestrictedUserCannotCreate(t *testing.T) {
	const msg = "You can only work with your assigned sites. Ask your administrator to add this site."
	ctx := context.Background()
	w := newWorld(t)
	wh := validInput()
	wh.BranchID = w.branchID
	own := w.create(t, wh)
	w.restrictPriya(t, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll}, own.ID)

	in := site.Input{Code: "PUNE-NEW", Name: "New Site", BranchID: w.branchID, Type: site.SiteKind}
	_, err := w.svc.Create(ctx, w.env.Priya, in)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Priya Create(PUNE-NEW) = %v; want auth.ErrForbidden", err)
	}
	if err.Error() != msg {
		t.Errorf("Priya Create(PUNE-NEW) message = %q; want %q", err.Error(), msg)
	}
	var st interface{ Status() int }
	if !errors.As(err, &st) || st.Status() != 403 {
		t.Errorf("Priya Create(PUNE-NEW) = %v (%T); want status 403", err, err)
	}
	var found bool
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		_, found, err = w.svc.ByCode(ctx, tx, "PUNE-NEW")
		return err
	}); err != nil {
		t.Fatalf("ByCode(PUNE-NEW): %v", err)
	}
	if found {
		t.Errorf("PUNE-NEW exists after Priya's refused Create; want no row")
	}

	if _, err := w.svc.Create(ctx, w.env.Kavya, in); err != nil {
		t.Errorf("Kavya Create(PUNE-NEW) = %v; want nil", err)
	}
}

// TestSite_LinkMessages: the branch and project links are checked in the
// write tx with CD-170's texts, on Create and on Update: an unknown id is
// DOC-032's pattern, a disabled one names its code.
func TestSite_LinkMessages(t *testing.T) {
	const (
		branchUnknown   = "Branch not found. Pick one from the list."
		branchDisabled  = "Branch BLR is disabled. Pick another branch."
		projectUnknown  = "Project not found. Pick one from the list."
		projectDisabled = "Project PRJ-METRO is disabled. Pick another project."
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
	prj := project.New(w.env.Kit)
	metro, err := prj.Create(ctx, w.env.Kavya, project.Input{Code: "PRJ-METRO", Name: "Pune Metro Depot", BranchID: w.branchID})
	if err != nil {
		t.Fatalf("create project PRJ-METRO: %v", err)
	}
	if err := prj.SetDisabled(ctx, w.env.Kavya, metro.ID, metro.Version, true); err != nil {
		t.Fatalf("disable project PRJ-METRO: %v", err)
	}

	cases := []struct {
		name, field, msg string
		branchID         int64 // 0 = PUNE
		projectID        int64
	}{
		{"branch unknown", "branch_id", branchUnknown, blr.ID + 1000, 0},
		{"branch disabled", "branch_id", branchDisabled, blr.ID, 0},
		{"project unknown", "project_id", projectUnknown, 0, metro.ID + 1000},
		{"project disabled", "project_id", projectDisabled, 0, metro.ID},
	}
	with := func(code string, branchID, projectID int64) site.Input {
		in := validInput()
		in.Code, in.BranchID, in.ProjectID = code, w.branchID, projectID
		if branchID != 0 {
			in.BranchID = branchID
		}
		return in
	}
	for _, c := range cases {
		t.Run("create "+c.name, func(t *testing.T) {
			_, err := w.svc.Create(ctx, w.env.Kavya, with(whCode, c.branchID, c.projectID))
			wantOneFieldError(t, err, c.field, c.msg)
		})
	}

	// The same checks on Update of a saved site (its own code, so a create
	// above that wrongly saved can't stop this half); the row doesn't move.
	const storeCode = "PUNE-STORE"
	saved := w.create(t, with(storeCode, 0, 0))
	for _, c := range cases {
		t.Run("update "+c.name, func(t *testing.T) {
			_, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, saved.Version, with(storeCode, c.branchID, c.projectID))
			wantOneFieldError(t, err, c.field, c.msg)
		})
	}
	if st := w.get(t, saved.ID); st.BranchID != w.branchID || st.ProjectID != 0 || st.Version != saved.Version {
		t.Errorf("site after refused updates = {BranchID %d, ProjectID %d, Version %d}; want {%d, 0, %d}",
			st.BranchID, st.ProjectID, st.Version, w.branchID, saved.Version)
	}
}
