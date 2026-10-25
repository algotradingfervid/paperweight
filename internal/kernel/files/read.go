package files

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	MsgNotFound    = "File not found."
	msgNoRight     = "You don't have permission to %s %s. Ask your administrator for a role that allows it."
	msgBlobMissing = "The file is missing from storage. Ask your administrator to restore it from backup."
)

// Open checks p's read right on the attachment's parent and opens its blob;
// a parent whose policy has ReadAnySignedIn only has to exist and p be signed in.
// The reader is an *os.File. A missing blob returns KindBlobMissing with the Meta.
func (s *Store) Open(ctx context.Context, p auth.Principal, id int64) (io.ReadCloser, Meta, error) {
	var row dbq.Attachment
	err := s.cfg.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		row, err = dbq.New(tx.SQL()).GetAttachment(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return &Error{Kind: KindNotFound, Code: http.StatusNotFound, Message: MsgNotFound, Err: doctype.ErrNotFound}
		}
		if err != nil {
			return err
		}
		if s.policy(row.ParentDocType).ReadAnySignedIn {
			var info doctype.Info
			info, err = s.cfg.Info.Info(ctx, tx, row.ParentDocType, row.ParentID)
			if err != nil {
				return notFoundErr(err)
			}
			if p.ID == 0 {
				return &Error{Kind: KindForbidden, Code: http.StatusForbidden, Message: fmt.Sprintf(msgNoRight, "read", info.DocLabel), Err: auth.ErrForbidden}
			}
			return nil
		}
		_, err = s.canOnParent(ctx, tx, p, "read", row.ParentDocType, row.ParentID)
		return err
	})
	if err != nil {
		return nil, Meta{}, err
	}
	m, err := metaFrom(row)
	if err != nil {
		return nil, Meta{}, err
	}
	path, err := BlobPath(s.cfg.DataDir, row.Sha256)
	if err != nil {
		return nil, m, err
	}
	f, err := os.Open(path) //nolint:gosec // path is built by BlobPath from a checked hash
	if errors.Is(err, fs.ErrNotExist) {
		s.cfg.Logger.Error("attachment blob missing", "attachment_id", id, "sha256", row.Sha256)
		return nil, m, &Error{Kind: KindBlobMissing, Code: http.StatusNotFound, Message: msgBlobMissing, Err: err}
	}
	if err != nil {
		return nil, m, err
	}
	return f, m, nil
}

// List returns parent's attachments, newest first, when p may read parent.
func (s *Store) List(ctx context.Context, p auth.Principal, parent auth.RecordRef) ([]Meta, error) {
	var rows []dbq.Attachment
	err := s.cfg.DB.Read(ctx, func(tx *db.Tx) error {
		if _, err := s.canOnParent(ctx, tx, p, "read", parent.DocType, parent.ID); err != nil {
			return err
		}
		var err error
		rows, err = dbq.New(tx.SQL()).ListForParent(ctx, dbq.ListForParentParams{ParentDocType: parent.DocType, ParentID: parent.ID})
		return err
	})
	if err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(rows))
	for _, r := range rows {
		m, err := metaFrom(r)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// canOnParent loads the parent's Info and checks right r on it (built from
// Info). A missing or hidden parent is KindNotFound; no right is KindForbidden
// with AUTH's text naming Info.DocLabel (write is worded "change").
func (s *Store) canOnParent(ctx context.Context, tx *db.Tx, p auth.Principal, r auth.Right, dt string, id int64) (doctype.Info, error) {
	info, err := s.cfg.Info.Info(ctx, tx, dt, id)
	if err != nil {
		return doctype.Info{}, notFoundErr(err)
	}
	ref := refFrom(info)
	verb := string(r)
	if r == "write" {
		verb = "change"
	}
	if err := s.cfg.Auth.Can(ctx, p, dt, r, &ref); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return doctype.Info{}, &Error{Kind: KindForbidden, Code: http.StatusForbidden, Message: fmt.Sprintf(msgNoRight, verb, info.DocLabel), Err: err}
		}
		return doctype.Info{}, notFoundErr(err)
	}
	return info, nil
}

// notFoundErr maps doctype.ErrNotFound to KindNotFound and returns any other error as is.
func notFoundErr(err error) error {
	if errors.Is(err, doctype.ErrNotFound) {
		return &Error{Kind: KindNotFound, Code: http.StatusNotFound, Message: MsgNotFound, Err: err}
	}
	return err
}

func metaFrom(r dbq.Attachment) (Meta, error) {
	at, err := time.Parse(timeLayout, r.UploadedAt)
	if err != nil {
		return Meta{}, fmt.Errorf("files: attachment %d uploaded_at %q: %w", r.ID, r.UploadedAt, err)
	}
	return Meta{
		ID:         r.ID,
		Name:       r.FileName,
		MIME:       r.ContentType,
		Size:       r.Size,
		SHA256:     r.Sha256,
		UploadedBy: auth.UserID(r.UploadedBy),
		UploadedAt: at,
	}, nil
}
