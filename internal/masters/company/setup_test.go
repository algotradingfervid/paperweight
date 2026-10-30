package company_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const panRequiredMsg = "PAN is required."

// setup runs SetupTx in db.Write and returns its error.
func setup(d *db.DB, s *company.Service, p auth.Principal, in company.SetupInput) error {
	return write(d, func(tx *db.Tx) error {
		return s.SetupTx(context.Background(), tx, p, in)
	})
}

// readBranch returns the branch with code, failing the test when it is missing.
func readBranch(t *testing.T, d *db.DB, s *company.Service, code string) company.Branch {
	t.Helper()
	var (
		b  company.Branch
		ok bool
	)
	err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		b, ok, err = s.ByCode(context.Background(), tx, code)
		return err
	})
	if err != nil {
		t.Fatalf("ByCode(%s): %v", code, err)
	}
	if !ok {
		t.Fatalf("ByCode(%s) ok = false; want the branch", code)
	}
	return b
}

// countBranches returns how many branches exist, and how many are head offices.
func countBranches(t *testing.T, d *db.DB) (all, headOffices int) {
	t.Helper()
	ctx := context.Background()
	err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT count(*), coalesce(sum(is_head_office), 0) FROM branch`).Scan(&all, &headOffices)
	})
	if err != nil {
		t.Fatalf("count branches: %v", err)
	}
	return all, headOffices
}

// wantNothingStored checks that a refused setup left no company and no branch.
func wantNothingStored(t *testing.T, u *unit) {
	t.Helper()
	if c, ok := readCompany(t, u.db, u.svc); ok {
		t.Errorf("company stored %+v; want none", c)
	}
	if all, _ := countBranches(t, u.db); all != 0 {
		t.Errorf("branches = %d; want 0", all)
	}
}

func TestSetup_SeedsCompanyAndBranch(t *testing.T) { // MST-028, CD-165
	env := msttest.New(t)
	svc := company.New(env.Kit)
	err := setup(env.DB, svc, env.Kavya, company.SetupInput{
		LegalName: legalName, StateCode: "27", GSTIN: puneGSTIN, BranchCode: "PUNE", BranchName: "Pune Works",
	})
	if err != nil {
		t.Fatalf("SetupTx: %v", err)
	}

	c, ok := readCompany(t, env.DB, svc)
	if !ok || c.LegalName != legalName || c.PAN != companyPAN {
		t.Errorf("Company() = %+v, %v; want %s / %s", c, ok, legalName, companyPAN)
	}
	b := readBranch(t, env.DB, svc, "PUNE")
	if b.Name != "Pune Works" || b.StateCode != "27" || b.GSTIN != puneGSTIN || !b.IsHeadOffice {
		t.Errorf("branch PUNE = %+v; want Pune Works, state 27, %s, head office", b, puneGSTIN)
	}
	if all, headOffices := countBranches(t, env.DB); all != 1 || headOffices != 1 {
		t.Errorf("branches = %d, head offices = %d; want 1, 1", all, headOffices)
	}
}

func TestSetup_BlankBranchNameIsHeadOffice(t *testing.T) { // CD-165
	for _, name := range []string{"", "   "} {
		t.Run("name "+name, func(t *testing.T) {
			u := newUnit(t)
			err := setup(u.db, u.svc, u.kavya, company.SetupInput{
				LegalName: legalName, StateCode: "27", GSTIN: puneGSTIN, BranchCode: "PUNE", BranchName: name,
			})
			if err != nil {
				t.Fatalf("SetupTx: %v", err)
			}
			if b := readBranch(t, u.db, u.svc, "PUNE"); b.Name != "Head Office" {
				t.Errorf("branch name = %q; want Head Office", b.Name)
			}
		})
	}
}

func TestSetup_BranchCodeRules(t *testing.T) { // CD-165
	cases := []struct{ name, code, msg string }{
		{"blank", "", "Code is required. Enter a short code such as SHREE."},
		{"format", "PUNE WORKS", "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := newUnit(t)
			err := setup(u.db, u.svc, u.kavya, company.SetupInput{
				LegalName: legalName, StateCode: "27", GSTIN: puneGSTIN, BranchCode: tc.code,
			})
			wantFieldError(t, err, "code", tc.msg)
			wantNothingStored(t, u)
		})
	}
}

func TestSetup_UnregisteredCompany(t *testing.T) { // CD-165: blank GSTIN, PAN typed
	u := newUnit(t)
	err := setup(u.db, u.svc, u.kavya, company.SetupInput{
		LegalName: legalName, StateCode: "27", PAN: "aafcv2449g", BranchCode: "PUNE",
	})
	if err != nil {
		t.Fatalf("SetupTx: %v", err)
	}
	if c, ok := readCompany(t, u.db, u.svc); !ok || c.PAN != companyPAN {
		t.Errorf("Company() = %+v, %v; want PAN %s", c, ok, companyPAN)
	}
	if b := readBranch(t, u.db, u.svc, "PUNE"); b.GSTIN != "" || !b.IsHeadOffice {
		t.Errorf("branch PUNE = %+v; want no GSTIN, head office", b)
	}
}

func TestSetup_NoGSTINNoPANRefused(t *testing.T) { // CD-165
	u := newUnit(t)
	err := setup(u.db, u.svc, u.kavya, company.SetupInput{
		LegalName: legalName, StateCode: "27", BranchCode: "PUNE",
	})
	wantFieldError(t, err, "pan", panRequiredMsg)
	wantNothingStored(t, u)
}

func TestCompany_PANRequired(t *testing.T) { // CD-165: a direct save, as setup
	u := newUnit(t)
	err := write(u.db, func(tx *db.Tx) error {
		return u.svc.SaveCompanyTx(context.Background(), tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: "  "}, 0)
	})
	wantFieldError(t, err, "pan", panRequiredMsg)
	wantNothingStored(t, u)
}

func TestSetup_TypedPANMustMatchGSTIN(t *testing.T) { // CD-165, MST-023
	t.Run("agrees", func(t *testing.T) {
		u := newUnit(t)
		err := setup(u.db, u.svc, u.kavya, company.SetupInput{
			LegalName: legalName, StateCode: "27", GSTIN: puneGSTIN, PAN: "aafcv2449g", BranchCode: "PUNE",
		})
		if err != nil {
			t.Fatalf("SetupTx: %v", err)
		}
		if c, ok := readCompany(t, u.db, u.svc); !ok || c.PAN != companyPAN {
			t.Errorf("Company() = %+v, %v; want PAN %s", c, ok, companyPAN)
		}
	})
	t.Run("disagrees", func(t *testing.T) {
		u := newUnit(t)
		err := setup(u.db, u.svc, u.kavya, company.SetupInput{
			LegalName: legalName, StateCode: "27", GSTIN: puneGSTIN, PAN: "AABCS1429B", BranchCode: "PUNE",
		})
		wantFieldError(t, err, "gstin", "GSTIN 27AAFCV2449G1Z7 belongs to PAN AAFCV2449G, not the company PAN AABCS1429B.")
		wantNothingStored(t, u)
	})
}

// A GSTIN too short to hold a PAN is reported as a GSTIN problem, not as a
// missing PAN.
func TestSetup_BadGSTINNamesTheGSTIN(t *testing.T) { // CD-165, MST-030
	u := newUnit(t)
	err := setup(u.db, u.svc, u.kavya, company.SetupInput{
		LegalName: legalName, StateCode: "27", GSTIN: "27AAFCV", BranchCode: "PUNE",
	})
	wantFieldError(t, err, "gstin", "GSTIN 27AAFCV must have 15 characters.")
	wantNothingStored(t, u)
}
