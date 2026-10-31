package expense

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Prints (spec §10): filled from the snapshots, on the claimant's branch
// letterhead, read only through the print page's tx.

func init() { print.Register(ClaimDocType, printClaim) }

// printClaim is the expense claim's print view. The advance number prints
// only when the viewer can read the advance (CD-233).
func printClaim(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	d, err := wiring.Get(tx)
	if err != nil {
		return nil, err
	}
	q := dbq.New(tx.SQL())
	h, err := q.E7ClaimPrint(ctx, id)
	if err != nil {
		return nil, err
	}
	doc := print.ExpenseDoc{
		Employee: employeeLabel(h.SnapEmployeeName, h.SnapEmployeeCode), Purpose: h.Purpose,
		GrandTotal: types.Money(h.Total.Int64), LessAdvance: types.Money(h.LessAdvance.Int64),
		NetPayable: types.Money(h.NetPayable.Int64), HasNetPayable: h.NetPayable.Valid,
	}
	if doc.PeriodFrom, err = dateOf(h.PeriodFrom); err != nil {
		return nil, err
	}
	if doc.PeriodTo, err = dateOf(h.PeriodTo); err != nil {
		return nil, err
	}
	if h.AdvanceID.Valid {
		p, _ := auth.PrincipalFrom(ctx)
		var le *auth.LinkError
		switch err := d.Auth.CheckLinkTx(ctx, tx, p, AdvanceDocType, h.AdvanceID.Int64); {
		case err == nil:
			doc.AdvanceNumber = h.AdvanceNumber.String
		case !errors.As(err, &le):
			return nil, err
		}
	}
	files, err := d.Files.ListTx(ctx, tx, auth.RecordRef{DocType: ClaimDocType, ID: id})
	if err != nil {
		return nil, err
	}
	attached := make(map[string]bool, len(files))
	for _, f := range files {
		attached[f.SHA256] = true
		doc.Attachments = append(doc.Attachments, f.Name)
	}
	lines, err := q.E7ClaimPrintLines(ctx, id)
	if err != nil {
		return nil, err
	}
	for i, l := range lines {
		date, err := dateOf(l.ExpenseDate)
		if err != nil {
			return nil, err
		}
		doc.Lines = append(doc.Lines, print.ExpenseLine{
			Sr: i + 1, Date: date, Type: l.SnapTypeName, Description: l.Description,
			Amount: types.Money(l.Amount.Int64), Project: l.SnapProjectCode,
			HasReceipt: attached[l.ReceiptSha256],
		})
	}
	if doc.ApprovedBy, doc.ApprovedAt, err = approvedBy(ctx, q, ClaimDocType, id); err != nil {
		return nil, err
	}
	if doc.Letterhead, err = print.LetterheadFor(ctx, tx, h.BranchID.Int64); err != nil {
		return nil, err
	}
	return print.ExpenseDocument(doc), nil
}

// employeeLabel is the snapshot name with its code: `Deepak Nair (EMP-007)`.
func employeeLabel(name, code string) string { return name + " (" + code + ")" }

// approvedBy is the name and time of the document's last approval, with a
// rule or without one; blank when it was never approved.
func approvedBy(ctx context.Context, q *dbq.Queries, dt string, id int64) (string, time.Time, error) {
	r, err := q.E7ApprovedBy(ctx, dbq.E7ApprovedByParams{Doctype: dt, DocID: id})
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, nil
	}
	if err != nil {
		return "", time.Time{}, err
	}
	at, err := types.ParseTimestamp(r.At)
	if err != nil {
		return "", time.Time{}, err
	}
	return r.UserName, at.Time(), nil
}
