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
	for _, name := range SubmitRequired(h.HasGoods) {
		switch {
		case name == "challan_no" && strings.Trim(h.ChallanNo, " ") == "":
			errs = append(errs, doctype.FieldError{Field: name, Message: msgChallanRequired})
		case name == "challan_kind" && h.ChallanKind == "":
			errs = append(errs, doctype.FieldError{Field: name, Message: msgChallanKind})
		}
	}
	return errs, nil
}

// SubmitProblems are submitFields on GRN id for its check page, which marks
// the step that fixes each one (R7b). The caller has checked the read right.
func SubmitProblems(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	return submitFields(ctx, tx, Deps{}, id)
}

// SubmitRequired is the header fields submit requires beyond the declared
// Required ones: the challan or invoice no. and its kind when any line is
// goods (CD-272 Gap 5), none otherwise. Step 2 marks them required.
func SubmitRequired(hasGoods bool) []string {
	if !hasGoods {
		return nil
	}
	return []string{"challan_no", "challan_kind"}
}
