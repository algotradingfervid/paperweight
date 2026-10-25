// Package audit records document history (spec §5, CD-03, CD-20). DOC owns it.
package audit

import (
	"context"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

type Ref struct { // CD-69
	DocType        string
	DocID          int64
	Number, Action string
}

type Change struct {
	Field, Before, After string
	LineNo               int
	LineID               int64
	Op                   string // Op: set add remove
}

type Entry struct {
	DocType  string
	DocID    int64
	Action   string
	From, To string
	Reason   string
	Changes  []Change
	By       types.UserID
	At       time.Time
	CausedBy *Ref
}

type Writer interface {
	Append(ctx context.Context, tx *db.Tx, e Entry) error
}

// Default is the Writer behind Append: the audit_log Store.
var Default Writer = Store{}

// Append = Default.Append.
func Append(ctx context.Context, tx *db.Tx, e Entry) error { return Default.Append(ctx, tx, e) }
