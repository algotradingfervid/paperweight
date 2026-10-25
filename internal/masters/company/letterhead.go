package company

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Letterhead implements print.Letterhead (CD-30, CD-154). AddressLines gives
// the branch's address (address imports company, so allmasters wires it);
// LogoURL gives the company's uploaded logo, and a nil LogoURL or "" means
// brand.DocumentLogoURL (CD-140).
type Letterhead struct {
	S            *Service
	AddressLines func(ctx context.Context, tx *db.Tx, branchID int64) ([]string, error)
	LogoURL      func(ctx context.Context, tx *db.Tx) (string, error)
}

var _ print.Letterhead = Letterhead{}

// ForBranch fills the letterhead from the company, the branch and its
// address. No company yet is doctype.ErrNotFound.
func (l Letterhead) ForBranch(ctx context.Context, tx *db.Tx, branchID int64) (print.LetterheadInfo, error) {
	c, ok, err := l.S.Company(ctx, tx)
	if err != nil {
		return print.LetterheadInfo{}, err
	}
	if !ok {
		return print.LetterheadInfo{}, doctype.ErrNotFound
	}
	b, err := l.S.Get(ctx, tx, branchID)
	if err != nil {
		return print.LetterheadInfo{}, err
	}
	var lines []string
	if l.AddressLines != nil {
		if lines, err = l.AddressLines(ctx, tx, branchID); err != nil {
			return print.LetterheadInfo{}, err
		}
	}
	logo := ""
	if l.LogoURL != nil {
		if logo, err = l.LogoURL(ctx, tx); err != nil {
			return print.LetterheadInfo{}, err
		}
	}
	if logo == "" {
		logo = brand.DocumentLogoURL
	}
	state := b.gstState()
	stateName, _ := gstcalc.StateName(state)
	return print.LetterheadInfo{
		LegalName:  c.LegalName,
		LogoURL:    logo,
		BranchName: b.Name,
		Address:    lines,
		StateName:  stateName,
		StateCode:  state,
		GSTIN:      b.GSTIN,
	}, nil
}
