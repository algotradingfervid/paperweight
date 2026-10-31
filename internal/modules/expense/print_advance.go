package expense

import (
	"context"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

func init() { print.Register(AdvanceDocType, printAdvance) }

// printAdvance is the employee advance's print view (spec §10).
func printAdvance(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	q := dbq.New(tx.SQL())
	h, err := q.E7AdvancePrint(ctx, id)
	if err != nil {
		return nil, err
	}
	doc := print.AdvanceDoc{
		Employee: employeeLabel(h.SnapEmployeeName, h.SnapEmployeeCode), Purpose: h.Purpose,
		Total: types.Money(h.Amount.Int64),
	}
	if doc.Date, err = dateOf(h.AdvanceDate); err != nil {
		return nil, err
	}
	lines, err := q.E7AdvancePrintLines(ctx, id)
	if err != nil {
		return nil, err
	}
	for i, l := range lines {
		doc.Lines = append(doc.Lines, print.AdvanceLine{
			Sr: i + 1, Type: l.SnapTypeName, Description: l.Description, Project: l.SnapProjectCode,
			Amount: types.Money(l.Amount.Int64),
		})
	}
	if doc.ApprovedBy, doc.ApprovedAt, err = approvedBy(ctx, q, AdvanceDocType, id); err != nil {
		return nil, err
	}
	if doc.Letterhead, err = print.LetterheadFor(ctx, tx, h.BranchID.Int64); err != nil {
		return nil, err
	}
	return print.AdvanceDocument(doc), nil
}
