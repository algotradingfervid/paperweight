package uom

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// QtyUse names a lines table that stores quantities in a UoM, so turning a
// UoM's whole-number flag on can look for fractional quantities (MST-092,
// Task 26).
type QtyUse struct {
	DocType, LinesTable, ParentColumn, UoMColumn, QtyColumn string
}

var qtyUses []QtyUse

// RegisterQtyUse records u. Call it from a package init().
func RegisterQtyUse(u QtyUse) { qtyUses = append(qtyUses, u) }

// fractionalUse returns the label of a document outside draft with a fractional
// quantity in UoM id. Drafts don't count, as for the other locks: their next
// save is refused instead (MST-086).
func fractionalUse(ctx context.Context, tx *db.Tx, id int64) (string, bool, error) {
	for _, u := range qtyUses {
		dt, ok := doctype.Lookup(u.DocType)
		if !ok {
			return "", false, fmt.Errorf("uom: QtyUse names unknown DocType %q", u.DocType)
		}
		var docID int64
		var number string
		err := tx.SQL().QueryRowContext(ctx,
			"SELECT h.id, COALESCE(h.number, '') FROM "+u.LinesTable+" l JOIN "+dt.Table+" h ON h.id = l."+u.ParentColumn+
				" WHERE l."+u.UoMColumn+" = ? AND l."+u.QtyColumn+" % ? != 0 AND h.state != ? ORDER BY h.id LIMIT 1", //nolint:gosec // G202: names come from RegisterQtyUse and the registry, never from a request
			id, qtyUnit, string(doctype.StateDraft)).Scan(&docID, &number)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", false, err
		}
		return naming.Label(number, docID), true, nil
	}
	return "", false, nil
}
