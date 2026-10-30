package site_test

import (
	"context"
	"errors"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// readBranch reads the branch with id.
func readBranch(t *testing.T, env *msttest.Env, id int64) company.Branch {
	t.Helper()
	var b company.Branch
	if err := env.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		b, err = company.New(env.Kit).Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("read branch %d: %v", id, err)
	}
	return b
}

// wantStale checks err is DOC's stale error naming title.
func wantStale(t *testing.T, err error, title string) {
	t.Helper()
	want := "Someone else changed " + title + " after you opened it. Reload it and make your changes again."
	var stale *doc.Error
	if !errors.As(err, &stale) || stale.Kind != doc.KindStale || stale.Message != want {
		t.Fatalf("err = %v (%T); want DOC's stale error %q", err, err, want)
	}
}

// TestSite_ProjectBranchMatch: on Create and on Update, whichever of the
// branch and the project changed.
func TestSite_ProjectBranchMatch(t *testing.T) { // MST-106
	const pune = "The site's branch must match the project's branch (PUNE)."
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc := site.New(env.Kit)
	w := &world{env: env, svc: svc}
	input := func(code, branch, project string) site.Input {
		return site.Input{Code: code, Name: code, BranchID: m.Branches[branch], ProjectID: m.Projects[project], Type: site.SiteKind}
	}
	techID, metroID := m.Sites["TECH-SITE"], m.Sites["METRO-SITE"]

	_, err := svc.Create(ctx, env.Kavya, input("BLR-STORE", "BLR", "PRJ-METRO"))
	wantOneFieldError(t, err, "project_id", pune)

	_, err = svc.Update(ctx, env.Kavya, techID, 1, input("TECH-SITE", "BLR", "PRJ-METRO"))
	wantOneFieldError(t, err, "project_id", pune)
	if st := w.get(t, techID); st.ProjectID != m.Projects["PRJ-TECHPARK"] || st.Version != 1 {
		t.Errorf("TECH-SITE after refusal = {ProjectID %d, Version %d}; want {%d, 1}", st.ProjectID, st.Version, m.Projects["PRJ-TECHPARK"])
	}

	_, err = svc.Update(ctx, env.Kavya, metroID, 1, input("METRO-SITE", "BLR", "PRJ-METRO"))
	wantOneFieldError(t, err, "project_id", pune)
	if st := w.get(t, metroID); st.BranchID != m.Branches["PUNE"] || st.Version != 1 {
		t.Errorf("METRO-SITE after refusal = {BranchID %d, Version %d}; want {%d, 1}", st.BranchID, st.Version, m.Branches["PUNE"])
	}

	// The same branch, or no project, saves.
	if _, err := svc.Create(ctx, env.Kavya, input("TECH-STORE", "BLR", "PRJ-TECHPARK")); err != nil {
		t.Errorf("site on its branch's project: %v", err)
	}
	if _, err := svc.Create(ctx, env.Kavya, input("BLR-STORE", "BLR", "")); err != nil {
		t.Errorf("site with no project: %v", err)
	}
}

// TestSite_BranchLocked: a draft doesn't lock the branch, an approved document
// does, and other fields still save. MST-107's GRN is not built yet, so
// mst.Order stands in for it.
func TestSite_BranchLocked(t *testing.T) { // MST-107
	const msg = "Branch can't change after the site is used."
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc := site.New(env.Kit)
	w := &world{env: env, svc: svc}
	id, pune, blr := m.Sites["PUNE-WH"], m.Branches["PUNE"], m.Branches["BLR"]
	input := func(name string, branchID int64) site.Input {
		return site.Input{Code: "PUNE-WH", Name: name, BranchID: branchID, Type: site.Warehouse}
	}
	save := func(in site.Input) {
		t.Helper()
		if _, err := svc.Update(ctx, env.Kavya, id, w.get(t, id).Version, in); err != nil {
			t.Fatalf("Update(%+v): %v", in, err)
		}
	}

	res, err := mstfixture.Save(ctx, env, env.Kavya, 0, mstfixture.Header{
		VendorID: m.Parties["SHREE"], AddressID: m.Addresses["Mumbai"], BranchID: pune, SiteID: id,
		Date: types.Date{Year: 2026, Month: time.October, Day: 24},
	}, []mstfixture.Line{{ItemID: m.Items["CEMENT-OPC"], UoMID: m.UoMs["BAG"], TaxCodeID: m.TaxCodes["GST18"], Qty: types.Qty(10_000), Rate: types.Rate(62_5000)}})
	if err != nil {
		t.Fatalf("save draft order at PUNE-WH: %v", err)
	}

	save(input("PUNE-WH", blr)) // a draft isn't use
	save(input("PUNE-WH", pune))

	mstfixture.Approve(t, env, res.ID)
	before := w.get(t, id)

	// A stale page gets the stale error, not the lock (CD-94).
	_, err = svc.Update(ctx, env.Kavya, id, before.Version-1, input("PUNE-WH", blr))
	wantStale(t, err, "Site PUNE-WH")

	_, err = svc.Update(ctx, env.Kavya, id, before.Version, input("PUNE-WH", blr))
	wantOneFieldError(t, err, "branch_id", msg)
	if st := w.get(t, id); st != before {
		t.Errorf("PUNE-WH after refusal = %+v; want %+v", st, before)
	}

	save(input("Pune Central Warehouse", pune)) // the same branch still saves
}

