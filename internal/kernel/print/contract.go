// Package print renders printable documents (spec §5). FILE owns it.
package print

import (
	"context"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

func InWordsINR(m types.Money) (string, error) { return "", errs.ErrNotImplemented }

type LetterheadInfo struct { // CD-58
	LegalName, LogoURL, BranchName string
	Address                        []string
	StateName, StateCode, GSTIN    string
}

type Letterhead interface { // CD-30: implemented by masters/company
	ForBranch(ctx context.Context, tx *db.Tx, branchID int64) (LetterheadInfo, error)
}
