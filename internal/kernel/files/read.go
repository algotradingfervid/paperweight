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
	"strconv"
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
		return s.canRead(ctx, tx, p, row.ParentDocType, row.ParentID)
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

// Latest returns parent's newest attachment, read in the caller's tx, and
// false when it has none. The user comes from ctx (CD-12) and is checked as in
// Open: a ReadAnySignedIn parent needs only a signed-in user and takes no second
// connection (CD-209); any other parent goes through auth.Can, whose real
// service does. A ctx with no user is refused, not answered with "none".
func (s *Store) Latest(ctx context.Context, tx *db.Tx, parent auth.RecordRef) (Meta, bool, error) {
	p, _ := auth.PrincipalFrom(ctx)
	if err := s.canRead(ctx, tx, p, parent.DocType, parent.ID); err != nil {
		return Meta{}, false, err
	}
	rows, err := dbq.New(tx.SQL()).ListForParent(ctx, dbq.ListForParentParams{ParentDocType: parent.DocType, ParentID: parent.ID})
	if err != nil || len(rows) == 0 {
		return Meta{}, false, err
	}
	m, err := metaFrom(rows[0])
	return m, err == nil, err
}

// LatestURL is Latest as the /files/{id} URL that serves it, "" when parent
// has no attachment (CD-58, CD-209).
func (s *Store) LatestURL(ctx context.Context, tx *db.Tx, parent auth.RecordRef) (string, error) {
	m, ok, err := s.Latest(ctx, tx, parent)
	if err != nil || !ok {
		return "", err
	}
	return "/files/" + strconv.FormatInt(m.ID, 10), nil
}

// canRead lets p read attachments of dt's record id: a parent whose policy has
// ReadAnySignedIn only has to exist and p be signed in.
func (s *Store) canRead(ctx context.Context, tx *db.Tx, p auth.Principal, dt string, id int64) error {
	if !s.policy(dt).ReadAnySignedIn {
		_, err := s.canOnParent(ctx, tx, p, "read", dt, id)
		return err
	}
	info, err := s.cfg.Info.Info(ctx, tx, dt, id)
	if err != nil {
		return NotFoundErr(err)
	}
	if p.ID == 0 {
		return &Error{Kind: KindForbidden, Code: http.StatusForbidden, Message: fmt.Sprintf(msgNoRight, "read", info.DocLabel), Err: auth.ErrForbidden}
	}
	return nil
}

// canOnParent is CanOnParent with the Store's own auth and InfoSource.
func (s *Store) canOnParent(ctx context.Context, tx *db.Tx, p auth.Principal, r auth.Right, dt string, id int64) (doctype.Info, error) {
	return CanOnParent(ctx, tx, s.cfg.Auth, s.cfg.Info, p, r, dt, id)
}

// CanOnParent loads the parent's Info from src and checks right r on it with
// a (built from Info). A missing or hidden parent is KindNotFound; no right is
// KindForbidden with AUTH's text naming Info.DocLabel (write is worded
// "change"). Exported so filesfake refuses what Store refuses (CD-125, CD-182).
func CanOnParent(ctx context.Context, tx *db.Tx, a auth.Service, src doctype.InfoSource, p auth.Principal, r auth.Right, dt string, id int64) (doctype.Info, error) {
	info, err := src.Info(ctx, tx, dt, id)
	if err != nil {
		return doctype.Info{}, NotFoundErr(err)
	}
	ref := refFrom(info)
	verb := string(r)
	if r == "write" {
		verb = "change"
	}
	if err := a.CanTx(ctx, tx, p, dt, r, &ref); err != nil {
		if errors.Is(err, auth.ErrForbidden) {
			return doctype.Info{}, &Error{Kind: KindForbidden, Code: http.StatusForbidden, Message: fmt.Sprintf(msgNoRight, verb, info.DocLabel), Err: err}
		}
		return doctype.Info{}, NotFoundErr(err)
	}
	return info, nil
}

// NotFoundErr maps doctype.ErrNotFound to KindNotFound and returns any other error as is.
func NotFoundErr(err error) error {
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