// TestSite_EnableUnderDisabledBranch: enabling a site would make the state
// MST-108 refuses from the branch's side.
func TestSite_EnableUnderDisabledBranch(t *testing.T) { // MST-108, CD-170
	const msg = "Branch PUNE is disabled. Pick another branch."
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc, co := site.New(env.Kit), company.New(env.Kit)
	w := &world{env: env, svc: svc}
	setSite := func(code string, disabled bool) error {
		t.Helper()
		return svc.SetDisabled(ctx, env.Kavya, m.Sites[code], w.get(t, m.Sites[code]).Version, disabled)
	}
	setPune := func(disabled bool) {
		t.Helper()
		if err := co.SetDisabled(ctx, env.Kavya, m.Branches["PUNE"], readBranch(t, env, m.Branches["PUNE"]).Version, disabled); err != nil {
			t.Fatalf("set PUNE disabled %v: %v", disabled, err)
		}
	}
	for _, code := range []string{"PUNE-WH", "METRO-SITE"} {
		if err := setSite(code, true); err != nil {
			t.Fatalf("disable %s: %v", code, err)
		}
	}
	setPune(true)

	before := w.get(t, m.Sites["PUNE-WH"])
	wantOneFieldError(t, setSite("PUNE-WH", false), "branch_id", msg)
	if st := w.get(t, before.ID); st != before {
		t.Errorf("PUNE-WH after refusal = %+v; want %+v", st, before)
	}

	// A stale page gets the stale error, not the refusal (CD-94).
	wantStale(t, svc.SetDisabled(ctx, env.Kavya, before.ID, before.Version-1, false), "Site PUNE-WH")

	setPune(false)
	if err := setSite("PUNE-WH", false); err != nil {
		t.Fatalf("enable PUNE-WH with PUNE enabled: %v", err)
	}
	if st := w.get(t, before.ID); st.Disabled || st.Version != before.Version+1 {
		t.Errorf("PUNE-WH = {Disabled %v, Version %d}; want {false, %d}", st.Disabled, st.Version, before.Version+1)
	}
}

// TestSite_BranchMatchesProjectRandomWrites: random site, project and branch
// writes, from both sides of MST-106 and MST-108; after each, every site with a
// project has the project's branch and no enabled site is on a disabled branch
// (CD-180).
func TestSite_BranchMatchesProjectRandomWrites(t *testing.T) { // MST-106, MST-108
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc, projects, co := site.New(env.Kit), project.New(env.Kit), company.New(env.Kit)
	w := &world{env: env, svc: svc}
	readProject := func(id int64) project.Project {
		t.Helper()
		var pr project.Project
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			pr, err = projects.Get(ctx, tx, id)
			return err
		}); err != nil {
			t.Fatalf("read project %d: %v", id, err)
		}
		return pr
	}
	branches := []int64{m.Branches["PUNE"], m.Branches["BLR"]}
	projectIDs := []int64{0, m.Projects["PRJ-METRO"], m.Projects["PRJ-TECHPARK"]}
	siteIDs := []int64{m.Sites["PUNE-WH"], m.Sites["METRO-SITE"], m.Sites["TECH-SITE"]}
	r := rand.New(rand.NewPCG(27, 106))
	var saves, branchOffs, enableRefused int
	for step := range 600 {
		var err error
		branch := branches[r.IntN(len(branches))]
		st := w.get(t, siteIDs[r.IntN(len(siteIDs))])
		switch r.IntN(6) {
		case 0:
			pr := readProject(projectIDs[1+r.IntN(2)])
			_, err = projects.Update(ctx, env.Kavya, pr.ID, pr.Version, project.Input{Code: pr.Code, Name: pr.Name, BranchID: branch})
		case 1, 2:
			_, err = svc.Update(ctx, env.Kavya, st.ID, st.Version, site.Input{
				Code: st.Code, Name: st.Name, BranchID: branch, ProjectID: projectIDs[r.IntN(len(projectIDs))], Type: st.Type,
			})
		case 3, 4:
			err = svc.SetDisabled(ctx, env.Kavya, st.ID, st.Version, !st.Disabled)
			if err != nil && st.Disabled {
				enableRefused++
			}
		default:
			b := readBranch(t, env, branch)
			err = co.SetDisabled(ctx, env.Kavya, b.ID, b.Version, !b.Disabled)
			if err == nil && !b.Disabled {
				branchOffs++
			}
		}
		var ve *mstkit.ValidationError
		var refused *mstkit.DeleteRefusedError
		if err != nil && !errors.As(err, &ve) && !errors.As(err, &refused) {
			t.Fatalf("step %d: %v; want saved or refused", step, err)
		}
		if err == nil {
			saves++
		}
		for _, id := range siteIDs {
			st := w.get(t, id)
			if st.ProjectID != 0 && readProject(st.ProjectID).BranchID != st.BranchID {
				t.Fatalf("step %d: site %s is on branch %d, its project on another", step, st.Code, st.BranchID)
			}
			if !st.Disabled && readBranch(t, env, st.BranchID).Disabled {
				t.Fatalf("step %d: enabled site %s is on disabled branch %d", step, st.Code, st.BranchID)
			}
		}
	}
	if saves == 0 || branchOffs == 0 || enableRefused == 0 {
		t.Errorf("saves %d, branches disabled %d, enables refused %d; want each above 0, or the sequence tests nothing", saves, branchOffs, enableRefused)
	}
}
