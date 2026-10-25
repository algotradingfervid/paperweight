package company_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var _ print.Letterhead = company.Letterhead{}

func TestCompany_LetterheadForBranch(t *testing.T) { // CD-30, CD-58, CD-140, CD-154
	env := msttest.New(t)
	ctx := context.Background()
	svc := company.New(env.Kit)
	saveCompany(t, env.DB, svc, env.Kavya, legalName, companyPAN)
	pune := createBranch(t, env.DB, svc, env.Kavya, company.BranchInput{Code: "PUNE", Name: "Pune Works", StateCode: "27", GSTIN: puneGSTIN, IsHeadOffice: true})

	var askedFor []int64
	lines := func(_ context.Context, _ *db.Tx, branchID int64) ([]string, error) {
		askedFor = append(askedFor, branchID)
		return []string{"Pune 411019"}, nil
	}
	want := print.LetterheadInfo{
		LegalName:  legalName,
		BranchName: "Pune Works",
		Address:    []string{"Pune 411019"},
		StateName:  "Maharashtra",
		StateCode:  "27",
		GSTIN:      puneGSTIN,
	}

	for _, tc := range []struct {
		name    string
		logo    func(ctx context.Context, tx *db.Tx) (string, error)
		wantURL string
	}{
		{"no logo source gives the brand logo", nil, brand.DocumentLogoURL},
		{"empty uploaded logo gives the brand logo", func(context.Context, *db.Tx) (string, error) { return "", nil }, brand.DocumentLogoURL},
		{"uploaded logo", func(context.Context, *db.Tx) (string, error) { return "/files/7", nil }, "/files/7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			askedFor = nil
			lh := company.Letterhead{S: svc, AddressLines: lines, LogoURL: tc.logo}
			var got print.LetterheadInfo
			if err := env.DB.Read(ctx, func(tx *db.Tx) error {
				var err error
				got, err = lh.ForBranch(ctx, tx, pune.ID)
				return err
			}); err != nil {
				t.Fatalf("ForBranch(PUNE): %v", err)
			}
			w := want
			w.LogoURL = tc.wantURL
			if !reflect.DeepEqual(got, w) {
				t.Errorf("ForBranch(PUNE) = %+v; want %+v", got, w)
			}
			if !reflect.DeepEqual(askedFor, []int64{pune.ID}) {
				t.Errorf("AddressLines called with %v; want [%d]", askedFor, pune.ID)
			}
		})
	}
}
