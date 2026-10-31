package goodsreceipt

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// BillInput is what the Bill details form posts, as typed. SaveBill parses
// each value after its load, right, version and state checks (CD-259), so
// a bad date never skips a 403 or a 409.
type BillInput struct {
	InvoiceNo, InvoiceDate, InvoiceAmount string
}

// BillView is a GRN's bill details (GRN spec §3.3): the stored row, or a
// zero view at version 0 when there is none. For an approved GRN with a tax
// invoice at the gate and no row, InvoiceNo and InvoiceDate are the
// challan's and Note says they were copied (CD-272 Gap 5); nothing is
// stored. UpdatedBy and UpdatedAt are the row's, for the card's "Last
// changed by" line.
type BillView struct {
	InvoiceNo     string
	InvoiceDate   types.Date
	InvoiceAmount sql.Null[types.Money]
	Version       int
	UpdatedBy     types.UserID
	UpdatedAt     time.Time
	Note          string
}

// Bill is GRN grnID's bill view, read in tx (plan G7 Do 1, 6).
func Bill(ctx context.Context, tx *db.Tx, grnID int64) (BillView, error) {
	q := dbq.New(tx.SQL())
	v, ok, err := billRow(ctx, q, grnID)
	if ok || err != nil {
		return v, err
	}
	g, err := q.G7Receipt(ctx, grnID)
	if err != nil {
		return BillView{}, err
	}
	if g.State != string(doctype.StateApproved) || g.ChallanKind != "Tax invoice" {
		return BillView{}, nil
	}
	// A tax invoice with no challan date offers no date.
	on, _ := types.DateFromISO(g.ChallanDate)
	return BillView{InvoiceNo: g.ChallanNo, InvoiceDate: on, Note: noteBillCopied}, nil
}

// billRow reads grnID's grn_bill row; ok is false when there is none.
func billRow(ctx context.Context, q *dbq.Queries, grnID int64) (BillView, bool, error) {
	r, err := q.G7Bill(ctx, grnID)
	if errors.Is(err, sql.ErrNoRows) {
		return BillView{}, false, nil
	}
	if err != nil {
		return BillView{}, false, err
	}
	on, err := types.DateFromISO(r.InvoiceDate)
	if err != nil {
		return BillView{}, false, err
	}
	at, err := types.ParseTimestamp(r.UpdatedAt)
	if err != nil {
		return BillView{}, false, err
	}
	return BillView{
		InvoiceNo: r.InvoiceNo, InvoiceDate: on,
		InvoiceAmount: sql.Null[types.Money]{V: types.Money(r.InvoiceAmount.Int64), Valid: r.InvoiceAmount.Valid},
		Version:       int(r.Version), UpdatedBy: types.UserID(r.UpdatedBy), UpdatedAt: at.Time(),
	}, true, nil
}

// SaveBill stores GRN grnID's bill details at the bill version the page
// posted (0: no row expected), inside one db.Write (CD-94), in plan G7 Do
// 2's order: load and write right (hidden or missing is AUTH's 404, CD-245),
// the version (DOC's stale text for Bill details, 409), the state (422),
// then the posted values, parsed only now (CD-259), every field error
// together. The invoice no. is checked, stored and compared trimmed. It
// inserts at version 1 or bumps the version, and audits the changed
// columns on the GRN (bill_saved). The warnings, read in the same transaction,
// are the duplicate invoice (naming the other GRN only when p can read it,
// CD-240, CD-248) and the accepted value line.
func (s *Service) SaveBill(ctx context.Context, p auth.Principal, grnID int64, version int, in BillInput) ([]string, error) {
	var warnings []string
	err := s.d.DB.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		g, err := q.G7Receipt(ctx, grnID)
		if errors.Is(err, sql.ErrNoRows) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		ref := grnRef(grnID, g.CreatedBy, g.ProjectID, g.SiteID)
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightWrite, ref); err != nil {
			return err
		}
		cur, _, err := billRow(ctx, q, grnID)
		if err != nil {
			return err
		}
		if version != cur.Version {
			return &doc.Error{Kind: doc.KindStale, Message: msgBillStale}
		}
		if g.State != string(doctype.StateApproved) {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgBillNotApproved}
		}
		in.InvoiceNo = strings.TrimSpace(in.InvoiceNo)
		on, amount, fields := s.parseBill(in)
		if len(fields) > 0 {
			return doc.Invalid(fields)
		}
		now := s.d.Clock.Now()
		at := types.TimestampOf(now).String()
		if cur.Version == 0 {
			err = q.G7BillInsert(ctx, dbq.G7BillInsertParams{GrnID: grnID, InvoiceNo: in.InvoiceNo, InvoiceDate: on.ISO(), InvoiceAmount: amount, At: at, By: int64(p.ID)})
		} else {
			err = q.G7BillUpdate(ctx, dbq.G7BillUpdateParams{InvoiceNo: in.InvoiceNo, InvoiceDate: on.ISO(), InvoiceAmount: amount, At: at, By: int64(p.ID), GrnID: grnID})
		}
		if err != nil {
			return err
		}
		if err := s.d.Audit.Append(ctx, tx, audit.Entry{
			DocType: DocType, DocID: grnID, Action: auditBillSaved, By: p.ID, At: now,
			Changes: billChanges(cur, in.InvoiceNo, on, amount),
		}); err != nil {
			return err
		}
		dup, err := s.duplicate(ctx, tx, q, p, grnID, g.VendorID, in.InvoiceNo)
		if err != nil {
			return err
		}
		if dup != "" {
			warnings = append(warnings, dup)
		}
		warnings = append(warnings, fmt.Sprintf(msgBillAccepted, types.FormatMoney(types.Money(g.AcceptedValue))))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return warnings, nil
}

