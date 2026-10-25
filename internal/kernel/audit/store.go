package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/audit/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Change.Op values (CD-20).
const (
	OpSet    = "set"
	OpAdd    = "add"
	OpRemove = "remove"
)

// TimeLayout is the stored UTC timestamp layout, shared with doc.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// Store is the Writer over the append-only audit_log table.
type Store struct{}

var _ Writer = Store{}

// storedChange is one element of the changes JSON array; it mirrors Change
// field for field so the two convert directly.
type storedChange struct {
	Field  string `json:"f"`
	Before string `json:"b"`
	After  string `json:"a"`
	LineNo int    `json:"n"`
	LineID int64  `json:"l"`
	Op     string `json:"o"`
}

// Append writes e to audit_log inside tx. It refuses an incomplete entry.
func (Store) Append(ctx context.Context, tx *db.Tx, e Entry) error {
	if err := ValidateEntry(e); err != nil {
		return err
	}
	var changes []storedChange // nil Changes is stored as null and reads back nil
	if e.Changes != nil {
		changes = make([]storedChange, len(e.Changes))
		for i, c := range e.Changes {
			changes[i] = storedChange(c)
		}
	}
	js, err := json.Marshal(changes)
	if err != nil {
		return fmt.Errorf("audit: encode changes: %w", err)
	}
	p := dbq.InsertEntryParams{
		DocType:   e.DocType,
		DocID:     e.DocID,
		Action:    e.Action,
		FromState: e.From,
		ToState:   e.To,
		Reason:    e.Reason,
		Changes:   string(js),
		UserID:    int64(e.By),
		At:        e.At.UTC().Format(TimeLayout),
	}
	if r := e.CausedBy; r != nil {
		p.CauseDocType = sql.NullString{String: r.DocType, Valid: true}
		p.CauseDocID = sql.NullInt64{Int64: r.DocID, Valid: true}
		p.CauseNumber = sql.NullString{String: r.Number, Valid: true}
		p.CauseAction = sql.NullString{String: r.Action, Valid: true}
	}
	if err := dbq.New(tx.SQL()).InsertEntry(ctx, p); err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	return nil
}

// ValidateEntry reports why the store would refuse e, or nil (CD-163). The
// store and auditfake both call it.
func ValidateEntry(e Entry) error {
	switch {
	case e.DocType == "":
		return errors.New("audit: entry has no DocType")
	case e.Action == "":
		return errors.New("audit: entry has no Action")
	case e.At.IsZero():
		return errors.New("audit: entry has no At")
	}
	for i, c := range e.Changes {
		switch c.Op {
		case OpSet:
		case OpAdd, OpRemove:
			if c.LineID == 0 {
				return fmt.Errorf("audit: change %d: %s needs a LineID", i, c.Op)
			}
		default:
			return fmt.Errorf("audit: change %d: unknown Op %q", i, c.Op)
		}
	}
	return nil
}
