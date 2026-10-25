package company

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// SetupInput is what first-install setup asks for (MST-028).
type SetupInput struct{ LegalName, StateCode, GSTIN string }

// SetupTx creates the company, its PAN taken from the GSTIN, and one
// head-office branch HO (MST-028).
func (s *Service) SetupTx(ctx context.Context, tx *db.Tx, p auth.Principal, in SetupInput) error {
	pan := gstin.PANFromGSTIN(gstin.Normalize(in.GSTIN))
	if err := s.SaveCompanyTx(ctx, tx, p, CompanyInput{LegalName: in.LegalName, PAN: pan}, 0); err != nil {
		return err
	}
	_, err := s.CreateTx(ctx, tx, p, BranchInput{Code: "HO", Name: "Head Office", StateCode: in.StateCode, GSTIN: in.GSTIN, IsHeadOffice: true})
	return err
}
