package expense

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Receipts (spec §3.3): a line holds its receipt's SHA-256, and the file stays
// attached to the claim. Every read goes through the caller's tx (CD-213).

// checkReceipts refuses a line whose receipt isn't among the claim's
// attachments, with FILE-045's text on its receipt_sha256 (Validate). Two
// lines may hold the same hash.
func checkReceipts(ctx context.Context, tx *db.Tx, d Deps, id int64) ([]doctype.FieldError, error) {
	lines, err := dbq.New(tx.SQL()).E3ReceiptLines(ctx, id)
	if err != nil {
		return nil, err
	}
	var attached map[string]bool
	var errs []doctype.FieldError
	for i, l := range lines {
		if l.ReceiptSha256 == "" {
			continue
		}
		if attached == nil {
			ms, err := d.Files.ListTx(ctx, tx, auth.RecordRef{DocType: ClaimDocType, ID: id})
			if err != nil {
				return nil, err
			}
			attached = make(map[string]bool, len(ms))
			for _, m := range ms {
				attached[m.SHA256] = true
			}
		}
		if !attached[l.ReceiptSha256] {
			errs = append(errs, doctype.FieldError{Field: "receipt_sha256", Line: i + 1, Message: files.ReceiptMismatch(i+1, "expense").Message})
		}
	}
	return errs, nil
}

// submitReceipts refuses a line with neither a receipt nor a reason (submit).
func submitReceipts(ctx context.Context, tx *db.Tx, _ Deps, id int64) ([]doctype.FieldError, error) {
	lines, err := dbq.New(tx.SQL()).E3ReceiptLines(ctx, id)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	for i, l := range lines {
		if l.ReceiptSha256 == "" && l.NoReceiptReason == "" {
			errs = append(errs, doctype.FieldError{Field: "no_receipt_reason", Line: i + 1, Message: fmt.Sprintf(msgNoReceipt, i+1)})
		}
	}
	return errs, nil
}

// receiptLine is the 1-based first line of claim parentID holding the receipt
// of attachmentID, or 0 when no line does (RemoveGuard). It reads the
// attachment's hash with Store.Meta in the remove's tx.
func receiptLine(ctx context.Context, tx *db.Tx, parentID, attachmentID int64) (int, error) {
	d, err := wiring.Get(tx)
	if err != nil {
		return 0, err
	}
	m, err := d.Files.Meta(ctx, tx, attachmentID)
	if err != nil {
		return 0, err
	}
	lines, err := dbq.New(tx.SQL()).E3ReceiptLines(ctx, parentID)
	if err != nil {
		return 0, err
	}
	for i, l := range lines {
		if l.ReceiptSha256 == m.SHA256 {
			return i + 1, nil
		}
	}
	return 0, nil
}
