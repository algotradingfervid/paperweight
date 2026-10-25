package company_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

func TestBranch_RequiredFields(t *testing.T) { // MST-021
	const msg = "State is required. Pick the state where this branch is located."
	_, err := company.ValidateBranch(company.BranchInput{Code: "MUM", Name: "Mumbai Office"}, companyPAN)
	wantFieldError(t, err, "state_code", msg)

	// Code and name are required too (their messages are mstkit's, MST-001/017).
	if _, err := company.ValidateBranch(company.BranchInput{Name: "Mumbai Office", StateCode: "27"}, companyPAN); err == nil {
		t.Error("ValidateBranch(no code) = nil; want an error")
	}
	if _, err := company.ValidateBranch(company.BranchInput{Code: "MUM", StateCode: "27"}, companyPAN); err == nil {
		t.Error("ValidateBranch(no name) = nil; want an error")
	}
}

func TestBranch_GSTINStateMatch(t *testing.T) { // MST-022
	const msg = "GSTIN 27AAFCV2449G1Z7 is for Maharashtra (27) but the branch state is Karnataka (29). Fix the GSTIN or the state."
	_, err := company.ValidateBranch(company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: puneGSTIN}, companyPAN)
	wantFieldError(t, err, "gstin", msg)

	got, err := company.ValidateBranch(company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: blrGSTIN}, companyPAN)
	if err != nil {
		t.Fatalf("ValidateBranch(BLR, %s) = %v; want nil", blrGSTIN, err)
	}
	if got.StateCode != "29" || got.GSTIN != blrGSTIN {
		t.Errorf("ValidateBranch(BLR) = {StateCode %q, GSTIN %q}; want {29, %s}", got.StateCode, got.GSTIN, blrGSTIN)
	}
}

func TestBranch_GSTINMatchesCompanyPAN(t *testing.T) { // MST-023
	const msg = "GSTIN 27AABCS1429B1ZU belongs to PAN AABCS1429B, not the company PAN AAFCV2449G."
	_, err := company.ValidateBranch(company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AABCS1429B1ZU"}, companyPAN)
	wantFieldError(t, err, "gstin", msg)
}

func TestBranch_GSTINOptional(t *testing.T) { // MST-024
	in := company.BranchInput{Code: "NASHIK", Name: "Nashik Office", StateCode: "27"}
	got, err := company.ValidateBranch(in, companyPAN)
	if err != nil {
		t.Fatalf("ValidateBranch(NASHIK, no GSTIN) = %v; want nil", err)
	}
	if got.GSTIN != "" || got.StateCode != "27" {
		t.Errorf("ValidateBranch(NASHIK) = {StateCode %q, GSTIN %q}; want {27, \"\"}", got.StateCode, got.GSTIN)
	}

	u := newUnit(t)
	saveCompany(t, u.db, u.svc, u.kavya, legalName, companyPAN)
	saved := createBranch(t, u.db, u.svc, u.kavya, in)
	var code string
	if err := u.db.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		code, err = u.svc.BranchStateCode(context.Background(), tx, saved.ID)
		return err
	}); err != nil {
		t.Fatalf("BranchStateCode(NASHIK): %v", err)
	}
	if code != "27" {
		t.Errorf("BranchStateCode(NASHIK) = %q; want 27", code)
	}
}

func TestBranch_GSTINValidated(t *testing.T) { // MST-026
	// The check digit is reported, not the PAN mismatch (MST-031 message).
	const msg = "GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."
	_, err := company.ValidateBranch(company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: "27AABCS1429B1Z5"}, companyPAN)
	wantFieldError(t, err, "gstin", msg)
}

func TestBranch_LastEnabledBranch(t *testing.T) { // MST-027
	const msg = "At least one branch must stay enabled."
	env := msttest.New(t)
	ctx := context.Background()
	svc := company.New(env.Kit)
	saveCompany(t, env.DB, svc, env.Kavya, legalName, companyPAN)
	pune := createBranch(t, env.DB, svc, env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: puneGSTIN, IsHeadOffice: true})
	blr := createBranch(t, env.DB, svc, env.Kavya, company.BranchInput{Code: "BLR", Name: "Bengaluru Site Office", StateCode: "29", GSTIN: blrGSTIN})

	// BLR may go: PUNE stays enabled.
	mustWrite(t, env.DB, func(tx *db.Tx) error {
		return svc.SetDisabledTx(ctx, tx, env.Kavya, blr.ID, blr.Version, true)
	})

	err := write(env.DB, func(tx *db.Tx) error {
		return svc.SetDisabledTx(ctx, tx, env.Kavya, pune.ID, pune.Version, true)
	})
	if err == nil || err.Error() != msg {
		t.Errorf("disable PUNE (last enabled): err = %v; want %q", err, msg)
	}
	var b company.Branch
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		b, err = svc.Get(ctx, tx, pune.ID)
		return err
	}); err != nil {
		t.Fatalf("Get(PUNE): %v", err)
	}
	if b.Disabled || b.Version != pune.Version {
		t.Errorf("PUNE after refused disable: Disabled %v, Version %d; want false, %d", b.Disabled, b.Version, pune.Version)
	}
}
