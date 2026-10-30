package company_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	otherPAN = "AABCS1429B"
	// uinGSTIN passes the check digit and the state but holds no PAN (MST-031).
	uinGSTIN = "0717UNO00157UNO"

	branchStateMsg = "Branch address state must match the branch state." // MST-029
	noPANMsg       = "GSTIN 0717UNO00157UNO has no PAN in it. Check for typing mistakes."
	panPuneMsg     = "GSTIN 27AAFCV2449G1Z7 belongs to PAN AAFCV2449G, not the company PAN AABCS1429B." // MST-023
	panBLRMsg      = "GSTIN 29AAFCV2449G1Z3 belongs to PAN AAFCV2449G, not the company PAN AABCS1429B." // MST-023
)

func getBranch(t *testing.T, d *db.DB, s *company.Service, id int64) company.Branch {
	t.Helper()
	var b company.Branch
	if err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		b, err = s.Get(context.Background(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Get(branch %d): %v", id, err)
	}
	return b
}

func branchInput(b company.Branch) company.BranchInput {
	return company.BranchInput{Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: b.GSTIN, IsHeadOffice: b.IsHeadOffice}
}

func TestBranch_SharedGSTINSameState(t *testing.T) { // MST-025
	const msg = "GSTIN 27AAFCV2449G1Z7 is for Maharashtra (27) but the branch state is Karnataka (29). Fix the GSTIN or the state."
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	s := company.New(env.Kit)

	mum := createBranch(t, env.DB, s, env.Kavya, company.BranchInput{Code: "MUM", Name: "Mumbai Office", StateCode: "27", GSTIN: puneGSTIN})
	if got := getBranch(t, env.DB, s, mum.ID).GSTIN; got != puneGSTIN {
		t.Errorf("MUM GSTIN = %q; want %q (shared with PUNE)", got, puneGSTIN)
	}

	blr := getBranch(t, env.DB, s, m.Branches["BLR"])
	in := branchInput(blr)
	in.GSTIN = puneGSTIN
	_, err := s.Update(context.Background(), env.Kavya, blr.ID, blr.Version, in)
	wantFieldError(t, err, "gstin", msg)
	if got := getBranch(t, env.DB, s, blr.ID); got.GSTIN != blrGSTIN || got.Version != blr.Version {
		t.Errorf("BLR after refusal: GSTIN %q, Version %d; want %q, %d", got.GSTIN, got.Version, blrGSTIN, blr.Version)
	}
}

func TestBranch_Address(t *testing.T) { // MST-029
	ctx := context.Background()
	input := func(branch int64) address.Input {
		return address.Input{BranchID: branch, Title: "Bhosari", Line1: "Plot 12, MIDC Bhosari", City: "Pune", StateCode: "27", Pincode: "411026", IsBilling: true}
	}

	t.Run("owned by the branch, in its state", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		as := address.New(env.Kit)
		saved, err := as.Create(ctx, env.Kavya, input(m.Branches["PUNE"]))
		if err != nil {
			t.Fatalf("Create = %v; want nil", err)
		}
		var a address.Address
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			a, err = as.Get(ctx, tx, saved.ID)
			return err
		}); err != nil {
			t.Fatalf("Get: %v", err)
		}
		if a.BranchID != m.Branches["PUNE"] || a.PartyID != 0 || a.StateCode != "27" {
			t.Errorf("address = {BranchID %d, PartyID %d, StateCode %q}; want {%d, 0, 27}", a.BranchID, a.PartyID, a.StateCode, m.Branches["PUNE"])
		}
	})

	t.Run("create in another state", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		in := input(m.Branches["PUNE"])
		in.StateCode, in.City, in.Pincode = "29", "Bengaluru", "560001"
		_, err := address.New(env.Kit).Create(ctx, env.Kavya, in)
		wantFieldError(t, err, "state_code", branchStateMsg)
	})

	t.Run("create abroad", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		in := input(m.Branches["PUNE"])
		in.Country, in.StateCode, in.Pincode = "Singapore", "", "118524"
		_, err := address.New(env.Kit).Create(ctx, env.Kavya, in)
		wantFieldError(t, err, "state_code", branchStateMsg)
	})

	t.Run("update to another state", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		as := address.New(env.Kit)
		in := input(m.Branches["PUNE"])
		saved, err := as.Create(ctx, env.Kavya, in)
		if err != nil {
			t.Fatalf("Create = %v; want nil", err)
		}
		in.StateCode, in.City, in.Pincode = "29", "Bengaluru", "560001"
		_, err = as.Update(ctx, env.Kavya, saved.ID, saved.Version, in)
		wantFieldError(t, err, "state_code", branchStateMsg)
	})

	// The other side of the invariant: the branch can't move away from its
	// addresses.
	t.Run("branch moves to another state", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		s := company.New(env.Kit)
		pune := getBranch(t, env.DB, s, m.Branches["PUNE"])
		in := branchInput(pune)
		in.GSTIN, in.StateCode = "", "29"
		_, err := s.Update(ctx, env.Kavya, pune.ID, pune.Version, in)
		wantFieldError(t, err, "state_code", branchStateMsg)
		if got := getBranch(t, env.DB, s, pune.ID); got.StateCode != "27" || got.Version != pune.Version {
			t.Errorf("PUNE after refusal: StateCode %q, Version %d; want 27, %d", got.StateCode, got.Version, pune.Version)
		}
	})

	// Re-enabling needs no state check because a disabled address still holds
	// the branch to its state.
	t.Run("branch moves away from a disabled address", func(t *testing.T) {
		env := msttest.New(t)
		m := msttest.SeedMasters(t, env)
		s, as := company.New(env.Kit), address.New(env.Kit)
		id := m.BranchAddresses["PUNE"]
		var version int
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			a, err := as.Get(ctx, tx, id)
			version = a.Version
			return err
		}); err != nil {
			t.Fatalf("Get: %v", err)
		}
		if err := as.SetDisabled(ctx, env.Kavya, id, version, true); err != nil {
			t.Fatalf("disable PUNE's address: %v", err)
		}
		pune := getBranch(t, env.DB, s, m.Branches["PUNE"])
		in := branchInput(pune)
		in.GSTIN, in.StateCode = "", "29"
		_, err := s.Update(ctx, env.Kavya, pune.ID, pune.Version, in)
		wantFieldError(t, err, "state_code", branchStateMsg)
	})

	t.Run("branch with no address may move", func(t *testing.T) {
		env := msttest.New(t)
		msttest.SeedMasters(t, env)
		s := company.New(env.Kit)
		saved := createBranch(t, env.DB, s, env.Kavya, company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "27"})
		if _, err := s.Update(ctx, env.Kavya, saved.ID, saved.Version, company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "29"}); err != nil {
			t.Fatalf("Update = %v; want nil (another branch's addresses don't count)", err)
		}
	})
}

