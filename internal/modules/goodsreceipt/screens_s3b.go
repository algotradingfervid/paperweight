package goodsreceipt

import (
	"context"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// BillCard is what the document page's Bill details card shows of a GRN
// for one viewer (UX doc §5.7). Bill is Bill's view: the stored row, the
// tax-invoice offer (the card shows it to a writer only) or the zero view.
type BillCard struct {
	State      doctype.State
	CanWrite   bool // the viewer holds write on the GRN
	Bill       BillView
	Hint       string   // the accepted value line: the Invoice total's hint
	ChangedBy  string   // the name of who saved the row last; "" with no row
	CopiedFrom string   // "Copied from {GR number}" while the row is the copy from the amended GRN and the viewer can read that GRN
	Warnings   []string // billWarnings
	Info       []string
	Today      types.Date
}

// BillCard is GRN id's BillCard for p, who must be able to read the GRN
// (CheckLinkTx's refusal otherwise), in one read.
func (s *Service) BillCard(ctx context.Context, p auth.Principal, id int64) (BillCard, error) {
	out := BillCard{Today: types.Today(s.d.Clock)}
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := s.d.Auth.CheckLinkTx(ctx, tx, p, DocType, id); err != nil {
			return err
		}
		q := dbq.New(tx.SQL())
		g, err := q.G7Receipt(ctx, id)
		if err != nil {
			return err
		}
		out.State, out.Hint = doctype.State(g.State), acceptedLine(g.AcceptedValue)
		switch err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightWrite, grnRef(id, g.CreatedBy, g.ProjectID, g.SiteID)); {
		case err == nil:
			out.CanWrite = true
		case !errors.Is(err, auth.ErrForbidden) && !errors.Is(err, auth.ErrNotFound):
			return err
		}
		if out.Bill, err = Bill(ctx, tx, id); err != nil {
			return err
		}
		if out.Bill.Version == 0 {
			return nil
		}
		if out.Warnings, out.Info, err = s.billWarnings(ctx, tx, q, p, id, g, out.Bill); err != nil {
			return err
		}
		u, _, err := s.d.Auth.User(ctx, tx, out.Bill.UpdatedBy)
		if err != nil {
			return err
		}
		out.ChangedBy = u.Name
		out.CopiedFrom, err = s.copiedFrom(ctx, tx, p, id, g)
		return err
	})
	return out, err
}

// billWarnings are the card's warnings and information lines for GRN id's
// stored bill (spec §3.3): the duplicate invoice, named only where p can
// read the other GRN (CD-240, CD-279 (3)), and the accepted value when the
// total is set and differs from it. The caller has checked p's read on id.
func (s *Service) billWarnings(ctx context.Context, tx *db.Tx, q *dbq.Queries, p auth.Principal, id int64, g dbq.G7ReceiptRow, bill BillView) (warnings, info []string, err error) {
	dup, err := s.duplicate(ctx, tx, q, p, id, g.VendorID, bill.InvoiceNo)
	if err != nil {
		return nil, nil, err
	}
	if dup != "" {
		warnings = append(warnings, dup)
	}
	if bill.InvoiceAmount.Valid && int64(bill.InvoiceAmount.V) != g.AcceptedValue {
		info = append(info, acceptedLine(g.AcceptedValue))
	}
	return warnings, info, nil
}

// copiedFrom is the copy's audit text when GRN id's last bill entry is the
// move from the GRN it amends and p can read that GRN, else "" (CD-279
// (11); CD-233: it names that GRN).
func (s *Service) copiedFrom(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, g dbq.G7ReceiptRow) (string, error) {
	entries, err := audit.ForDoc(ctx, tx, DocType, id)
	if err != nil {
		return "", err
	}
	var reason string // the copy's text while no save came after it
	for _, e := range entries {
		switch e.Action {
		case auditBillSaved:
			reason = ""
		case auditBillCopied:
			reason = e.Reason
		}
	}
	if reason == "" {
		return "", nil
	}
	switch err := s.d.Auth.CheckLinkTx(ctx, tx, p, DocType, g.AmendedFromID.Int64); {
	case errors.Is(err, auth.ErrForbidden), errors.Is(err, auth.ErrNotFound):
		return "", nil
	case err != nil:
		return "", err
	}
	return reason, nil
}

// acceptedLine is the accepted value line for accepted value v (spec §3.3).
func acceptedLine(v int64) string {
	return fmt.Sprintf(msgBillAccepted, types.FormatMoney(types.Money(v)))
}
