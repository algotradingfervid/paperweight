package modkit

import (
	"errors"
	"sync"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Wiring holds a module's dependencies per database. Module services wired by
// internal/app reach their dependencies from inside kernel hooks that only get
// a tx, and one binary may run many databases at once (CD-284).
type Wiring[T any] struct {
	mu   sync.Mutex
	byDB map[*db.DB]T
}

// Wire sets the value for d, replacing any earlier one.
func (w *Wiring[T]) Wire(d *db.DB, v T) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.byDB == nil {
		w.byDB = make(map[*db.DB]T)
	}
	w.byDB[d] = v
}

// Get returns the value wired for tx's database, or an error if none was.
func (w *Wiring[T]) Get(tx *db.Tx) (T, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	v, ok := w.byDB[tx.DB()]
	if !ok {
		var zero T
		return zero, errors.New("modkit: nothing wired for this database")
	}
	return v, nil
}