// TestBranch_GSTINWithoutPAN pins CD-204 (3): a GSTIN with no PAN inside has
// its own message, not MST-023's with a blank PAN.
func TestBranch_GSTINWithoutPAN(t *testing.T) {
	in := company.BranchInput{Code: "DEL", Name: "Delhi Office", StateCode: "07", GSTIN: uinGSTIN}
	for _, pan := range []string{companyPAN, ""} {
		_, err := company.ValidateBranch(in, pan)
		wantFieldError(t, err, "gstin", noPANMsg)
	}

	u := newUnit(t)
	saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
	err := write(u.db, func(tx *db.Tx) error {
		_, err := u.svc.CreateTx(context.Background(), tx, u.kavya, in)
		return err
	})
	wantFieldError(t, err, "gstin", noPANMsg)

	// Setup would otherwise report a missing PAN.
	u = newUnit(t)
	err = write(u.db, func(tx *db.Tx) error {
		return u.svc.SetupTx(context.Background(), tx, u.kavya, company.SetupInput{
			LegalName: legalName, StateCode: "07", GSTIN: uinGSTIN, BranchCode: "DEL",
		})
	})
	wantFieldError(t, err, "gstin", noPANMsg)
}

// TestCompany_PANChangeChecksBranches is MST-023 from the company's side:
// every branch GSTIN must carry the PAN the company ends up with, a disabled
// branch's too.
func TestCompany_PANChangeChecksBranches(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) (*unit, mstkit.Saved) {
		t.Helper()
		u := newUnit(t)
		saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: puneGSTIN, IsHeadOffice: true})
		blr := createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: blrGSTIN})
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "27"})
		return u, blr
	}

	t.Run("every branch named", func(t *testing.T) {
		u, blr := seed(t)
		mustWrite(t, u.db, func(tx *db.Tx) error {
			return u.svc.SetDisabledTx(ctx, tx, u.kavya, blr.ID, blr.Version, true)
		})
		err := write(u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: otherPAN}, 1)
		})
		var ve *mstkit.ValidationError
		if !errors.As(err, &ve) {
			t.Fatalf("err = %v (%T); want *mstkit.ValidationError", err, err)
		}
		want := []doctype.FieldError{{Field: "pan", Message: panBLRMsg}, {Field: "pan", Message: panPuneMsg}}
		got := ve.FieldErrors()
		if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
			t.Errorf("FieldErrors() = %+v; want %+v (by branch code)", got, want)
		}
		if c, _ := readCompany(t, u.db, u.svc); c.PAN != companyPAN || c.Version != 1 {
			t.Errorf("company after refusal: PAN %q, Version %d; want %q, 1", c.PAN, c.Version, companyPAN)
		}
	})

	t.Run("a shared GSTIN is named once", func(t *testing.T) {
		u := newUnit(t)
		saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: puneGSTIN})
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "MUM", Name: "Mumbai Office", StateCode: "27", GSTIN: puneGSTIN})
		err := write(u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: otherPAN}, 1)
		})
		wantFieldError(t, err, "pan", panPuneMsg)
	})

	t.Run("a stale page gets the conflict", func(t *testing.T) {
		u, _ := seed(t)
		err := write(u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: otherPAN}, 9)
		})
		var he doctype.HTTPError
		if !errors.As(err, &he) || he.Status() != 409 {
			t.Errorf("err = %v; want a 409", err)
		}
	})

	t.Run("same PAN saves", func(t *testing.T) {
		u, _ := seed(t)
		mustWrite(t, u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: "Vardhan Infra Ltd", PAN: companyPAN}, 1)
		})
		if c, _ := readCompany(t, u.db, u.svc); c.LegalName != "Vardhan Infra Ltd" {
			t.Errorf("LegalName = %q; want the new name", c.LegalName)
		}
	})

	t.Run("no branch GSTIN, PAN may change", func(t *testing.T) {
		u := newUnit(t)
		saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "27"})
		mustWrite(t, u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: otherPAN}, 1)
		})
	})

	// A branch saved before the company exists is checked against no PAN.
	t.Run("first save checks existing branches", func(t *testing.T) {
		u := newUnit(t)
		createBranch(t, u.db, u.svc, u.kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: puneGSTIN})
		err := write(u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(ctx, tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: otherPAN}, 0)
		})
		wantFieldError(t, err, "pan", panPuneMsg)
		saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
	})
}

