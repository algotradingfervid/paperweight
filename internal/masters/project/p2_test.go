package project_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
)

// TestProject_CustomerRole: the customer link is checked on Create and on
// Update, also when it is unchanged: a vendor-only party has MST-103's text, a
// disabled or unknown one CD-170's.
func TestProject_CustomerRole(t *testing.T) { // MST-103, CD-170
	const (
		roleMsg     = "Shree Steel Traders is not marked as a customer."
		disabledMsg = "Customer METRO-RAIL is disabled. Pick another customer."
		unknownMsg  = "Customer not found. Pick one from the list."
	)
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc, parties := project.New(env.Kit), party.New(env.Kit)
	rail, err := parties.Create(ctx, env.Kavya, party.Input{Code: "METRO-RAIL", Name: "Metro Rail Corporation", IsCustomer: true})
	if err != nil {
		t.Fatalf("create customer METRO-RAIL: %v", err)
	}
	both, err := parties.Create(ctx, env.Kavya, party.Input{Code: "BOTH", Name: "Vendor And Customer", IsVendor: true, IsCustomer: true})
	if err != nil {
		t.Fatalf("create party BOTH: %v", err)
	}
	input := func(code string, customerID int64) project.Input {
		return project.Input{Code: code, Name: "Pune Metro Depot", BranchID: m.Branches["PUNE"], CustomerID: customerID}
	}
	const metro = "PRJ-METRO"
	version := 1

	// Refused on both write paths, and the row doesn't move.
	t.Run("vendor only", func(t *testing.T) {
		_, err := svc.Create(ctx, env.Kavya, input("PRJ-NEW", m.Parties["SHREE"]))
		wantOneFieldError(t, err, "customer_id", roleMsg)
		_, err = svc.Update(ctx, env.Kavya, m.Projects[metro], version, input(metro, m.Parties["SHREE"]))
		wantOneFieldError(t, err, "customer_id", roleMsg)
	})
	t.Run("unknown", func(t *testing.T) {
		_, err := svc.Create(ctx, env.Kavya, input("PRJ-NEW", rail.ID+1000))
		wantOneFieldError(t, err, "customer_id", unknownMsg)
		_, err = svc.Update(ctx, env.Kavya, m.Projects[metro], version, input(metro, rail.ID+1000))
		wantOneFieldError(t, err, "customer_id", unknownMsg)
	})

	// A customer saves, a party with both roles too.
	saved, err := svc.Create(ctx, env.Kavya, input("PRJ-BOTH", both.ID))
	if err != nil {
		t.Fatalf("project for a vendor and customer: %v", err)
	}
	updated, err := svc.Update(ctx, env.Kavya, m.Projects[metro], version, input(metro, rail.ID))
	if err != nil {
		t.Fatalf("set customer METRO-RAIL: %v", err)
	}
	if err := parties.SetDisabled(ctx, env.Kavya, rail.ID, rail.Version, true); err != nil {
		t.Fatalf("disable METRO-RAIL: %v", err)
	}

	t.Run("disabled", func(t *testing.T) {
		_, err := svc.Create(ctx, env.Kavya, input("PRJ-NEW", rail.ID))
		wantOneFieldError(t, err, "customer_id", disabledMsg)
		_, err = svc.Update(ctx, env.Kavya, saved.ID, saved.Version, input("PRJ-BOTH", rail.ID))
		wantOneFieldError(t, err, "customer_id", disabledMsg)
		// The unchanged link is checked too (CD-170 has no exception).
		_, err = svc.Update(ctx, env.Kavya, m.Projects[metro], updated.Version, input(metro, rail.ID))
		wantOneFieldError(t, err, "customer_id", disabledMsg)
	})

	w := &world{env: env, svc: svc}
	if p := w.get(t, saved.ID); p.CustomerID != both.ID || p.Version != saved.Version {
		t.Errorf("PRJ-BOTH = {CustomerID %d, Version %d}; want {%d, %d}", p.CustomerID, p.Version, both.ID, saved.Version)
	}
	if p := w.get(t, m.Projects[metro]); p.CustomerID != rail.ID || p.Version != updated.Version {
		t.Errorf("%s = {CustomerID %d, Version %d}; want {%d, %d}", metro, p.CustomerID, p.Version, rail.ID, updated.Version)
	}
}

