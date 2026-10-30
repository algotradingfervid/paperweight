package site_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const projectRefused = "You can only link sites to your assigned projects. Ask your administrator to add this project." // CD-214

// projectWorld is the canonical masters, with Priya (restricted to PRJ-METRO)
// holding read, create and write on Site.
type projectWorld struct {
	*world
	m msttest.Masters
}

func newProjectWorld(t *testing.T) projectWorld {
	t.Helper()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	a := authService(t, env)
	rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}
	if err := a.SetRights(context.Background(), env.Admin, "Purchase User", site.DocType, rights); err != nil {
		t.Fatalf("SetRights(Purchase User, Site): %v", err)
	}
	return projectWorld{world: &world{env: env, svc: site.New(env.Kit)}, m: m}
}

func authService(t *testing.T, env *msttest.Env) *auth.Auth {
	t.Helper()
	a, ok := env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("msttest Kit.Auth is %T; want *auth.Auth", env.Kit.Auth)
	}
	return a
}

// input is a site of branch and project ("" = none).
func (w projectWorld) input(code, branch, project string) site.Input {
	return site.Input{Code: code, Name: code, BranchID: w.m.Branches[branch], ProjectID: w.m.Projects[project], Type: site.SiteKind}
}

// wantProjectRefused checks err is CD-214's refusal: a 403 with the exact text.
func wantProjectRefused(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err = %v (%T); want auth.ErrForbidden", err, err)
	}
	if err.Error() != projectRefused {
		t.Errorf("err = %q; want %q", err, projectRefused)
	}
}

func (w projectWorld) wantNoSite(t *testing.T, code string) {
	t.Helper()
	var found bool
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		_, found, err = w.svc.ByCode(context.Background(), tx, code)
		return err
	}); err != nil {
		t.Fatalf("ByCode(%s): %v", code, err)
	}
	if found {
		t.Errorf("site %s exists after the refusal", code)
	}
}

func TestSite_CreateOnOwnProject(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	saved, err := w.svc.Create(context.Background(), w.env.Priya, w.input("METRO-2", "PUNE", "PRJ-METRO"))
	if err != nil {
		t.Fatalf("Create on Priya's project: %v", err)
	}
	if got := w.get(t, saved.ID).ProjectID; got != w.m.Projects["PRJ-METRO"] {
		t.Errorf("ProjectID = %d; want %d", got, w.m.Projects["PRJ-METRO"])
	}
}

func TestSite_CreateOnOtherProjectRefused(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	_, err := w.svc.Create(context.Background(), w.env.Priya, w.input("TECH-2", "BLR", "PRJ-TECHPARK"))
	wantProjectRefused(t, err)
	w.wantNoSite(t, "TECH-2")
}

// TestSite_RefusalNamesNothingOfTheProject: the refusal comes before the link
// checks, whose texts carry a project's or branch's code the user can't see.
func TestSite_RefusalNamesNothingOfTheProject(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	ctx := context.Background()
	wrongBranch := w.input("TECH-2", "PUNE", "PRJ-TECHPARK")
	_, err := w.svc.Create(ctx, w.env.Priya, wrongBranch)
	wantProjectRefused(t, err)
	_, err = w.svc.Update(ctx, w.env.Priya, w.m.Sites["PUNE-WH"], 1, wrongBranch)
	wantProjectRefused(t, err)
}

