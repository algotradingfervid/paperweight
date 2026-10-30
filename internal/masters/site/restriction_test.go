package site_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	projectRefused  = "You can only link sites to your assigned projects. Ask your administrator to add this project." // CD-214
	readOnlyRefused = "This site is on a project you aren't assigned to. Ask your administrator to change it."         // CD-216(6)
)

// projectWorld is the canonical masters, with Priya (restricted to PRJ-METRO)
// holding read, create, write and delete on Site.
type projectWorld struct {
	*world
	m msttest.Masters
}

func newProjectWorld(t *testing.T) projectWorld {
	t.Helper()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	a := authService(t, env)
	rights := map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll, auth.RightDelete: auth.ScopeAll,
	}
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

// TestSite_UpdateKeepingOtherProjectRefused: a real edit of a site on another
// project is refused with CD-216(6)'s text, which replaced CD-214's here.
func TestSite_UpdateKeepingOtherProjectRefused(t *testing.T) { // CD-214, CD-216(6)
	w := newProjectWorld(t)
	id := w.m.Sites["TECH-SITE"]
	in := w.input("TECH-SITE", "BLR", "PRJ-TECHPARK")
	in.Name = "Renamed"
	_, err := w.svc.Update(context.Background(), w.env.Priya, id, 1, in)
	wantReadOnly(t, err)
	if st := w.get(t, id); st.Name == "Renamed" || st.Version != 1 {
		t.Errorf("site after refusal = %+v; want it unchanged at version 1", st)
	}
}

// TestSite_NoOpSaveStillChecksProject: the check runs before mstkit.Unchanged
// skips the write, so a save that changes nothing is refused as well.
func TestSite_NoOpSaveStillChecksProject(t *testing.T) { // CD-214, CD-208, CD-216(6)
	w := newProjectWorld(t)
	ctx := context.Background()
	st := w.get(t, w.m.Sites["TECH-SITE"])
	in := site.Input{Code: st.Code, Name: st.Name, BranchID: st.BranchID, ProjectID: st.ProjectID, Type: st.Type}
	_, err := w.svc.Update(ctx, w.env.Priya, st.ID, st.Version, in)
	wantReadOnly(t, err)

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

// TestSite_UpdatePostedProjectError: PUNE-WH has no project, so the read-only
// check reads nothing, and an error from the check on the posted project
// refuses the update and is returned as is.
func TestSite_UpdatePostedProjectError(t *testing.T) { // CD-214
	w := newProjectWorld(t)
	kit := w.env.Kit
	kit.Auth = failingRestricted{kit.Auth}
	_, err := site.New(kit).Update(context.Background(), w.env.Priya, w.m.Sites["PUNE-WH"], 1, w.input("PUNE-WH", "PUNE", "PRJ-METRO"))
	if !errors.Is(err, errRestrictedBoom) {
		t.Errorf("Update posting a project with failing ValueAllowedTx = %v; want %v", err, errRestrictedBoom)
	}
}

// wantReadOnly checks err is CD-216(6)'s refusal: a 403 with the exact text.
func wantReadOnly(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err = %v (%T); want auth.ErrForbidden", err, err)
	}
	if err.Error() != readOnlyRefused {
		t.Errorf("err = %q; want %q", err, readOnlyRefused)
	}
}

// siteAudits counts the audit rows of every site.
func (w projectWorld) siteAudits(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.env.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), "SELECT count(*) FROM audit_log WHERE doc_type = ?", site.DocType).Scan(&n)
	}); err != nil {
		t.Fatalf("count site audit rows: %v", err)
	}
	return n
}

// readOnlyAttempts is every write of the site st that CD-216(6) refuses, each
// at the given version: blanking the project, moving it to Priya's own project
// (and with a branch that fails the link check, whose text names a code), a
// rename, a no-op save, disable, enable and delete.
func (w projectWorld) readOnlyAttempts(st site.Site, version int) map[string]func() error {
	ctx := context.Background()
	update := func(edit func(*site.Input)) func() error {
		return func() error {
			in := site.Input{Code: st.Code, Name: st.Name, BranchID: st.BranchID, ProjectID: st.ProjectID, Type: st.Type}
			edit(&in)
			_, err := w.svc.Update(ctx, w.env.Priya, st.ID, version, in)
			return err
		}
	}
	return map[string]func() error{
		"rename": update(func(in *site.Input) { in.Name = "Renamed" }),
		"no-op":  update(func(*site.Input) {}),
		"blank":  update(func(in *site.Input) { in.ProjectID = 0 }),
		"own project": update(func(in *site.Input) {
			in.BranchID, in.ProjectID = w.m.Branches["PUNE"], w.m.Projects["PRJ-METRO"]
		}),
		"own project, wrong branch": update(func(in *site.Input) {
			in.BranchID, in.ProjectID = w.m.Branches["BLR"], w.m.Projects["PRJ-METRO"]
		}),
		"unknown branch": update(func(in *site.Input) { in.BranchID = 999999 }),
		"disable":        func() error { return w.svc.SetDisabled(ctx, w.env.Priya, st.ID, version, true) },
		"enable":         func() error { return w.svc.SetDisabled(ctx, w.env.Priya, st.ID, version, false) },
		"delete":         func() error { return w.svc.Delete(ctx, w.env.Priya, st.ID, version) },
	}
}

