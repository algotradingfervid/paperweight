package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ForDoc returns the entries of one document, oldest first.
func ForDoc(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]Entry, error) {
	rows, err := dbq.New(tx.SQL()).ListByDoc(ctx, dbq.ListByDocParams{DocType: doctype, DocID: id})
	if err != nil {
		return nil, fmt.Errorf("audit: list %s %d: %w", doctype, id, err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, err := entryOf(r)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// Deleted returns every delete entry, newest first.
func Deleted(ctx context.Context, tx *db.Tx) ([]Entry, error) {
	rows, err := dbq.New(tx.SQL()).ListDeleted(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit: list deleted: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e, err := entryOf(dbq.ListByDocRow(r))
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// entryOf decodes one stored row.
func entryOf(r dbq.ListByDocRow) (Entry, error) {
	var stored []storedChange
	if err := json.Unmarshal([]byte(r.Changes), &stored); err != nil {
		return Entry{}, fmt.Errorf("audit: decode changes: %w", err)
	}
	var changes []Change
	if stored != nil {
		changes = make([]Change, len(stored))
		for i, c := range stored {
			changes[i] = Change(c)
		}
	}
	at, err := time.Parse(TimeLayout, r.At)
	if err != nil {
		return Entry{}, fmt.Errorf("audit: parse at: %w", err)
	}
	e := Entry{
		DocType: r.DocType,
		DocID:   r.DocID,
		Action:  r.Action,
		From:    r.FromState,
		To:      r.ToState,
		Reason:  r.Reason,
		Changes: changes,
		By:      types.UserID(r.UserID),
		At:      at,
	}
	if r.CauseDocType.Valid {
		e.CausedBy = &Ref{DocType: r.CauseDocType.String, DocID: r.CauseDocID.Int64, Number: r.CauseNumber.String, Action: r.CauseAction.String}
	}
	return e, nil
}

// Count returns the number of rows in audit_log.
func Count(ctx context.Context, tx *db.Tx) (int, error) {
	n, err := dbq.New(tx.SQL()).CountAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("audit: count: %w", err)
	}
	return int(n), nil
}
