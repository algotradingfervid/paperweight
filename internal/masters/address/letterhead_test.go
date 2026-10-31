package address_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TestAddress_BranchLetterheadNoDuplicates pins CD-309 (6): the branch's
// letterhead lines stop at street and city, because print.LetterheadBlock
// draws the state and GSTIN itself; the rendered letterhead shows each once.
// No Line2 checks that an empty line is skipped.
func TestAddress_BranchLetterheadNoDuplicates(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()
	s := address.New(env.Kit)
	branch := m5Branch(t, env)
	const branchGSTIN = "27AAFCV2449G1Z7"
	addrCreate(t, env, s, address.Input{BranchID: branch, Title: "Pune works", Line1: "Plot 7, Chakan MIDC",
		City: "Pune", StateCode: "27", Country: "India", Pincode: "410501", GSTIN: branchGSTIN, IsBilling: true})

	var lines []string
	var lh print.LetterheadInfo
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if lines, err = s.BranchAddressLines(ctx, tx, branch); err != nil {
			return err
		}
		lh, err = company.Letterhead{S: company.New(env.Kit), AddressLines: s.BranchAddressLines}.ForBranch(ctx, tx, branch)
		return err
	}); err != nil {
		t.Fatalf("BranchAddressLines/ForBranch(PUNE): %v", err)
	}
	want := []string{"Plot 7, Chakan MIDC", "Pune - 410501"}
	if !reflect.DeepEqual(lines, want) {
		t.Errorf("BranchAddressLines(PUNE) = %q; want %q", lines, want)
	}

	var b strings.Builder
	if err := print.LetterheadBlock(lh).Render(ctx, &b); err != nil {
		t.Fatalf("render letterhead: %v", err)
	}
	for _, s := range []string{"GSTIN: " + branchGSTIN, "Maharashtra (27)"} {
		if n := strings.Count(b.String(), s); n != 1 {
			t.Errorf("letterhead shows %q %d times; want once:\n%s", s, n, b.String())
		}
	}
}
