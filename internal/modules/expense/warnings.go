package expense

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ClaimWarnings is every spec §5 warning on claim claimID for p, in line
// order, read in one transaction outside any write: the check page and the
// approval page show them; they never refuse. Another live claim is named
// only when p can read it, checked in the same read (CD-240, CD-248);
// cancelled claims don't count.
func (s *Service) ClaimWarnings(ctx context.Context, p auth.Principal, claimID int64) ([]string, error) {
	today := types.Today(s.deps.Clock)
	var out []string
	err := s.db.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		ref, err := q.E6ClaimRef(ctx, claimID)
		if errors.Is(err, sql.ErrNoRows) {
			return doc.Gone()
		}
		if err != nil {
			return err
		}
		// AUTH builds the record from the claim's Info in tx (CD-312).
		err = s.deps.Auth.CheckLinkTx(ctx, tx, p, ClaimDocType, claimID)
		if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
			return doc.Gone() // hidden looks the same as missing (CD-245)
		}
		if err != nil {
			return err
		}
		lines, err := q.E6ClaimLines(ctx, claimID)
		if err != nil {
			return err
		}
		name := func(ids []int64) (string, error) { return s.claimName(ctx, tx, p, ids) }
		for i, l := range lines {
			n := l.LineNo
			if l.ReceiptSha256 != "" {
				ids, err := q.E6SameReceipt(ctx, dbq.E6SameReceiptParams{ReceiptSha256: l.ReceiptSha256, ClaimID: claimID})
				if err != nil {
					return err
				}
				if len(ids) > 0 {
					c, err := name(ids)
					if err != nil {
						return err
					}
					out = append(out, fmt.Sprintf(msgSameReceipt, n, c))
				}
			}
			// NULL never equals NULL, so an unfinished line matches nothing.
			ids, err := q.E6SameLine(ctx, dbq.E6SameLineParams{EmployeeID: ref.EmployeeID, ExpenseDate: l.ExpenseDate,
				ExpenseTypeID: l.ExpenseTypeID, Amount: l.Amount, ClaimID: claimID})
			if err != nil {
				return err
			}
			if len(ids) > 0 {
				c, err := name(ids)
				if err != nil {
					return err
				}
				out = append(out, fmt.Sprintf(msgSameLine, n, c))
			}
			if m, ok := sameEarlierLine(lines[:i], l); ok {
				out = append(out, fmt.Sprintf(msgSameInClaim, n, m))
			}
			if l.SupplierGstin != "" && l.InvoiceNo != "" {
				ids, err := q.E6SameInvoice(ctx, dbq.E6SameInvoiceParams{SupplierGstin: l.SupplierGstin, InvoiceNo: l.InvoiceNo, ClaimID: claimID})
				if err != nil {
					return err
				}
				if len(ids) > 0 {
					c, err := name(ids)
					if err != nil {
						return err
					}
					out = append(out, fmt.Sprintf(msgSameInvoice, n, l.InvoiceNo, l.SupplierGstin, c))
				}
			}
			if l.CompanyBill == 1 && l.ExpenseDate.Valid {
				d, err := dateOf(l.ExpenseDate)
				if err != nil {
					return err
				}
				if fy, closed, barred := timeBarred(d, today); barred {
					out = append(out, fmt.Sprintf(msgTimeBarred, n, fy, types.FormatDate(closed)))
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// claimName names the first of the other claims ids that p can read, by its
// number, or "another claim" when p can read none of them.
func (s *Service) claimName(ctx context.Context, tx *db.Tx, p auth.Principal, ids []int64) (string, error) {
	for _, id := range ids {
		err := s.deps.Auth.CheckLinkTx(ctx, tx, p, ClaimDocType, id)
		if errors.Is(err, auth.ErrForbidden) {
			continue
		}
		if err != nil {
			return "", err
		}
		h, err := dbq.New(tx.SQL()).ClaimHeader(ctx, id)
		if err != nil {
			return "", err
		}
		return naming.Label(h.Number.String, id), nil
	}
	return anotherClaim, nil
}

// sameEarlierLine is the number of the first earlier line with l's date, type
// and amount, unless the two share one receipt on purpose (a split bill,
// CD-271 G2).
func sameEarlierLine(earlier []dbq.E6ClaimLinesRow, l dbq.E6ClaimLinesRow) (int64, bool) {
	if !l.ExpenseDate.Valid || !l.ExpenseTypeID.Valid || !l.Amount.Valid {
		return 0, false
	}
	for _, e := range earlier {
		if e.ExpenseDate == l.ExpenseDate && e.ExpenseTypeID == l.ExpenseTypeID && e.Amount == l.Amount &&
			(l.ReceiptSha256 == "" || e.ReceiptSha256 != l.ReceiptSha256) {
			return e.LineNo, true
		}
	}
	return 0, false
}

// timeBarred reports whether input tax credit on a bill dated d closed before
// today: the 30 November after d's financial year (CGST s.16(4), CD-271 G8).
// It returns the year's short label and that date.
func timeBarred(d, today types.Date) (string, types.Date, bool) {
	fy := types.FiscalYear(d)
	closed := types.Date{Year: fy.End.Year, Month: time.November, Day: 30}
	return fy.Short, closed, today.Compare(closed) > 0
}
