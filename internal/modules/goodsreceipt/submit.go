package goodsreceipt

import (
	"context"
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// submitFields are the submit-only rules (GRN spec §4, Submit; CD-264 (2)):
// a rejected quantity needs a reason (the text numbers the PO line, the
// error's Line the posted position; AD-12), and a GRN with any goods line
// needs the challan or invoice no. and its kind (CD-272 Gap 5).
// BeforeSubmit returns them with the lock's error as one doc.Invalid.
func submitFields(ctx context.Context, tx *db.Tx, _ Deps, id int64) ([]doctype.FieldError, error) {
	q := dbq.New(tx.SQL())
	lines, err := q.G2RejectedNoReason(ctx, id)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	for _, l := range lines {
		errs = append(errs, doctype.FieldError{Field: "reject_reason", Line: int(l.LineNo), Message: fmt.Sprintf(msgRejectNoReason, l.PoLineNo)})
	}
	h, err := q.G2SubmitHeader(ctx, id)
	if err != nil {
		return nil, err
	}
	if h.HasGoods && strings.Trim(h.ChallanNo, " ") == "" {
		errs = append(errs, doctype.FieldError{Field: "challan_no", Message: msgChallanRequired})
	}
	if h.HasGoods && h.ChallanKind == "" {
		errs = append(errs, doctype.FieldError{Field: "challan_kind", Message: msgChallanKind})
	}
	return errs, nil
}
