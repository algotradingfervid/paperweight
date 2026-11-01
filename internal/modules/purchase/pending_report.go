package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// PendingPageSize is the pending report's rows per page (UX §5.19).
const PendingPageSize = 25

// PendingFilter narrows the pending report (spec §13); a 0 id is every
// vendor, project, site or item. OverdueOnly is the `overdue=1` filter.
type PendingFilter struct {
	VendorID, ProjectID, SiteID, ItemID int64
	OverdueOnly                         bool
}

// PendingRow is one PO line with pending above 0 on an approved PO: spec
// §13's columns. Received includes already received; NetRate is the line's
// rate after discount and PendingValue is Pending × NetRate. A zero Date is
// blank. Overdue is the due date (ExpectedOn when set, else RequiredBy,
// §27.1) before today.
type PendingRow struct {
	LineID, POID                      int64
	PONumber                          string
	OrderDate, RequiredBy, ExpectedOn types.Date
	Vendor, Project, Site             string
	ItemCode, ItemName                string
	Ordered, Received, Pending        types.Qty
	UoM                               string
	NetRate                           types.Rate
	PendingValue                      types.Money
	Overdue                           bool
}

// PendingView is one page of the pending report and Total, the number of
// rows over every page.
type PendingView struct {
	Rows  []PendingRow
	Total int
}

// PendingReport is page page (from 1, PendingPageSize rows) of the pending
// report rows p may read that match f; a page below 1 is every row (the
// CSV). Rows read through Auth.Scope, so no filter widens access (AUTH-110);
// labels come from the PO's and line's snapshots and the master codes.
func (s *Service) PendingReport(ctx context.Context, p auth.Principal, f PendingFilter, page int) (PendingView, error) {
	pred, err := s.d.Auth.Scope(ctx, p, DocType, auth.RightRead)
	if err != nil {
		return PendingView{}, err
	}
	today := types.Today(s.d.Clock)
	var v PendingView
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		in, err := scopeIn(ctx, tx, "purchase_order", pred)
		if err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).PendingLines(ctx, dbq.PendingLinesParams{
			VendorID: f.VendorID, ProjectID: f.ProjectID, SiteID: f.SiteID, ItemID: f.ItemID,
		})
		if err != nil {
			return err
		}
		for _, r := range rows {
			if !in(r.PoID) {
				continue
			}
			row, err := pendingRowOf(r, today)
			if err != nil {
				return err
			}
			if f.OverdueOnly && !row.Overdue {
				continue
			}
			v.Rows = append(v.Rows, row)
		}
		return nil
	})
	if err != nil {
		return PendingView{}, err
	}
	v.Total = len(v.Rows)
	if page >= 1 {
		from := min((page-1)*PendingPageSize, v.Total)
		v.Rows = v.Rows[from:min(from+PendingPageSize, v.Total)]
	}
	return v, nil
}

// OverdueCount is the number of overdue pending lines p may read, for the
// Purchase home's Overdue deliveries card (spec §25, §27.1).
func (s *Service) OverdueCount(ctx context.Context, p auth.Principal) (int, error) {
	v, err := s.PendingReport(ctx, p, PendingFilter{OverdueOnly: true}, 0)
	return v.Total, err
}

// CanExport is AUTH's export check on Purchase Order (CD-145): nil when p
// may export, else AUTH's refusal. The pending CSV checks it before reading
// any row.
func (s *Service) CanExport(ctx context.Context, p auth.Principal) error {
	return s.d.Auth.Can(ctx, p, DocType, auth.RightExport, nil)
}

// pendingRowOf is r as a report row, Overdue judged against today.
func pendingRowOf(r dbq.PendingLinesRow, today types.Date) (PendingRow, error) {
	var dates [4]types.Date
	for i, iso := range []string{r.OrderDate, r.RequiredBy, r.ExpectedOn, r.Due} {
		if iso == "" {
			continue
		}
		d, err := types.DateFromISO(iso)
		if err != nil {
			return PendingRow{}, err
		}
		dates[i] = d
	}
	qty, already, received := types.Qty(r.Qty), types.Qty(r.AlreadyReceived), types.Qty(r.ReceivedQty)
	pending := Pending(qty, already, received)
	rate := types.Rate(r.Rate)
	value, err := pending.Times(rate)
	if err != nil {
		return PendingRow{}, err
	}
	due := dates[3]
	return PendingRow{
		LineID: r.LineID, POID: r.PoID, PONumber: r.Number,
		OrderDate: dates[0], RequiredBy: dates[1], ExpectedOn: dates[2],
		Vendor: r.SnapVendorName, Project: r.ProjectCode, Site: r.SiteCode,
		ItemCode: r.SnapItemCode, ItemName: r.SnapItemName,
		Ordered: qty, Received: already + received, Pending: pending, UoM: r.SnapUomCode,
		NetRate: rate, PendingValue: value,
		Overdue: due != (types.Date{}) && due.Compare(today) < 0,
	}, nil
}
