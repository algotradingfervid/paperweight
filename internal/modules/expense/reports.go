package expense

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Reports (spec §13). Each reads only the rows p may read (Auth.Scope for
// read); labels of other records come from the row's snapshots, so a report
// names nothing p couldn't already see (CD-233, CD-240). Dates are IST
// business dates. The CSV handlers check export (CD-145).

// OpenClaimRow is one row of the open claims report. ApprovedOn (the zero
// Date) and ApprovedBy are blank for a claim with no approval history.
type OpenClaimRow struct {
	ID                       int64
	Claim, Employee, Purpose string
	ApprovedOn               types.Date
	ApprovedBy               string
	Total, LessAdvance       types.Money
	ToPay                    types.Money
	NoReceipt                int // lines without a receipt
}

// OpenClaimsReport is the approved, not closed claims p may read.
func (s *Service) OpenClaimsReport(ctx context.Context, p auth.Principal) ([]OpenClaimRow, error) {
	var out []OpenClaimRow
	err := s.scoped(ctx, p, ClaimDocType, func(tx *db.Tx, in func(int64) bool) error {
		rows, err := dbq.New(tx.SQL()).E8OpenClaims(ctx)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.ID) {
				continue
			}
			row := OpenClaimRow{
				ID: r.ID, Claim: r.Number, Employee: r.SnapEmployeeName, Purpose: r.Purpose, ApprovedBy: r.ApprovedBy.String,
				Total: types.Money(r.Total), LessAdvance: types.Money(r.LessAdvance), ToPay: types.Money(r.NetPayable),
				NoReceipt: int(r.NoReceipt),
			}
			if r.ApprovedAt.Valid {
				at, err := types.ParseTimestamp(r.ApprovedAt.String)
				if err != nil {
					return err
				}
				row.ApprovedOn = types.BusinessDate(at.Time())
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

// AdvancesFilter narrows the open advances report; 0 is every employee.
type AdvancesFilter struct{ EmployeeID int64 }

// AdvanceRow is one row of the open advances report. PaidOn is nil while
// the advance is unpaid.
type AdvanceRow struct {
	ID                int64
	Advance, Employee string
	Date              types.Date
	Purpose           string
	Amount            types.Money
	PaidOn            *types.Date
	Used, Left        types.Money
	AgeDays           int // IST today − the advance date
}

// AdvancesReport is the approved, not closed advances p may read.
func (s *Service) AdvancesReport(ctx context.Context, p auth.Principal, f AdvancesFilter) ([]AdvanceRow, error) {
	today := types.Today(s.deps.Clock)
	var out []AdvanceRow
	err := s.scoped(ctx, p, AdvanceDocType, func(tx *db.Tx, in func(int64) bool) error {
		rows, err := dbq.New(tx.SQL()).E8OpenAdvances(ctx, f.EmployeeID)
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.ID) {
				continue
			}
			date, err := types.DateFromISO(r.AdvanceDate)
			if err != nil {
				return err
			}
			row := AdvanceRow{
				ID: r.ID, Advance: r.Number, Employee: r.SnapEmployeeName, Date: date, Purpose: r.Purpose,
				Amount: types.Money(r.Amount), Used: types.Money(r.UsedAmount), Left: types.Money(r.Amount - r.UsedAmount),
				AgeDays: types.DaysBetween(date, today),
			}
			if r.PaidOn.Valid {
				paid, err := types.DateFromISO(r.PaidOn.String)
				if err != nil {
					return err
				}
				row.PaidOn = &paid
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

// LinesFilter narrows the expense lines report. Month is "YYYY-MM" of the
// line date and "" every month; a 0 id is every type, project or employee.
type LinesFilter struct {
	Month                         string
	TypeID, ProjectID, EmployeeID int64
}

// LineRow is one line of an approved or closed claim. Project, Site and
// BilledToGSTIN are the snapshot codes; Receipt is whether a receipt is
// linked.
type LineRow struct {
	ClaimID                                 int64
	Claim, Employee                         string
	Date                                    types.Date
	Type, Project, Site, Note               string
	Amount, GST                             types.Money
	SupplierGSTIN, InvoiceNo, BilledToGSTIN string
	Receipt                                 bool
	State                                   doc.State
}

// LinesReport is the expense lines report's rows and their total amount.
type LinesReport struct {
	Rows  []LineRow
	Total types.Money
}

// ExpenseLinesReport is the lines of the approved and closed claims p may
// read that match f (CD-271 G6).
func (s *Service) ExpenseLinesReport(ctx context.Context, p auth.Principal, f LinesFilter) (LinesReport, error) {
	var rep LinesReport
	err := s.scoped(ctx, p, ClaimDocType, func(tx *db.Tx, in func(int64) bool) error {
		rows, err := dbq.New(tx.SQL()).E8ClaimLines(ctx, dbq.E8ClaimLinesParams{
			Month: f.Month, TypeID: f.TypeID, ProjectID: f.ProjectID, EmployeeID: f.EmployeeID,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.ClaimID) {
				continue
			}
			date, err := types.DateFromISO(r.ExpenseDate)
			if err != nil {
				return err
			}
			rep.Rows = append(rep.Rows, LineRow{
				ClaimID: r.ClaimID, Claim: r.Number, Employee: r.SnapEmployeeName, Date: date,
				Type: r.SnapTypeName, Project: r.SnapProjectCode, Site: r.SnapSiteCode, Note: r.Description,
				Amount: types.Money(r.Amount), GST: types.Money(r.GstAmount),
				SupplierGSTIN: r.SupplierGstin, InvoiceNo: r.InvoiceNo, BilledToGSTIN: r.SnapBilledGstin,
				Receipt: r.ReceiptSha256 != "", State: doc.State(r.State),
			})
			rep.Total += types.Money(r.Amount)
		}
		return nil
	})
	return rep, err
}

// scoped runs fn in one read transaction with in, which reports whether a
// row id of dt's table is in p's read scope. Scope is asked before the
// transaction opens: it takes its own connection.
func (s *Service) scoped(ctx context.Context, p auth.Principal, dt string, fn func(tx *db.Tx, in func(int64) bool) error) error {
	pred, err := s.deps.Auth.Scope(ctx, p, dt, auth.RightRead)
	if err != nil {
		return err
	}
	typ, _ := doctype.Lookup(dt) // registered: Scope refuses an unknown DocType
	table := typ.Table
	return s.db.Read(ctx, func(tx *db.Tx) error {
		if pred.SQL == "" {
			return fn(tx, func(int64) bool { return true })
		}
		rows, err := tx.SQL().QueryContext(ctx, `SELECT "`+table+`".id FROM "`+table+`" WHERE `+pred.SQL, pred.Args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		ids := map[int64]bool{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			ids[id] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return fn(tx, func(id int64) bool { return ids[id] })
	})
}
