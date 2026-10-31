package modkit

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// CheckNotLocked refuses (422) a document dated on or before the period lock
// (PO spec §14.2). label is the DocType's label and display
// naming.Label(number, id). Callers run it inside their transaction, from
// BeforeSubmit and ReverseEffects.
func CheckNotLocked(ctx context.Context, tx *db.Tx, label, display string, date types.Date) error {
	s, err := readSettings(ctx, tx)
	if err != nil {
		return err
	}
	if !s.LockedUpTo.Valid || date.Compare(s.LockedUpTo.V) > 0 {
		return nil
	}
	return &doc.Error{Kind: doc.KindInvalid, Message: fmt.Sprintf(msgLocked, label, display, types.FormatDate(date), types.FormatDate(s.LockedUpTo.V))}
}