// parseBill parses and checks the posted values (spec §3.3; CD-279 (4)):
// the invoice date and total, and every field error together.
func (s *Service) parseBill(in BillInput) (types.Date, sql.NullInt64, []doc.FieldError) {
	var fields []doc.FieldError
	switch {
	case in.InvoiceNo == "":
		fields = append(fields, doc.FieldError{Field: "invoice_no", Message: msgBillNoRequired})
	case utf8.RuneCountInString(in.InvoiceNo) > billNoMaxLen:
		fields = append(fields, doc.FieldError{Field: "invoice_no", Message: msgBillNoMaxLen})
	}
	var on types.Date
	if in.InvoiceDate == "" {
		fields = append(fields, doc.FieldError{Field: "invoice_date", Message: msgBillDateNeeded})
	} else if d, err := types.ParseDate(in.InvoiceDate); err != nil {
		fields = append(fields, doc.FieldError{Field: "invoice_date", Message: err.Error()})
	} else if d.Compare(types.Today(s.d.Clock)) > 0 {
		fields = append(fields, doc.FieldError{Field: "invoice_date", Message: msgBillDateFuture})
	} else {
		on = d
	}
	var amount sql.NullInt64
	if in.InvoiceAmount != "" {
		m, err := types.ParseMoney(in.InvoiceAmount)
		switch {
		case err != nil:
			fields = append(fields, doc.FieldError{Field: "invoice_amount", Message: err.Error()})
		case m < 0:
			fields = append(fields, doc.FieldError{Field: "invoice_amount", Message: msgBillNegative})
		default:
			amount = sql.NullInt64{Int64: int64(m), Valid: true}
		}
	}
	return on, amount, fields
}

// billChanges are the audit changes from cur (version 0: no row) to the new
// values, one per changed column, in plain form ("" for none).
func billChanges(cur BillView, no string, on types.Date, amount sql.NullInt64) []audit.Change {
	before := [3]string{cur.InvoiceNo, "", ""}
	if cur.Version > 0 {
		before[1] = cur.InvoiceDate.ISO()
	}
	if cur.InvoiceAmount.Valid {
		before[2] = strconv.FormatInt(int64(cur.InvoiceAmount.V), 10)
	}
	after := [3]string{no, on.ISO(), ""}
	if amount.Valid {
		after[2] = strconv.FormatInt(amount.Int64, 10)
	}
	var out []audit.Change
	for i, f := range [3]string{"invoice_no", "invoice_date", "invoice_amount"} {
		if before[i] != after[i] {
			out = append(out, audit.Change{Field: f, Before: before[i], After: after[i], Op: audit.OpSet})
		}
	}
	return out
}

// duplicate is the duplicate-invoice warning for invoice no on GRN grnID of
// vendor, or "" (spec §3.3): the other GRNs named when p can read each of
// them, else the "another receipt" sentence alone (CD-279 (3); UX §5.7).
func (s *Service) duplicate(ctx context.Context, tx *db.Tx, q *dbq.Queries, p auth.Principal, grnID, vendor int64, no string) (string, error) {
	rows, err := q.G7Duplicates(ctx, dbq.G7DuplicatesParams{InvoiceNo: no, VendorID: sql.NullInt64{Int64: vendor, Valid: true}, GrnID: grnID})
	if err != nil || len(rows) == 0 {
		return "", err
	}
	var names []string
	for _, r := range rows {
		err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, grnRef(r.ID, r.CreatedBy, r.ProjectID, r.SiteID))
		if errors.Is(err, auth.ErrNotFound) {
			return fmt.Sprintf(msgBillDupHidden, no), nil
		}
		if err != nil {
			return "", err
		}
		names = append(names, r.Number)
	}
	list := names[len(names)-1]
	if n := len(names); n > 1 {
		list = strings.Join(names[:n-1], ", ") + " and " + list
	}
	return fmt.Sprintf(msgBillDup, no, list), nil
}

// grnRef is GRN id as AUTH sees it (register.go's owner, project and site
// fields).
func grnRef(id, createdBy, projectID, siteID int64) *auth.RecordRef {
	return &auth.RecordRef{
		DocType: DocType, ID: id, CreatedBy: types.UserID(createdBy),
		ProjectIDs: []int64{projectID}, SiteIDs: []int64{siteID},
	}
}
