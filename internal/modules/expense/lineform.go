package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// LineType is one expense type on the expense line page.
type LineType struct {
	ID       int64
	Name     string
	Disabled bool
}

// BilledToBranch is one branch Billed to offers.
type BilledToBranch struct {
	ID                int64
	Code, Name, GSTIN string
}

// LineForm is what the expense line page offers (UX §5.5): every expense
// type by name (only enabled ones are offered), the branches Billed to
// offers by code, and BranchGSTINs, read in one transaction.
type LineForm struct {
	Types        []LineType
	Branches     []BilledToBranch
	BranchGSTINs int
}

// LineForm reads the expense line page's choices. Expense types and
// branches carry no restrictions, so every user who can write a claim gets
// them all.
func (s *Service) LineForm(ctx context.Context) (LineForm, error) {
	var f LineForm
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		types, err := q.S2LineTypes(ctx)
		if err != nil {
			return err
		}
		for _, t := range types {
			f.Types = append(f.Types, LineType{ID: t.ID, Name: t.Name, Disabled: t.Disabled != 0})
		}
		branches, err := q.S2BilledTo(ctx)
		if err != nil {
			return err
		}
		for _, b := range branches {
			f.Branches = append(f.Branches, BilledToBranch{ID: b.ID, Code: b.Code, Name: b.Name, GSTIN: b.Gstin})
		}
		f.BranchGSTINs, err = BranchGSTINs(ctx, tx)
		return err
	})
	return f, err
}
