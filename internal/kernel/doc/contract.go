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
	// Only, when not nil, scopes a save of an existing draft (CD-205, CD-207):
	// field names, plus "lines". A header error refuses only on a named field;
	// a line error only on a line this save adds or changes, and on a named
	// field or with "lines" named. Other field errors don't stop the write and
	// return in Result.Problems. Parse, read-only and missing-link errors, any
	// error with no field name, and CD-178's restricted Required error refuse
	// even out of scope. An unknown name is a plain internal error. Create,
	// submit and approve ignore it. Only scopes errors, not values: callers
	// send values only for the names in Only.
	Only []string
}

type Result struct { // CD-68
	ID       int64
	State    State
	Number   string
	Version  int
	Warnings []string
	Problems []FieldError // errors outside ActionInput.Only (CD-205)
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
	// Problems returns the errors a draft save would find on stored draft id,
	// in DOC's text (CD-205, CD-207), and (nil, nil) for a document past draft.
	// It takes no principal and checks no rights: the caller has already
	// checked read. It calls no hook, because Hooks.Validate may write and
	// Problems runs in a read tx, so hook errors appear only at Submit.
	Problems(ctx context.Context, tx *db.Tx, doctype string, id int64) ([]FieldError, error)
	doctype.InfoSource
}
