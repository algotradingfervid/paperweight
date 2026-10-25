package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const msgStorage = "Couldn't save the file. Try again or contact your administrator."

// timeLayout is the fixed-width UTC text every timestamp is stored as (§4.3).
const timeLayout = "2006-01-02T15:04:05.000Z"

// Attach checks the parent, the user's write right, the parent's state, the
// file and the per-document count, then moves the bytes into place, inserts
// the attachment row and audits the add, all inside tx (CD-94). On a refusal
// it removes f's temp file.
func (s *Store) Attach(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (int64, error) {
	c, err := s.attachRules(ctx, tx, p, parent, name, f)
	if err != nil {
		_ = f.Cleanup()
		return 0, err
	}
	now := s.cfg.Clock.Now().UTC()
	if err := s.placeBlob(f, now); err != nil {
		return 0, err
	}
	id, err := dbq.New(tx.SQL()).InsertAttachment(ctx, dbq.InsertAttachmentParams{
		ParentDocType: parent.DocType,
		ParentID:      parent.ID,
		FileName:      c.Name,
		Size:          f.Size(),
		ContentType:   c.ContentType,
		Sha256:        f.SHA256(),
		UploadedBy:    int64(p.ID),
		UploadedAt:    now.Format(timeLayout),
	})
	if err != nil {
		return 0, err
	}
	err = s.cfg.Audit.Append(ctx, tx, audit.Entry{
		DocType: parent.DocType,
		DocID:   parent.ID,
		Action:  "attachment_added",
		Changes: []audit.Change{
			{Field: "file_name", After: c.Name},
			{Field: "size", After: strconv.FormatInt(f.Size(), 10)},
			{Field: "sha256", After: f.SHA256()},
		},
		By: p.ID,
		At: now,
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

// attachRules runs Attach's checks in order: parent exists, write right on it
// (built from its Info), not cancelled, Check, and the per-document count.
func (s *Store) attachRules(ctx context.Context, tx *db.Tx, p auth.Principal, parent auth.RecordRef, name string, f *Spooled) (Checked, error) {
	pol := s.policy(parent.DocType)
	info, err := s.cfg.Info.Info(ctx, tx, parent.DocType, parent.ID)
	if err != nil {
		return Checked{}, parentErr(err)
	}
	ref := refFrom(info)
	if err := s.cfg.Auth.Can(ctx, p, parent.DocType, "write", &ref); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return Checked{}, &Error{Kind: KindForbidden, Code: http.StatusForbidden, Message: fmt.Sprintf(msgForbidden, info.Display), Err: err}
		}
		return Checked{}, parentErr(err)
	}
	if !pol.IgnoreState && info.State == doctype.StateCancelled {
		return Checked{}, &Error{Kind: KindCancelled, Code: http.StatusUnprocessableEntity, Message: msgCancelled}
	}
	c, err := Check(pol, name, f.Size(), f.Head())
	if err != nil {
		return Checked{}, err
	}
	n, err := dbq.New(tx.SQL()).CountForParent(ctx, dbq.CountForParentParams{ParentDocType: parent.DocType, ParentID: parent.ID})
	if err != nil {
		return Checked{}, err
	}
	if n >= int64(pol.MaxCount) {
		return Checked{}, &Error{Kind: KindLimit, Code: http.StatusUnprocessableEntity, Message: msgLimit}
	}
	return c, nil
}

// parentErr maps doctype.ErrNotFound (which is auth.ErrNotFound, CD-54) to
// KindParentMissing and returns any other error as is.
func parentErr(err error) error {
	if errors.Is(err, doctype.ErrNotFound) {
		return &Error{Kind: KindParentMissing, Code: http.StatusNotFound, Message: msgParent, Err: err}
	}
	return err
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
