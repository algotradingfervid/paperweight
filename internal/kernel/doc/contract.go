// Package doc is the document engine (spec §5). DOC owns it. It re-exports
// every doctype name as an alias.
package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Aliases for every doctype name (CD-02).
type (
	State        = doctype.State
	Action       = doctype.Action
	FieldError   = doctype.FieldError
	FieldType    = doctype.FieldType
	Field        = doctype.Field
	SortKey      = doctype.SortKey
	LinkSpec     = doctype.LinkSpec
	RollUp       = doctype.RollUp
	Hooks        = doctype.Hooks
	MasterInfo   = doctype.MasterInfo
	MasterLookup = doctype.MasterLookup
	DocType      = doctype.DocType
	Info         = doctype.Info
	HTTPError    = doctype.HTTPError
	InfoSource   = doctype.InfoSource
)

const (
	StateDraft     = doctype.StateDraft
	StatePending   = doctype.StatePending
	StateApproved  = doctype.StateApproved
	StateClosed    = doctype.StateClosed
	StateRejected  = doctype.StateRejected
	StateCancelled = doctype.StateCancelled

	ActionSave     = doctype.ActionSave
	ActionSubmit   = doctype.ActionSubmit
	ActionWithdraw = doctype.ActionWithdraw
	ActionApprove  = doctype.ActionApprove
	ActionReject   = doctype.ActionReject
	ActionRevise   = doctype.ActionRevise
	ActionClose    = doctype.ActionClose
	ActionCancel   = doctype.ActionCancel
	ActionAmend    = doctype.ActionAmend
	ActionDelete   = doctype.ActionDelete

	Text      = doctype.Text
	Int       = doctype.Int
	Check     = doctype.Check
	Select    = doctype.Select
	Link      = doctype.Link
	Money     = doctype.Money
	Qty       = doctype.Qty
	Rate      = doctype.Rate
	Percent   = doctype.Percent
	Factor    = doctype.Factor
	Date      = doctype.Date
	Timestamp = doctype.Timestamp
)

var ErrNotFound = doctype.ErrNotFound

func Register(dt DocType)                 { doctype.Register(dt) }
func Lookup(name string) (*DocType, bool) { return doctype.Lookup(name) }
func All() []*DocType                     { return doctype.All() }

type ActionInput struct {
	Reason  string
	Version int
	Values  map[string]string
	Lines   []map[string]string
}

type Result struct { // CD-68
	ID       int64
	State    State
	Number   string
	Version  int
	Warnings []string
}

type Reference struct { // CD-22
	DocType, Field string
	ID             int64
	Display        string
	State          State
}

type Record struct { // CD-43
	Info    doctype.Info
	Version int
	Values  map[string]string
	Lines   []map[string]string
}

type Engine interface {
	Do(ctx context.Context, p auth.Principal, doctype string, id int64, a Action, in ActionInput) (Result, error)
	IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (firstRef string, ok bool, err error)
	References(ctx context.Context, tx *db.Tx, table string, id int64, limit int) ([]Reference, error)
	Load(ctx context.Context, tx *db.Tx, doctype string, id int64) (Record, error) // CD-43
	doctype.InfoSource
}
