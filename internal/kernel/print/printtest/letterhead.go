package printtest

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Branch IDs the Letterhead fake knows (00-fixtures.md F1).
const (
	PuneID int64 = 1
	BLRID  int64 = 2
)

// Letterheads is a map-backed print.Letterhead keyed by branch ID.
type Letterheads map[int64]print.LetterheadInfo

// ForBranch returns the letterhead for branchID, or doctype.ErrNotFound.
func (l Letterheads) ForBranch(_ context.Context, _ *db.Tx, branchID int64) (print.LetterheadInfo, error) {
	info, ok := l[branchID]
	if !ok {
		return print.LetterheadInfo{}, fmt.Errorf("printtest: branch %d: %w", branchID, doctype.ErrNotFound)
	}
	return info, nil
}

// Letterhead is the fixture company's letterheads for PUNE and BLR.
var Letterhead = Letterheads{
	PuneID: {
		LegalName: "Vardhan Infra Projects Pvt Ltd", LogoURL: "/files/1", BranchName: "Pune Works",
		Address: []string{"Pune, Maharashtra"}, StateName: "Maharashtra", StateCode: "27", GSTIN: "27AAFCV2449G1Z7",
	},
	BLRID: {
		LegalName: "Vardhan Infra Projects Pvt Ltd", LogoURL: "/files/1", BranchName: "Bengaluru Site Office",
		Address: []string{"Bengaluru, Karnataka"}, StateName: "Karnataka", StateCode: "29", GSTIN: "29AAFCV2449G1Z3",
	},
}