// TestSite_UnknownProjectRefusedLikeHidden: a restricted user gets the same
// refusal for a project id that doesn't exist as for one they can't see, so the
// answer doesn't tell them which ids exist.
func TestSite_UnknownProjectRefusedLikeHidden(t *testing.T) { // CD-214
	const unknownID = 999999
	w := newProjectWorld(t)
	ctx := context.Background()
	in := w.input("NOWHERE", "PUNE", "")
	in.ProjectID = unknownID
	_, unknown := w.svc.Create(ctx, w.env.Priya, in)
	wantProjectRefused(t, unknown)
	w.wantNoSite(t, "NOWHERE")
	_, hidden := w.svc.Create(ctx, w.env.Priya, w.input("NOWHERE", "BLR", "PRJ-TECHPARK"))
	wantProjectRefused(t, hidden)
	if unknown.Error() != hidden.Error() {
		t.Errorf("unknown project = %q; hidden project = %q; want the same text", unknown, hidden)
	}

	id := w.m.Sites["METRO-SITE"]
	in.Code = "METRO-SITE"
	_, err := w.svc.Update(ctx, w.env.Priya, id, 1, in)
	wantProjectRefused(t, err)
	if st := w.get(t, id); st.ProjectID != w.m.Projects["PRJ-METRO"] || st.Version != 1 {
		t.Errorf("site after refusal = %+v; want it unchanged at version 1", st)
	}
}

// TestSite_StalePageOnOtherProjectRefused: a stale page posting a foreign
// project gets the 403, not the 409: the refusal comes before CheckVersion.
func TestSite_StalePageOnOtherProjectRefused(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	ctx := context.Background()
	id := w.m.Sites["METRO-SITE"]
	bump := w.input("METRO-SITE", "PUNE", "PRJ-METRO")
	bump.Name = "Moved on"
	if _, err := w.svc.Update(ctx, w.env.Priya, id, 1, bump); err != nil {
		t.Fatalf("Update to move the version on: %v", err)
	}
	_, err := w.svc.Update(ctx, w.env.Priya, id, 1, w.input("METRO-SITE", "BLR", "PRJ-TECHPARK"))
	wantProjectRefused(t, err)
	st := w.get(t, id)
	if st.ProjectID != w.m.Projects["PRJ-METRO"] || st.Version != 2 {
		t.Errorf("site after refusal = %+v; want PRJ-METRO at version 2", st)
	}
}

func TestSite_UpdateOntoOtherProjectRefused(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	id := w.m.Sites["METRO-SITE"]
	in := w.input("METRO-SITE", "BLR", "PRJ-TECHPARK")
	_, err := w.svc.Update(context.Background(), w.env.Priya, id, 1, in)
	wantProjectRefused(t, err)
	st := w.get(t, id)
	if st.ProjectID != w.m.Projects["PRJ-METRO"] || st.BranchID != w.m.Branches["PUNE"] || st.Version != 1 {
		t.Errorf("site after refusal = %+v; want it unchanged at version 1", st)
	}
}

// TestSite_UpdateKeepingOtherProjectRefused: every save that posts the project
// is checked, so a real edit of a site on another project is refused too.
func TestSite_UpdateKeepingOtherProjectRefused(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	id := w.m.Sites["TECH-SITE"]
	in := w.input("TECH-SITE", "BLR", "PRJ-TECHPARK")
	in.Name = "Renamed"
	_, err := w.svc.Update(context.Background(), w.env.Priya, id, 1, in)
	wantProjectRefused(t, err)
	if st := w.get(t, id); st.Name == "Renamed" || st.Version != 1 {
		t.Errorf("site after refusal = %+v; want it unchanged at version 1", st)
	}
}

// TestSite_NoOpSaveStillChecksProject: the check runs before mstkit.Unchanged
// skips the write, so a save that changes nothing is refused as well.
func TestSite_NoOpSaveStillChecksProject(t *testing.T) { // CD-214, CD-208
	w := newProjectWorld(t)
	ctx := context.Background()
	st := w.get(t, w.m.Sites["TECH-SITE"])
	in := site.Input{Code: st.Code, Name: st.Name, BranchID: st.BranchID, ProjectID: st.ProjectID, Type: st.Type}
	_, err := w.svc.Update(ctx, w.env.Priya, st.ID, st.Version, in)
	wantProjectRefused(t, err)

	own := w.get(t, w.m.Sites["METRO-SITE"])
	in = site.Input{Code: own.Code, Name: own.Name, BranchID: own.BranchID, ProjectID: own.ProjectID, Type: own.Type}
	saved, err := w.svc.Update(ctx, w.env.Priya, own.ID, own.Version, in)
	if err != nil {
		t.Fatalf("no-op save on Priya's project: %v", err)
	}
	if saved.Version != own.Version {
		t.Errorf("no-op save Version = %d; want %d", saved.Version, own.Version)
	}
}

