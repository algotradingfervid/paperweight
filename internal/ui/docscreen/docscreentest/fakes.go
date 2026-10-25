package docscreentest

import (
	"context"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// Reader is a docscreen.Reader that calls fn(nil): docfake ignores tx.
type Reader struct{}

// Read calls fn with a nil transaction.
func (Reader) Read(_ context.Context, fn func(tx *db.Tx) error) error { return fn(nil) }

// Lister is an in-memory docscreen.Lister that records its queries.
type Lister struct {
	mu      sync.Mutex
	results map[string]types.Result[docscreen.ListRow]
	err     error
	last    docscreen.ListQuery
	calls   int
}

// NewLister returns a Lister with no rows.
func NewLister() *Lister {
	return &Lister{results: map[string]types.Result[docscreen.ListRow]{}}
}

// Set fixes what List returns for name, whatever the query.
func (l *Lister) Set(name string, r types.Result[docscreen.ListRow]) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.results[name] = r
}

// Fail makes every List call return err.
func (l *Lister) Fail(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

// Last returns the query of the latest List call.
func (l *Lister) Last() docscreen.ListQuery {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.last
}

// Calls returns how many times List was called.
func (l *Lister) Calls() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls
}

// List returns the Set result for dt.
func (l *Lister) List(_ context.Context, _ auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls++
	l.last = q
	if l.err != nil {
		return types.Result[docscreen.ListRow]{}, l.err
	}
	return l.results[dt.Name], nil
}

type docKey struct {
	doctype string
	id      int64
}

// Extras is an in-memory docscreen.Extras; unset documents read as zero
// values.
type Extras struct {
	mu          sync.Mutex
	approvals   map[docKey]docscreen.ApprovalView
	origins     map[docKey]docscreen.Origin
	audits      map[docKey][]docscreen.HistoryRow
	attachments map[docKey][]components.Attachment
}

// NewExtras returns an empty Extras.
func NewExtras() *Extras {
	return &Extras{
		approvals:   map[docKey]docscreen.ApprovalView{},
		origins:     map[docKey]docscreen.Origin{},
		audits:      map[docKey][]docscreen.HistoryRow{},
		attachments: map[docKey][]components.Attachment{},
	}
}

// SetApproval fixes what Approval returns for the document.
func (e *Extras) SetApproval(name string, id int64, v docscreen.ApprovalView) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.approvals[docKey{name, id}] = v
}

// SetOrigin fixes what Origin returns for the document.
func (e *Extras) SetOrigin(name string, id int64, o docscreen.Origin) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.origins[docKey{name, id}] = o
}

// SetAudit fixes what Audit returns for the document.
func (e *Extras) SetAudit(name string, id int64, rows []docscreen.HistoryRow) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.audits[docKey{name, id}] = rows
}

// SetAttachments fixes what Attachments returns for the document.
func (e *Extras) SetAttachments(name string, id int64, files []components.Attachment) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.attachments[docKey{name, id}] = files
}

// Approval returns the SetApproval value.
func (e *Extras) Approval(_ context.Context, _ auth.Principal, name string, id int64) (docscreen.ApprovalView, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.approvals[docKey{name, id}], nil
}

// Origin returns the SetOrigin value.
func (e *Extras) Origin(_ context.Context, _ auth.Principal, name string, id int64) (docscreen.Origin, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.origins[docKey{name, id}], nil
}

// Audit returns the SetAudit value.
func (e *Extras) Audit(_ context.Context, _ auth.Principal, name string, id int64) ([]docscreen.HistoryRow, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.audits[docKey{name, id}], nil
}

// Attachments returns the SetAttachments value for rec.
func (e *Extras) Attachments(_ context.Context, _ auth.Principal, rec auth.RecordRef) ([]components.Attachment, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.attachments[docKey{rec.DocType, rec.ID}], nil
}
