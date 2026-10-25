package files

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const msgStorage = "Couldn't save the file. Try again or contact your administrator."

// timeLayout is the fixed-width UTC text every timestamp is stored as (§4.3).
const timeLayout = "2006-01-02T15:04:05.000Z"

// Attach checks the spooled file against parent's policy, moves its bytes into
// place and inserts the attachment row, all inside tx.
func (s *Store) Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (int64, error) {
	c, err := Check(s.policy(parent.DocType), name, f.Size(), f.Head())
	if err != nil {
		return 0, err
	}
	now := s.cfg.Clock.Now().UTC()
	if err := s.placeBlob(f, now); err != nil {
		return 0, err
	}
	return dbq.New(tx.SQL()).InsertAttachment(ctx, dbq.InsertAttachmentParams{
		ParentDocType: parent.DocType,
		ParentID:      parent.ID,
		FileName:      c.Name,
		Size:          f.Size(),
		ContentType:   c.ContentType,
		Sha256:        f.SHA256(),
		UploadedBy:    int64(p.ID),
		UploadedAt:    now.Format(timeLayout),
	})
}

// placeBlob makes the blob for f exist at its BlobPath. An existing blob of the
// same size is kept and f's temp file removed; otherwise the temp file is
// synced and renamed into place, which is atomic, so two uploads of the same
// bytes at once both succeed. The blob's mtime is set to now (the GC grace clock).
func (s *Store) placeBlob(f *Spooled, now time.Time) error {
	final, err := BlobPath(s.cfg.DataDir, f.SHA256())
	if err != nil {
		return s.storageErr("blob path", f.path, err)
	}
	st, err := os.Stat(final)
	switch {
	case err == nil && st.Size() == f.Size():
		if err := os.Chtimes(final, now, now); err != nil {
			return s.storageErr("touch blob", final, err)
		}
		if err := f.Cleanup(); err != nil {
			return s.storageErr("remove temp", f.path, err)
		}
		return nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return s.storageErr("stat blob", final, err)
	}
	if err := syncPath(f.path); err != nil {
		return s.storageErr("sync temp", f.path, err)
	}
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return s.storageErr("create blob dir", dir, err)
	}
	if err := os.Rename(f.path, final); err != nil {
		return s.storageErr("rename blob", final, err)
	}
	if err := syncPath(dir); err != nil {
		return s.storageErr("sync blob dir", dir, err)
	}
	if err := os.Chtimes(final, now, now); err != nil {
		return s.storageErr("touch blob", final, err)
	}
	return nil
}

// syncPath fsyncs the file or directory at path.
func syncPath(path string) error {
	f, err := os.Open(path) //nolint:gosec // path is a temp file or blob dir this package built
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// storageErr logs an OS failure and returns the one storage message users see.
func (s *Store) storageErr(op, path string, err error) error {
	s.cfg.Logger.Error("files: storage failure", "op", op, "path", path, "err", err)
	return &Error{Kind: KindStorage, Code: http.StatusInternalServerError, Message: msgStorage, Err: err}
}
