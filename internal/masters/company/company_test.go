package company_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// Unit tests use the literal 06-masters.md values; for Company and Branch
// these already are the canonical fixtures (M7, CD-131).
const (
	legalName  = "Vardhan Infra Projects Pvt Ltd"
	companyPAN = "AAFCV2449G"
	puneGSTIN  = "27AAFCV2449G1Z7"
	blrGSTIN   = "29AAFCV2449G1Z3"

	panMsg       = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."
	singletonMsg = "This install supports one company. Add a branch instead."
)

// unit is a company Service on a migrated test DB with AUTH, DOC and audit
// fakes (no msttest): Kavya may read and write Company and do everything on
// Branch.
type unit struct {
	svc   *company.Service
	db    *db.DB
	kavya auth.Principal
}

func newUnit(t *testing.T) *unit {
	t.Helper()
	u := &unit{db: testkit.NewDB(t), kavya: testkit.Principal(t, "kavya@vardhan.test")}
	az := authfake.New()
	az.AddRole(u.kavya.ID, "Masters Manager")
	az.Allow(u.kavya.ID, "Company", auth.RightRead, auth.RightWrite)
	az.Allow(u.kavya.ID, "Branch", auth.RightRead, auth.RightCreate, auth.RightWrite, auth.RightDelete)
	env := mstkit.Env{DB: u.db, Auth: az, Engine: docfake.New(), Auditor: auditfake.New(), Clock: testkit.Clock()}
	u.svc = company.New(env)
	return u
}

// write runs fn in db.Write and returns its error.
func write(d *db.DB, fn func(tx *db.Tx) error) error {
	return d.Write(context.Background(), fn)
}

// mustWrite runs fn in db.Write and fails the test on error.
func mustWrite(t *testing.T, d *db.DB, fn func(tx *db.Tx) error) {
	t.Helper()
	if err := write(d, fn); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// readCompany returns the stored company, failing the test on a read error.
func readCompany(t *testing.T, d *db.DB, s *company.Service) (company.Company, bool) {
	t.Helper()
	var (
		c  company.Company
		ok bool
	)
	err := d.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		c, ok, err = s.Company(context.Background(), tx)
		return err
	})
	if err != nil {
		t.Fatalf("Company: %v", err)
	}
	return c, ok
}

// saveCompany creates the company (version 0: none exists yet).
func saveCompany(t *testing.T, d *db.DB, s *company.Service, p auth.Principal, name, pan string) {
	t.Helper()
	mustWrite(t, d, func(tx *db.Tx) error {
		return s.SaveCompanyTx(context.Background(), tx, p, company.CompanyInput{LegalName: name, PAN: pan}, 0)
	})
}

// createBranch creates a branch and returns what was saved.
func createBranch(t *testing.T, d *db.DB, s *company.Service, p auth.Principal, in company.BranchInput) mstkit.Saved {
	t.Helper()
	var saved mstkit.Saved
	mustWrite(t, d, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(context.Background(), tx, p, in)
		return err
	})
	return saved
}

// wantFieldError checks that err is a 422 *mstkit.ValidationError holding
// exactly one field error, on field, with message msg.
func wantFieldError(t *testing.T, err error, field, msg string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v (%T); want *mstkit.ValidationError %q", err, err, msg)
	}
	if ve.Status() != 422 {
		t.Errorf("Status() = %d; want 422", ve.Status())
	}
	want := []doctype.FieldError{{Field: field, Message: msg}}
	got := ve.FieldErrors()
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("FieldErrors() = %+v; want %+v", got, want)
	}
}

func TestCompany_PANFormat(t *testing.T) { // MST-020
	u := newUnit(t)
	for _, bad := range []string{"AAFCV2449", "1AFCV2449G"} {
		err := write(u.db, func(tx *db.Tx) error {
			return u.svc.SaveCompanyTx(context.Background(), tx, u.kavya, company.CompanyInput{LegalName: legalName, PAN: bad}, 0)
		})
		wantFieldError(t, err, "pan", panMsg)
		if c, ok := readCompany(t, u.db, u.svc); ok {
			t.Errorf("after PAN %q: company stored %+v; want none", bad, c)
		}
	}

	saveCompany(t, u.db, u.svc, u.kavya, legalName, "aafcv2449g")
	c, ok := readCompany(t, u.db, u.svc)
	if !ok {
		t.Fatal("Company() ok = false after save; want the saved company")
	}
	if c.PAN != companyPAN || c.LegalName != legalName {
		t.Errorf("Company() = {LegalName %q, PAN %q}; want {%q, %q}", c.LegalName, c.PAN, legalName, companyPAN)
	}
}

func TestCompany_Singleton(t *testing.T) { // MST-019
	env := msttest.New(t)
	ctx := context.Background()
	svc := company.New(env.Kit)
	saveCompany(t, env.DB, svc, env.Kavya, legalName, companyPAN)

	// A second company: a create (version 0) while one exists.
	err := write(env.DB, func(tx *db.Tx) error {
		return svc.SaveCompanyTx(ctx, tx, env.Kavya, company.CompanyInput{LegalName: "Shree Steel Traders", PAN: "AABCS1429B"}, 0)
	})
	if err == nil || err.Error() != singletonMsg {
		t.Errorf("second company: err = %v; want %q", err, singletonMsg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 422 {
		t.Errorf("second company: err = %v; want a 422 doctype.HTTPError", err)
	}
	c, ok := readCompany(t, env.DB, svc)
	if !ok || c.LegalName != legalName || c.PAN != companyPAN {
		t.Fatalf("after second company: Company() = %+v, %v; want %s / %s unchanged", c, ok, legalName, companyPAN)
	}
	var n int
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM company`).Scan(&n)
	}); err != nil {
		t.Fatalf("count company: %v", err)
	}
	if n != 1 {
		t.Errorf("company rows = %d; want 1", n)
	}

	// Deleting the company is refused with the same message.
	err = write(env.DB, func(tx *db.Tx) error {
		return svc.DeleteCompanyTx(ctx, tx, env.Kavya, c.Version)
	})
	if err == nil || err.Error() != singletonMsg {
		t.Errorf("delete company: err = %v; want %q", err, singletonMsg)
	}
	if !errors.As(err, &he) || he.Status() != 422 {
		t.Errorf("delete company: err = %v; want a 422 doctype.HTTPError", err)
	}
	if after, ok := readCompany(t, env.DB, svc); !ok || after.LegalName != legalName || after.Version != c.Version {
		t.Errorf("after delete: Company() = %+v, %v; want %+v still there", after, ok, c)
	}
}
