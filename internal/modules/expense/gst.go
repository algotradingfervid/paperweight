package expense

import (
	"context"
	"fmt"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// GST capture on claim lines (spec §3.2, §5, §8; CD-271 G4, CD-281 (c)).

// BranchGSTINs is the number of distinct GSTINs on enabled branches. With one,
// Billed to is hidden and Validate sets it; with more it is required on a
// company bill at submit.
func BranchGSTINs(ctx context.Context, tx *db.Tx) (int, error) {
	n, err := dbq.New(tx.SQL()).E2BranchGSTINs(ctx)
	return int(n), err
}

// checkGST is the save-time GST rules of line l: GST not above the amount, a
// valid supplier GSTIN, a valid HSN/SAC, and a Billed to branch that has a
// GSTIN. Only a
// company-bill line holds Billed to: a draft save clears it on the others
// (setBilledTo) before this runs.
func checkGST(ctx context.Context, q *dbq.Queries, l dbq.E2ClaimLinesRow) ([]doctype.FieldError, error) {
	n := int(l.LineNo)
	var errs []doctype.FieldError
	if l.GstAmount.Valid && l.Amount.Valid && l.GstAmount.Int64 > l.Amount.Int64 {
		errs = append(errs, doctype.FieldError{Field: "gst_amount", Line: n, Message: fmt.Sprintf(msgGSTAboveAmount, n)})
	}
	if l.SupplierGstin != "" {
		if err := gstin.ValidateGSTIN(l.SupplierGstin); err != nil {
			errs = append(errs, doctype.FieldError{Field: "supplier_gstin", Line: n, Message: fmt.Sprintf(msgLinePrefix, n) + err.Error()})
		}
	}
	if fe := checkLineHSN(n, l.HsnSac); fe != nil {
		errs = append(errs, *fe)
	}
	if l.BilledToID.Valid {
		g, err := q.E2BranchGSTIN(ctx, l.BilledToID.Int64)
		if err != nil {
			return nil, err
		}
		if g == "" {
			errs = append(errs, doctype.FieldError{Field: "billed_to_id", Line: n, Message: fmt.Sprintf(msgBranchNoGSTIN, n)})
		}
	}
	return errs, nil
}

// checkLineHSN checks line n's HSN/SAC, when not blank, with the expense
// type master's rule for its default (CD-337 (5)): a code starting 99 is a
// SAC, else a goods HSN; gstin's text goes behind the line prefix.
func checkLineHSN(n int, code string) *doctype.FieldError {
	c := strings.TrimSpace(code)
	if c == "" {
		return nil
	}
	check := gstin.ValidateGoodsHSN
	if strings.HasPrefix(c, "99") {
		check = gstin.ValidateSAC
	}
	if _, err := check(c); err != nil {
		return &doctype.FieldError{Field: "hsn_sac", Line: n, Message: fmt.Sprintf(msgLinePrefix, n) + err.Error()}
	}
	return nil
}

// submitClaimFields is the claim's submit-only field rules: a company bill
// needs a supplier GSTIN, an invoice no. and, with more than one branch
// GSTIN, Billed to.
func submitClaimFields(ctx context.Context, tx *db.Tx, _ Deps, id int64) ([]doctype.FieldError, error) {
	lines, err := dbq.New(tx.SQL()).E2ClaimLines(ctx, id)
	if err != nil {
		return nil, err
	}
	branches, err := BranchGSTINs(ctx, tx)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	for _, l := range lines {
		if l.CompanyBill != 1 {
			continue
		}
		n := int(l.LineNo)
		if l.SupplierGstin == "" {
			errs = append(errs, doctype.FieldError{Field: "supplier_gstin", Line: n, Message: fmt.Sprintf(msgGSTINRequired, n)})
		}
		if l.InvoiceNo == "" {
			errs = append(errs, doctype.FieldError{Field: "invoice_no", Line: n, Message: fmt.Sprintf(msgInvoiceRequired, n)})
		}
		if branches > 1 && !l.BilledToID.Valid {
			errs = append(errs, doctype.FieldError{Field: "billed_to_id", Line: n, Message: fmt.Sprintf(msgBilledToRequired, n)})
		}
	}
	return errs, nil
}
