package approval

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// HasRules reports whether an enabled approval rule exists for doctype, the
// rules submit evaluates, read through tx. It is open to every caller
// (CD-258 Q7(a), CD-280).
func (a *Approvals) HasRules(ctx context.Context, tx *db.Tx, doctype string) (bool, error) {
	return dbq.New(tx.SQL()).HasRulesForDocType(ctx, doctype)
}
