package company

import (
	"context"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// defaultBranchName names the head office when setup leaves it blank (CD-165).
const defaultBranchName = "Head Office"

// SetupInput is what first-install setup asks for (MST-028, CD-165).
type SetupInput struct{ LegalName, StateCode, GSTIN, PAN, BranchCode, BranchName string }

// SetupTx creates the company and its one head-office branch (MST-028,
// CD-165). The PAN comes from the GSTIN unless one is typed; CreateTx refuses
// a typed PAN the GSTIN disagrees with (MST-023), and the caller's transaction
// then drops the company too. The branch is checked first, so a bad GSTIN is
// reported as that and not as a missing PAN.
func (s *Service) SetupTx(ctx context.Context, tx *db.Tx, p auth.Principal, in SetupInput) error {
	name := in.BranchName
	if strings.TrimSpace(name) == "" {
		name = defaultBranchName
	}
	branch, err := ValidateBranch(BranchInput{Code: in.BranchCode, Name: name, StateCode: in.StateCode, GSTIN: in.GSTIN, IsHeadOffice: true}, "")
	if err != nil {
		return err
	}
	pan := strings.TrimSpace(in.PAN)
	if pan == "" {
		pan = gstin.PANFromGSTIN(branch.GSTIN)
	}
	if err := s.SaveCompanyTx(ctx, tx, p, CompanyInput{LegalName: in.LegalName, PAN: pan}, 0); err != nil {
		return err
	}
	_, err = s.CreateTx(ctx, tx, p, branch)
	return err
}
