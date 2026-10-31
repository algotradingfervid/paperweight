package expense

import (
	"context"
	"database/sql"

	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Claim line defaults, set by Validate while the claim is a draft (spec §3.2,
// §9).

// typeDefaults copies the expense type's HSN/SAC and tax code into the lines
// that leave them blank (expensetype.Defaults: no tax code when the default
// one is disabled, CD-216 item 4).
func typeDefaults(ctx context.Context, tx *db.Tx, d Deps, lines []dbq.E2ClaimLinesRow) error {
	q := dbq.New(tx.SQL())
	for i, l := range lines {
		if !l.ExpenseTypeID.Valid || (l.HsnSac != "" && l.TaxCodeID.Valid) {
			continue
		}
		hsn, taxCode, err := d.ExpenseTypes.Defaults(ctx, tx, l.ExpenseTypeID.Int64)
		if err != nil {
			return err
		}
		if l.HsnSac == "" {
			lines[i].HsnSac = hsn
		}
		if !l.TaxCodeID.Valid && taxCode != 0 {
			lines[i].TaxCodeID = sql.NullInt64{Int64: taxCode, Valid: true}
		}
		if err := q.E2SetClaimLineDefaults(ctx, dbq.E2SetClaimLineDefaultsParams{
			HsnSac: lines[i].HsnSac, TaxCodeID: lines[i].TaxCodeID, ID: l.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// setBilledTo keeps Billed to for company bills only (spec §3.2): it clears
// it on a line without the tick, and sets it on a ticked line that leaves it
// blank when the enabled branches carry one GSTIN: the head-office branch
// holding it, else the one with the lowest code (CD-281 (c)). lines is
// updated too.
func setBilledTo(ctx context.Context, tx *db.Tx, lines []dbq.E2ClaimLinesRow) error {
	q := dbq.New(tx.SQL())
	n, err := BranchGSTINs(ctx, tx)
	if err != nil {
		return err
	}
	var branch int64
	if n == 1 {
		if branch, err = q.E2DefaultBranch(ctx); err != nil {
			return err
		}
	}
	for i, l := range lines {
		set := l.BilledToID
		if l.CompanyBill != 1 {
			set = sql.NullInt64{}
		} else if !set.Valid && branch != 0 {
			set = sql.NullInt64{Int64: branch, Valid: true}
		}
		if set == l.BilledToID {
			continue
		}
		lines[i].BilledToID = set
		if err := q.E2SetClaimLineBilledTo(ctx, dbq.E2SetClaimLineBilledToParams{BilledToID: set, ID: l.ID}); err != nil {
			return err
		}
	}
	return nil
}
