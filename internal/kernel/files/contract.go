// Package files stores attachments (spec §5, CD-29). FILE owns it.
package files

import (
	"context"
	"io"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

type Meta struct {
	ID         int64
	Name, MIME string
	Size       int64
	SHA256     string
	UploadedBy auth.UserID
	UploadedAt time.Time
}

// Spooled is a temp file on disk, hashed and type-checked outside the transaction.
type Spooled struct {
	path   string // under files/tmp/ until Attach moves it into place
	size   int64
	sha256 string // lower-case hex
	head   []byte // the first min(size, SniffBytes) bytes
}

type Service interface {
	Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (AttachmentID int64, err error)
	Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error
	Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, Meta, error)
	List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]Meta, error)
	CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error // copies uploaded by to.CreatedBy
	DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error
}
