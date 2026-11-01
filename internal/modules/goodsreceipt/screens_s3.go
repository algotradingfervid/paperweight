package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ReceiptPage is what the check page, the document page and the cancel
// page show of a GRN beyond its fields (UX doc §5.5, §5.6, §5.8). Whatever
// comes from its PO (G5's warnings, which name the PO and its pending, the
// as-ordered note, the PO's state) is left out when the viewer can't read
// the PO (CD-233, CD-248, CD-340): Warnings is empty, Notes has no
// as-ordered note and PO is nil.
type ReceiptPage struct {
	Warnings []string   // G5's Warnings
	Notes    []string   // G5's Notes
	EWay     string     // the e-way bill hint among Notes, or ""
	Site     string     // the GRN's snapshot of its site's name
	Project  string     // the GRN's snapshot of its project's code
	Invoice  string     // the stored bill details' invoice no., "" with none
	Challan  string     // G11's DuplicateChallan for the viewer, or ""
	PO       *ReceiptPO // nil when the viewer can't read the PO
}

// ReceiptPO is the GRN's PO as the document page's card shows it.
type ReceiptPO struct {
	ID             int64
	Number         string // naming.Label
	OrderDate      string // dd-mm-yyyy
	Closed         bool
	ReceiptStatus  string // purchase.ReceiptLabel
	Pending, Lines int    // lines with anything pending, of all lines
}

// Page is GRN id's ReceiptPage for p, in one read: a hidden or missing GRN
// is AUTH's 404 (CanTx read with the GRN's record, as BillCard; CD-245); the
// PO goes through auth.CheckLinkTx, never a master link check (CD-312).
func (s *Service) Page(ctx context.Context, p auth.Principal, id int64) (ReceiptPage, error) {
	var out ReceiptPage
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		g, err := q.G7Receipt(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, grnRef(id, g.CreatedBy, g.ProjectID, g.SiteID)); err != nil {
			return err
		}
		r, err := q.S3Receipt(ctx, id)
		if err != nil {
			return err
		}
		out.Site, out.Project = r.SiteName, r.ProjectCode
		po, err := readablePO(ctx, tx, s.d.Auth, p, q, r.PurchaseOrderID)
		if err != nil {
			return err
		}
		out.PO = po
		if out.Notes, err = Notes(ctx, tx, id); err != nil {
			return err
		}
		if po == nil {
			out.Notes = slices.DeleteFunc(out.Notes, func(n string) bool { return n == noteAsOrdered })
		} else if out.Warnings, err = Warnings(ctx, tx, id); err != nil {
			return err
		}
		for _, n := range out.Notes {
			if n != noteAsOrdered {
				out.EWay = n
			}
		}
		if out.Challan, err = DuplicateChallan(ctx, tx, p, id); err != nil { // read on id checked above
			return err
		}
		bill, _, err := billRow(ctx, q, id) // a zero BillView when there's no bill
		out.Invoice = bill.InvoiceNo
		return err
	})
	return out, err
}

// readablePO is PO poID's card for p, or nil when p can't read it.
func readablePO(ctx context.Context, tx *db.Tx, a auth.Service, p auth.Principal, q *dbq.Queries, poID int64) (*ReceiptPO, error) {
	switch err := a.CheckLinkTx(ctx, tx, p, purchase.DocType, poID); {
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
		return nil, nil
	case err != nil:
		return nil, err
	}
	o, err := q.S3Order(ctx, poID)
	if err != nil {
		return nil, err
	}
	lines, err := q.S3OrderLines(ctx, poID)
	if err != nil {
		return nil, err
	}
	po := &ReceiptPO{
		ID: poID, Number: naming.Label(o.Number, poID), Closed: o.State == string(doctype.StateClosed),
		ReceiptStatus: purchase.ReceiptLabel(o.ReceiptStatus), Lines: len(lines),
	}
	on, err := types.DateFromISO(o.OrderDate) // Required on submit: a GRN's PO has one
	if err != nil {
		return nil, err
	}
	po.OrderDate = types.FormatDate(on)
	for _, l := range lines {
		if purchase.Pending(types.Qty(l.Qty), types.Qty(l.AlreadyReceived), types.Qty(l.ReceivedQty)) > 0 {
			po.Pending++
		}
	}
	return po, nil
}