// TestSite_OtherProjectIsReadOnly: every write of a site on a project outside
// Priya's list is refused, enabled or disabled, leaving the row, its version and
// the audit trail as they were.
func TestSite_OtherProjectIsReadOnly(t *testing.T) { // CD-216(6)
	w := newProjectWorld(t)
	id := w.m.Sites["TECH-SITE"]
	check := func() {
		t.Helper()
		before, audits := w.get(t, id), w.siteAudits(t)
		for name, attempt := range w.readOnlyAttempts(before, before.Version) {
			t.Run(name, func(t *testing.T) {
				wantReadOnly(t, attempt())
				if st := w.get(t, id); st != before {
					t.Errorf("site after refusal = %+v; want %+v", st, before)
				}
				if n := w.siteAudits(t); n != audits {
					t.Errorf("site audit rows = %d; want %d", n, audits)
				}
			})
		}
	}
	check()
	if err := w.svc.SetDisabled(context.Background(), w.env.Kavya, id, 1, true); err != nil {
		t.Fatalf("disable TECH-SITE as Kavya: %v", err)
	}
	check()
}

// TestSite_ReadOnlyBeforeBranchCheckOnEnable: the disabled-branch text names a
// code, so the refusal comes first.
func TestSite_ReadOnlyBeforeBranchCheckOnEnable(t *testing.T) { // CD-216(6)
	w := newProjectWorld(t)
	ctx := context.Background()
	id, blr := w.m.Sites["TECH-SITE"], w.m.Branches["BLR"]
	if err := w.svc.SetDisabled(ctx, w.env.Kavya, id, 1, true); err != nil {
		t.Fatalf("disable TECH-SITE as Kavya: %v", err)
	}
	if err := company.New(w.env.Kit).SetDisabled(ctx, w.env.Kavya, blr, readBranch(t, w.env, blr).Version, true); err != nil {
		t.Fatalf("disable BLR: %v", err)
	}
	wantReadOnly(t, w.svc.SetDisabled(ctx, w.env.Priya, id, 2, false))
}

// TestSite_ReadOnlyStalePageGetsStaleError: the refusal reads the stored row, so
// a stale page gets DOC's stale error, on a site that was already foreign and on
// one moved away under the page; a reload then gets the refusal.
func TestSite_ReadOnlyStalePageGetsStaleError(t *testing.T) { // CD-216(6), CD-94
	for _, code := range []string{"TECH-SITE", "METRO-SITE"} {
		t.Run(code, func(t *testing.T) {
			w := newProjectWorld(t)
			id := w.m.Sites[code]
			stalePage := w.get(t, id)
			in := w.input(code, "BLR", "PRJ-TECHPARK")
			in.Name = "Moved on"
			if _, err := w.svc.Update(context.Background(), w.env.Kavya, id, 1, in); err != nil {
				t.Fatalf("Update as Kavya: %v", err)
			}
			current, audits := w.get(t, id), w.siteAudits(t)
			for name, attempt := range w.readOnlyAttempts(stalePage, stalePage.Version) {
				t.Run(name, func(t *testing.T) { wantStale(t, attempt(), "Site "+code) })
			}
			for name, attempt := range w.readOnlyAttempts(stalePage, current.Version) {
				t.Run("reload "+name, func(t *testing.T) { wantReadOnly(t, attempt()) })
			}
			if st := w.get(t, id); st != current {
				t.Errorf("site after refusals = %+v; want %+v", st, current)
			}
			if n := w.siteAudits(t); n != audits {
				t.Errorf("site audit rows = %d; want %d", n, audits)
			}
		})
	}
}

