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

// submitProblems is everything BeforeSubmit refuses on GRN id beyond DOC's
// own checks: the period lock, as a receipt_date error (CD-282 Q9), then
// the submit-only rules (CD-264 (2)).
func submitProblems(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	errs, err := checkLockFields(ctx, tx, d, id)
	if err != nil {
		return nil, err
	}
	more, err := submitFields(ctx, tx, d, id)
	if err != nil {
		return nil, err
	}
	return append(errs, more...), nil
}

// SubmitProblems is submitProblems for GRN id's check page, which marks the
// step that fixes each one (R7b). The caller has checked the read right.
func SubmitProblems(ctx context.Context, tx *db.Tx, id int64) ([]doctype.FieldError, error) {
	d, err := wiring.Get(tx)
	if err != nil {
		return nil, err
	}
	return submitProblems(ctx, tx, d, id)
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