// TestProject_Branch: a project needs a branch on both write paths; the
// disabled half of MST-104 is in TestProject_BranchLinkMessages.
func TestProject_Branch(t *testing.T) { // MST-104
	const msg = "Branch is required."
	in := validInput()
	in.BranchID = 0
	_, err := project.Validate(in)
	wantOneFieldError(t, err, "branch_id", msg)

	w := newWorld(t)
	_, err = w.svc.Create(context.Background(), w.env.Kavya, in)
	wantOneFieldError(t, err, "branch_id", msg)

	saved := w.create(t, validInput())
	_, err = w.svc.Update(context.Background(), w.env.Kavya, saved.ID, saved.Version, in)
	wantOneFieldError(t, err, "branch_id", msg)
	if p := w.get(t, saved.ID); p.BranchID != w.branchID || p.Version != saved.Version {
		t.Errorf("project after refused update = {BranchID %d, Version %d}; want {%d, %d}", p.BranchID, p.Version, w.branchID, saved.Version)
	}
}

// TestProject_BranchChangeWithSites is MST-106 from the project's side: every
// site on the project, disabled ones too, must be unlinked before its branch
// changes; a save that keeps the branch is fine, and so is another project's
// site.
func TestProject_BranchChangeWithSites(t *testing.T) { // MST-106
	const refusal = "Unlink the project's sites first: METRO-A, METRO-SITE."
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	svc, sites := project.New(env.Kit), site.New(env.Kit)
	w := &world{env: env, svc: svc}
	metroID, pune, blr := m.Projects["PRJ-METRO"], m.Branches["PUNE"], m.Branches["BLR"]
	// Created after METRO-SITE, sorted before it: the refusal sorts by code.
	store, err := sites.Create(ctx, env.Kavya, site.Input{Code: "METRO-A", Name: "Metro Store", BranchID: pune, ProjectID: metroID, Type: site.Warehouse})
	if err != nil {
		t.Fatalf("create METRO-A: %v", err)
	}
	if err := sites.SetDisabled(ctx, env.Kavya, store.ID, store.Version, true); err != nil {
		t.Fatalf("disable METRO-A: %v", err)
	}
	in := project.Input{Code: "PRJ-METRO", Name: "Pune Metro Depot", BranchID: blr}

	// A stale page gets the stale error, not the refusal (CD-94).
	_, err = svc.Update(ctx, env.Kavya, metroID, 2, in)
	var stale *doc.Error
	if !errors.As(err, &stale) || stale.Kind != doc.KindStale {
		t.Fatalf("stale Update = %v (%T); want DOC's stale error", err, err)
	}

	_, err = svc.Update(ctx, env.Kavya, metroID, 1, in)
	wantOneFieldError(t, err, "branch_id", refusal)
	if p := w.get(t, metroID); p.BranchID != pune || p.Version != 1 {
		t.Errorf("PRJ-METRO after refusal = {BranchID %d, Version %d}; want {%d, 1}", p.BranchID, p.Version, pune)
	}

	in.BranchID = pune
	savedOnPune, err := svc.Update(ctx, env.Kavya, metroID, 1, in)
	if err != nil {
		t.Fatalf("save keeping the branch: %v", err)
	}

	unlink := func(id int64, code string, branchID int64, version int) {
		t.Helper()
		if _, err := sites.Update(ctx, env.Kavya, id, version, site.Input{Code: code, Name: code, BranchID: branchID, Type: site.Warehouse}); err != nil {
			t.Fatalf("unlink %s: %v", code, err)
		}
	}
	unlink(m.Sites["METRO-SITE"], "METRO-SITE", pune, 1)
	unlink(store.ID, "METRO-A", pune, store.Version+1)

	in.BranchID = blr
	if _, err := svc.Update(ctx, env.Kavya, metroID, savedOnPune.Version, in); err != nil {
		t.Fatalf("change the branch with no sites left: %v", err)
	}
	if p := w.get(t, metroID); p.BranchID != blr {
		t.Errorf("PRJ-METRO BranchID = %d; want %d", p.BranchID, blr)
	}

	// PRJ-TECHPARK's site doesn't hold PRJ-METRO; the new branch is compared.
	_, err = svc.Update(ctx, env.Kavya, m.Projects["PRJ-TECHPARK"], 1, project.Input{Code: "PRJ-TECHPARK", Name: "Whitefield Tech Park, Bengaluru", BranchID: pune})
	wantOneFieldError(t, err, "branch_id", "Unlink the project's sites first: TECH-SITE.")
}
