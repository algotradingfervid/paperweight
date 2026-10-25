// Package auditfake is an in-memory audit.Writer for tests.
package auditfake

import (
	"context"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var _ audit.Writer = (*Recorder)(nil)

// Recorder keeps every appended entry in order (CD-86).
type Recorder struct {
	mu       sync.Mutex
	entries  []audit.Entry
	failNext error
}

func New() *Recorder { return &Recorder{} }

// Append records e, or returns and clears the error set with FailNext.
func (r *Recorder) Append(ctx context.Context, tx *db.Tx, e audit.Entry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.failNext; err != nil {
		r.failNext = nil
		return err
	}
	r.entries = append(r.entries, copyEntry(e))
	return nil
}

func copyEntry(e audit.Entry) audit.Entry {
	e.Changes = slices.Clone(e.Changes)
	if e.CausedBy != nil {
		ref := *e.CausedBy
		e.CausedBy = &ref
	}
	return e
}

func copyEntries(es []audit.Entry) []audit.Entry {
	var out []audit.Entry
	for _, e := range es {
		out = append(out, copyEntry(e))
	}
	return out
}

// Entries returns every recorded entry in order.
func (r *Recorder) Entries() []audit.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return copyEntries(r.entries)
}

// For returns the entries for one document, in order.
func (r *Recorder) For(doctype string, id int64) []audit.Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []audit.Entry
	for _, e := range r.entries {
		if e.DocType == doctype && e.DocID == id {
			out = append(out, copyEntry(e))
		}
	}
	return out
}

// FailNext makes the next Append return err and record nothing.
func (r *Recorder) FailNext(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failNext = err
}
