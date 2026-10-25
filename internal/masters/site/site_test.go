package site_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
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
