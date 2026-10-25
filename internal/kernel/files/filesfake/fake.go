// Package filesfake is an in-memory files.Service for tests.
package filesfake

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
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
	auth    auth.Service       // nil: no parent, permission or file checks
	info    doctype.InfoSource // set with auth
	spooled map[*files.Spooled][]byte
	stored  []stored // upload order
	lastID  int64
}

// New returns a lenient double that checks nothing: any upload is attached to
// any parent. It is kept only for existing callers outside files; new tests
// use NewWith (CD-125).
func New(c clock.Clock) *Fake {
	return &Fake{clock: c, spooled: map[*files.Spooled][]byte{}}
}

// NewWith returns a fake that refuses what the real Store refuses under
// files.DefaultPolicy (CD-125, CD-182): parents come from info and rights from
// a, through the files package's own checks. Its gaps: DefaultPolicy only (no
// LogoPolicy, so no ReadAnySignedIn) and no RemoveGuards (no "receipt on line
// N" refusal).
func NewWith(c clock.Clock, a auth.Service, info doctype.InfoSource) *Fake {
	f := New(c)
	f.auth, f.info = a, info
	return f
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

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f *Fake) addLocked(parent parentKey, name, mime string, data []byte, by auth.UserID) int64 {
	f.lastID++
	f.stored = append(f.stored, stored{
		parent: parent,
		data:   data,
		meta: files.Meta{
			ID: f.lastID, Name: name, MIME: mime, Size: int64(len(data)), SHA256: sha(data),
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
	if f.auth == nil {
		return f.addLocked(keyOf(parent), name, "", data, p.ID), nil
	}
	pol := files.DefaultPolicy
	info, err := files.AttachParent(ctx, tx, f.auth, f.info, pol, p, parent)
	if err != nil {
		return 0, err
	}
	c, err := files.Check(pol, name, int64(len(data)), data)
	if err != nil {
		return 0, err
	}
	sum := sha(data)
	var dup bool
	var n int64
	for _, s := range f.stored {
		if s.parent == keyOf(parent) {
			n++
			dup = dup || s.meta.SHA256 == sum
		}
	}
	if err := files.AttachLimits(pol, info.Display, dup, n); err != nil {
		return 0, err
	}
	return f.addLocked(keyOf(parent), c.Name, c.ContentType, data, p.ID), nil
}

// canLocked checks right r on parent k like the real Store; nil without auth.
func (f *Fake) canLocked(ctx context.Context, tx *db.Tx, p auth.Principal, r auth.Right, k parentKey) (doctype.Info, error) {
	if f.auth == nil {
		return doctype.Info{}, nil
	}
	return files.CanOnParent(ctx, tx, f.auth, f.info, p, r, k.doctype, k.id)
}

func (f *Fake) Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := slices.IndexFunc(f.stored, func(s stored) bool { return s.meta.ID == id })
	if i < 0 {
		return files.NotFoundErr(doctype.ErrNotFound)
	}
	if f.auth != nil {
		info, err := f.canLocked(ctx, tx, p, "write", f.stored[i].parent)
		if err != nil {
			return err
		}
		if err := files.RemoveAllowed(info.State); err != nil {
			return err
		}
	}
	f.stored = slices.Delete(f.stored, i, i+1)
	return nil
}

func (f *Fake) Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, files.Meta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.stored {
		if s.meta.ID == id {
			if _, err := f.canLocked(ctx, nil, p, "read", s.parent); err != nil {
				return nil, files.Meta{}, err
			}
			return io.NopCloser(bytes.NewReader(s.data)), s.meta, nil
		}
	}
	return nil, files.Meta{}, files.NotFoundErr(doctype.ErrNotFound)
}

// List returns parent's attachments newest first, like the real Store.
func (f *Fake) List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]files.Meta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, err := f.canLocked(ctx, nil, p, "read", keyOf(parent)); err != nil {
		return nil, err
	}
	var out []files.Meta
	for _, s := range f.stored {
		if s.parent == keyOf(parent) {
			out = append(out, s.meta)
		}
	}
	slices.SortStableFunc(out, func(a, b files.Meta) int {
		if c := b.UploadedAt.Compare(a.UploadedAt); c != 0 {
			return c
		}
		return cmp.Compare(b.ID, a.ID)
	})
	return out, nil
}

// CopyAll copies from's attachments to to, uploaded by to.CreatedBy now (CD-29).
func (f *Fake) CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.info != nil {
		if _, err := f.info.Info(ctx, tx, from.DocType, from.ID); err != nil {
			return err
		}
	}
	for _, s := range slices.Clone(f.stored) {
		if s.parent == keyOf(from) {
			f.addLocked(keyOf(to), s.meta.Name, s.meta.MIME, s.data, to.CreatedBy)
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