// TestBranch_DisableWithSites: only enabled sites hold a branch, named by code
// in sorted order, and the refused branch doesn't move. The other branch's
// sites don't count.
func TestBranch_DisableWithSites(t *testing.T) { // MST-108
	ctx := context.Background()
	env := msttest.New(t)
	m := msttest.SeedMasters(t, env)
	co, sites := company.New(env.Kit), site.New(env.Kit)
	pune := func() company.Branch {
		t.Helper()
		var b company.Branch
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			b, err = co.Get(ctx, tx, m.Branches["PUNE"])
			return err
		}); err != nil {
			t.Fatalf("read PUNE: %v", err)
		}
		return b
	}
	disableSite := func(code string) {
		t.Helper()
		var st site.Site
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			st, err = sites.Get(ctx, tx, m.Sites[code])
			return err
		}); err != nil {
			t.Fatalf("read %s: %v", code, err)
		}
		if err := sites.SetDisabled(ctx, env.Kavya, st.ID, st.Version, true); err != nil {
			t.Fatalf("disable %s: %v", code, err)
		}
	}
	wantRefused := func(msg string) {
		t.Helper()
		before := pune()
		err := co.SetDisabled(ctx, env.Kavya, before.ID, before.Version, true)
		var refused *mstkit.DeleteRefusedError
		if !errors.As(err, &refused) || refused.Message != msg {
			t.Fatalf("disable PUNE = %v (%T); want refusal %q", err, err, msg)
		}
		if refused.Status() != 422 || refused.FieldErrors() != nil {
			t.Errorf("refusal = status %d, fields %+v; want 422 with no field", refused.Status(), refused.FieldErrors())
		}
		if after := pune(); after != before {
			t.Errorf("PUNE after refusal = %+v; want %+v", after, before)
		}
	}

	wantRefused("Disable the branch's sites first: METRO-SITE, PUNE-WH.")
	disableSite("PUNE-WH")
	wantRefused("Disable the branch's sites first: METRO-SITE.")
	disableSite("METRO-SITE")

	b := pune()
	if err := co.SetDisabled(ctx, env.Kavya, b.ID, b.Version, true); err != nil {
		t.Fatalf("disable PUNE with its sites disabled: %v", err)
	}
	if !pune().Disabled {
		t.Error("PUNE is still enabled")
	}
}
