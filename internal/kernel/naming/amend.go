package naming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/naming/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// AssignAmendment gives an amendment the number root-k inside the caller's
// db.Write transaction. The root and k are stored on the ledger row, never
// parsed out of a number; the counter is never touched.
func (n *Namer) AssignAmendment(ctx context.Context, tx *db.Tx, doctype string, docID int64, root string, k int) (string, error) {
	if tx == nil || !tx.Writable() {
		return "", ErrNeedsTx
	}
	q := tx.SQL()
	s, err := loadSeries(ctx, q, doctype)
	if err != nil {
		return "", err
	}
	dq := dbq.New(q)
	rootRow, err := dq.GetRootIssued(ctx, dbq.GetRootIssuedParams{DocType: doctype, Number: root})
	if errors.Is(err, sql.ErrNoRows) {
		return "", unknownRoot(root, s.Label)
	}
	if err != nil {
		return "", err
	}
	if k < 1 {
		return "", fmt.Errorf("naming: amendment number %d is outside 1..9", k)
	}
	if err := CheckAmendmentNo(root, k, s.Label); err != nil {
		return "", err
	}
	existing, err := amendmentOf(ctx, dq, root, k)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return "", &Error{Kind: ErrAlreadyAmended, Msg: fmt.Sprintf(msgAlreadyAmended, root, existing)}
	}
	if k > 1 {
		prev, err := amendmentOf(ctx, dq, root, k-1)
		if err != nil {
			return "", err
		}
		if prev == "" {
			return "", unknownRoot(root, s.Label)
		}
	}
	issued, err := dq.CountIssuedForDoc(ctx, dbq.CountIssuedForDocParams{DocType: doctype, DocID: docID})
	if err != nil {
		return "", err
	}
	if issued > 0 {
		return "", &Error{Kind: ErrNumberImmutable, Msg: msgImmutable}
	}
	num := amendmentNumber(root, k)
	if err := dq.InsertIssued(ctx, dbq.InsertIssuedParams{
		DocType:     doctype,
		DocID:       docID,
		Number:      num,
		Prefix:      rootRow.Prefix,
		Seq:         rootRow.Seq,
		Root:        root,
		AmendmentNo: int64(k),
		IssuedAt:    nowText(n.clk),
	}); err != nil {
		return "", err
	}
	return num, nil
}

// amendmentOf returns the number of root's k-th amendment, or "" when none
// has been issued.
func amendmentOf(ctx context.Context, dq *dbq.Queries, root string, k int) (string, error) {
	num, err := dq.GetAmendmentNumber(ctx, dbq.GetAmendmentNumberParams{Root: root, AmendmentNo: int64(k)})
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return num, err
}

func unknownRoot(root, label string) error {
	return &Error{Kind: ErrUnknownRoot, Msg: fmt.Sprintf(msgUnknownRoot, root, label)}
}
