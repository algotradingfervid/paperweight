// Package filesfake is an in-memory files.Service for tests.
package filesfake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var _ files.Service = (*Fake)(nil)

type parentKey struct {
	doctype string
	id      int64
}

type stored struct {
	parent parentKey
	meta   files.Meta
	data   []byte
}

type Fake struct {
	mu      sync.Mutex
	clock   clock.Clock
	spooled map[*files.Spooled][]byte
	stored  []stored // upload order
	lastID  int64
}

func New(c clock.Clock) *Fake {
	return &Fake{clock: c, spooled: map[*files.Spooled][]byte{}}
}

func keyOf(r auth.RecordRef) parentKey { return parentKey{r.DocType, r.ID} }

// Spool returns a fresh opaque *files.Spooled holding b.
func (f *Fake) Spool(name string, b []byte) *files.Spooled {
	f.mu.Lock()
	defer f.mu.Unlock()
	sp := &files.Spooled{}
	f.spooled[sp] = slices.Clone(b)
	return sp
}

func (f *Fake) addLocked(parent parentKey, name string, data []byte, by auth.UserID) int64 {
	f.lastID++
	sum := sha256.Sum256(data)
	f.stored = append(f.stored, stored{
		parent: parent,
		data:   data,
		meta: files.Meta{
			ID: f.lastID, Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(sum[:]),
			UploadedBy: by, UploadedAt: f.clock.Now(),
		},
	})
	return f.lastID
}

func (f *Fake) Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, sp *files.Spooled) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.spooled[sp]
	if !ok {
		return 0, fmt.Errorf("filesfake: Attach of a file that Spool didn't create")
	}
	return f.addLocked(keyOf(parent), name, data, p.ID), nil
}

func (f *Fake) Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.stored, func(s stored) bool { return s.meta.ID == id })
	if i < 0 {
		return auth.ErrNotFound
	}
	f.stored = slices.Delete(f.stored, i, i+1)
	return nil
}

func (f *Fake) Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, files.Meta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.stored {
		if s.meta.ID == id {
			return io.NopCloser(bytes.NewReader(s.data)), s.meta, nil
		}
	}
	return nil, files.Meta{}, auth.ErrNotFound
}

// List returns parent's attachments in upload order.
func (f *Fake) List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]files.Meta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []files.Meta
	for _, s := range f.stored {
		if s.parent == keyOf(parent) {
			out = append(out, s.meta)
		}
	}
	return out, nil
}

// CopyAll copies from's attachments to to, uploaded by to.CreatedBy now (CD-29).
func (f *Fake) CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range slices.Clone(f.stored) {
		if s.parent == keyOf(from) {
			f.addLocked(keyOf(to), s.meta.Name, s.data, to.CreatedBy)
		}
	}
	return nil
}

func (f *Fake) DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stored = slices.DeleteFunc(f.stored, func(s stored) bool { return s.parent == keyOf(parent) })
	return nil
}
