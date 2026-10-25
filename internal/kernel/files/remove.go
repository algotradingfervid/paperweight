package files

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgRemoveLocked = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
	msgInUse        = "This file is the receipt on line %d. Clear it there first."
)

// Remove deletes attachment id when p may write its parent and the parent is
// draft, rejected or stateless and no RemoveGuard reports a line using it,
// and audits the removal. The blob stays for GC.
func (s *Store) Remove(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) error {
	q := dbq.New(tx.SQL())
	row, err := q.GetAttachment(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return &Error{Kind: KindNotFound, Code: http.StatusNotFound, Message: MsgNotFound, Err: doctype.ErrNotFound}
	}
	if err != nil {
		return err
	}
	info, err := s.canOnParent(ctx, tx, p, "write", row.ParentDocType, row.ParentID)
	if err != nil {
		return err
	}
	switch info.State {
	case doctype.StateDraft, doctype.StateRejected, "":
	default:
		return &Error{Kind: KindRemoveLocked, Code: http.StatusUnprocessableEntity, Message: msgRemoveLocked}
	}
	if guard := s.cfg.RemoveGuards[row.ParentDocType]; guard != nil {
		line, err := guard(ctx, tx, row.ParentID, id)
		if err != nil {
			return err
		}
		if line > 0 {
			return &Error{Kind: KindInUse, Code: http.StatusUnprocessableEntity, Message: fmt.Sprintf(msgInUse, line)}
		}
	}
	if err := q.DeleteAttachment(ctx, id); err != nil {
		return err
	}
	return s.cfg.Audit.Append(ctx, tx, audit.Entry{
		DocType: row.ParentDocType,
		DocID:   row.ParentID,
		Action:  "attachment_removed",
		Changes: []audit.Change{
			{Field: "file_name", Before: row.FileName},
			{Field: "size", Before: strconv.FormatInt(row.Size, 10)},
			{Field: "sha256", Before: row.Sha256},
		},
		By: p.ID,
		At: s.cfg.Clock.Now().UTC(),
	})
}

// CopyAll copies every attachment row of from onto to, uploaded by
// to.CreatedBy now, sharing the blobs, and audits each copy. DOC has already
// checked amend, so there is no permission check.
func (s *Store) CopyAll(ctx context.Context, tx *db.Tx, from, to auth.RecordRef) error {
	info, err := s.cfg.Info.Info(ctx, tx, from.DocType, from.ID)
	if err != nil {
		return err
	}
	q := dbq.New(tx.SQL())
	rows, err := q.ListForParent(ctx, dbq.ListForParentParams{ParentDocType: from.DocType, ParentID: from.ID})
	if err != nil {
		return err
	}
	now := s.cfg.Clock.Now().UTC()
	slices.Reverse(rows) // ListForParent is newest first; copy oldest first to keep the order
	for _, r := range rows {
		c, err := q.CopyRow(ctx, dbq.CopyRowParams{
			ParentDocType: to.DocType,
			ParentID:      to.ID,
			UploadedBy:    int64(to.CreatedBy),
			UploadedAt:    now.Format(timeLayout),
			ID:            r.ID,
		})
		if err != nil {
			return err
		}
		err = s.cfg.Audit.Append(ctx, tx, audit.Entry{
			DocType: to.DocType,
			DocID:   to.ID,
			Action:  "attachment_added",
			Changes: []audit.Change{
				{Field: "file_name", After: c.FileName},
				{Field: "size", After: strconv.FormatInt(c.Size, 10)},
				{Field: "sha256", After: c.Sha256},
			},
			Reason: "copied from " + info.Display,
			By:     to.CreatedBy,
			At:     now,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// DeleteAll deletes parent's attachment rows without auditing (DOC audits the
// deleted draft). The blobs stay for GC.
func (s *Store) DeleteAll(ctx context.Context, tx *db.Tx, parent auth.RecordRef) error {
	return dbq.New(tx.SQL()).DeleteForParent(ctx, dbq.DeleteForParentParams{ParentDocType: parent.DocType, ParentID: parent.ID})
}