func TestSite_NoProjectSavesForRestrictedUser(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	ctx := context.Background()
	saved, err := w.svc.Create(ctx, w.env.Priya, w.input("PLAIN", "PUNE", ""))
	if err != nil {
		t.Fatalf("Create with no project: %v", err)
	}
	in := w.input("PLAIN", "PUNE", "")
	in.Name = "Renamed"
	if _, err := w.svc.Update(ctx, w.env.Priya, saved.ID, saved.Version, in); err != nil {
		t.Fatalf("Update with no project: %v", err)
	}
	if got := w.get(t, saved.ID).Name; got != "Renamed" {
		t.Errorf("Name = %q; want Renamed", got)
	}
}

func TestSite_UnrestrictedUserPasses(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	ctx := context.Background()
	saved, err := w.svc.Create(ctx, w.env.Kavya, w.input("TECH-2", "BLR", "PRJ-TECHPARK"))
	if err != nil {
		t.Fatalf("Create as Kavya: %v", err)
	}
	in := w.input("TECH-2", "BLR", "PRJ-TECHPARK")
	in.Name = "Renamed"
	if _, err := w.svc.Update(ctx, w.env.Kavya, saved.ID, saved.Version, in); err != nil {
		t.Fatalf("Update as Kavya: %v", err)
	}
}

// TestSite_AdministratorPasses: Administrator bypasses a restriction row, as
// in Can.
func TestSite_AdministratorPasses(t *testing.T) { // CD-214, CD-174
	w := newProjectWorld(t)
	ctx := context.Background()
	a := authService(t, w.env)
	metro := []int64{w.m.Projects["PRJ-METRO"]}
	if err := a.ChangeRestrictions(ctx, w.env.Admin, w.env.Admin.ID, auth.DimProject, metro, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions(Admin): %v", err)
	}
	if _, err := w.svc.Create(ctx, w.env.Admin, w.input("TECH-2", "BLR", "PRJ-TECHPARK")); err != nil {
		t.Fatalf("Create as Administrator: %v", err)
	}
}

// TestSite_ListShowsEverySiteToProjectRestrictedUser: Site declares no
// ProjectFields, so the project restriction doesn't hide sites.
func TestSite_ListShowsEverySiteToProjectRestrictedUser(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	dt, ok := doctype.Lookup(site.DocType)
	if !ok {
		t.Fatalf("DocType %s is not registered", site.DocType)
	}
	ls := list.Service{DB: w.env.DB, Auth: w.env.Kit.Auth}
	res, err := ls.List(context.Background(), w.env.Priya, dt, list.Query{Page: 1})
	if err != nil {
		t.Fatalf("list sites: %v", err)
	}
	var got []string
	for _, row := range res.Rows {
		code, _ := row.Values["code"].(string)
		got = append(got, code)
	}
	slices.Sort(got)
	if want := []string{"METRO-SITE", "PUNE-WH", "TECH-SITE"}; !slices.Equal(got, want) {
		t.Errorf("sites Priya lists = %v; want %v", got, want)
	}
}

// TestSite_UpdateRestrictedError: an error from auth.Restricted refuses the
// update and is returned as is.
func TestSite_UpdateRestrictedError(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	kit := w.env.Kit
	kit.Auth = failingRestricted{kit.Auth}
	_, err := site.New(kit).Update(context.Background(), w.env.Priya, w.m.Sites["METRO-SITE"], 1, w.input("METRO-SITE", "PUNE", "PRJ-METRO"))
	if !errors.Is(err, errRestrictedBoom) {
		t.Errorf("Update with failing Restricted = %v; want %v", err, errRestrictedBoom)
	}
}
