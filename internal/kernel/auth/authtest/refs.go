package authtest

import (
	"context"
	"sync"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Refs implements auth.ReferenceChecker from rows added with Add.
type Refs struct {
	mu   sync.Mutex
	refs map[refKey]string
}

type refKey struct {
	table string
	id    int64
}

// Add records that row id of table is referenced by ref.
func (r *Refs) Add(table string, id int64, ref string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.refs == nil {
		r.refs = map[refKey]string{}
	}
	r.refs[refKey{table, id}] = ref
}

// IsReferenced reports the reference added for (table, id), if any.
func (r *Refs) IsReferenced(ctx context.Context, tx *db.Tx, table string, id int64) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ref, ok := r.refs[refKey{table, id}]
	return ref, ok, nil
}