// fullWrites renames, disables, enables and deletes the site as p.
func (w projectWorld) fullWrites(t *testing.T, p auth.Principal, id int64) {
	t.Helper()
	ctx := context.Background()
	st := w.get(t, id)
	in := site.Input{Code: st.Code, Name: "Renamed", BranchID: st.BranchID, ProjectID: st.ProjectID, Type: st.Type}
	saved, err := w.svc.Update(ctx, p, id, st.Version, in)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := w.svc.SetDisabled(ctx, p, id, saved.Version, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := w.svc.SetDisabled(ctx, p, id, saved.Version+1, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if err := w.svc.Delete(ctx, p, id, saved.Version+2); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, found := w.find(t, id); found {
		t.Errorf("site %d exists after Delete", id)
	}
}

// TestSite_OwnAndNoProjectStayEditable: a site on Priya's project and a site
// with no project are hers to change.
func TestSite_OwnAndNoProjectStayEditable(t *testing.T) { // CD-216(6)
	for name, project := range map[string]string{"own project": "PRJ-METRO", "no project": ""} {
		t.Run(name, func(t *testing.T) {
			w := newProjectWorld(t)
			saved, err := w.svc.Create(context.Background(), w.env.Priya, w.input("NEW-SITE", "PUNE", project))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			w.fullWrites(t, w.env.Priya, saved.ID)
		})
	}
}

// TestSite_NoProjectOntoOtherProjectKeepsItsMessage: from a site Priya may
// edit, a move onto a foreign project keeps CD-214's text.
func TestSite_NoProjectOntoOtherProjectKeepsItsMessage(t *testing.T) { // CD-214, CD-216(6)
	w := newProjectWorld(t)
	_, err := w.svc.Update(context.Background(), w.env.Priya, w.m.Sites["PUNE-WH"], 1, w.input("PUNE-WH", "BLR", "PRJ-TECHPARK"))
	wantProjectRefused(t, err)
}

// TestSite_UnrestrictedUserWritesOtherProject: Kavya has no project restriction.
func TestSite_UnrestrictedUserWritesOtherProject(t *testing.T) { // CD-216(6)
	w := newProjectWorld(t)
	w.fullWrites(t, w.env.Kavya, w.m.Sites["TECH-SITE"])
}

// TestSite_AdministratorWritesOtherProject: Administrator bypasses a restriction
// row, as in Can.
func TestSite_AdministratorWritesOtherProject(t *testing.T) { // CD-216(6), CD-174
	w := newProjectWorld(t)
	a := authService(t, w.env)
	metro := []int64{w.m.Projects["PRJ-METRO"]}
	if err := a.ChangeRestrictions(context.Background(), w.env.Admin, w.env.Admin.ID, auth.DimProject, metro, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions(Admin): %v", err)
	}
	w.fullWrites(t, w.env.Admin, w.m.Sites["TECH-SITE"])
}

// TestSite_DeleteWithoutRightGetsRightsRefusal: the Can check comes first, so a
// user with no delete right gets the rights text, not the read-only refusal.
func TestSite_DeleteWithoutRightGetsRightsRefusal(t *testing.T) { // CD-216(6)
	const denied = "You don't have permission to delete Site. Ask your administrator for a role that allows it."
	w := newProjectWorld(t)
	ctx := context.Background()
	rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}
	if err := authService(t, w.env).SetRights(ctx, w.env.Admin, "Purchase User", site.DocType, rights); err != nil {
		t.Fatalf("SetRights(Purchase User, Site): %v", err)
	}
	before := w.get(t, w.m.Sites["TECH-SITE"])
	err := w.svc.Delete(ctx, w.env.Priya, before.ID, before.Version)
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Delete without the right = %v (%T); want auth.ErrForbidden", err, err)
	}
	if err.Error() != denied {
		t.Errorf("Delete without the right = %q; want %q", err, denied)
	}
	if st := w.get(t, before.ID); st != before {
		t.Errorf("site after refusal = %+v; want %+v", st, before)
	}
}

// TestSite_LifecycleRestrictedError: an error from auth.Restricted refuses the
// disable and the delete and is returned as is.
func TestSite_LifecycleRestrictedError(t *testing.T) { // CD-216(6)
	w := newProjectWorld(t)
	ctx := context.Background()
	kit := w.env.Kit
	kit.Auth = failingRestricted{kit.Auth}
	svc, id := site.New(kit), w.m.Sites["METRO-SITE"]
	if err := svc.SetDisabled(ctx, w.env.Priya, id, 1, true); !errors.Is(err, errRestrictedBoom) {
		t.Errorf("SetDisabled with failing Restricted = %v; want %v", err, errRestrictedBoom)
	}
	if err := svc.Delete(ctx, w.env.Priya, id, 1); !errors.Is(err, errRestrictedBoom) {
		t.Errorf("Delete with failing Restricted = %v; want %v", err, errRestrictedBoom)
	}
}

// TestSite_LifecycleMissingSite: a missing site stays doctype.ErrNotFound.
func TestSite_LifecycleMissingSite(t *testing.T) { // CD-216(6)
	const noSite = 999999
	w := newProjectWorld(t)
	ctx := context.Background()
	if err := w.svc.SetDisabled(ctx, w.env.Priya, noSite, 1, true); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("SetDisabled(missing) = %v; want doctype.ErrNotFound", err)
	}
	if err := w.svc.Delete(ctx, w.env.Priya, noSite, 1); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Delete(missing) = %v; want doctype.ErrNotFound", err)
	}
}
