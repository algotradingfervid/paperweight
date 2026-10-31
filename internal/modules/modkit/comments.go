package modkit

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit/dbq"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Comments on module documents (PO spec §22, CD-262): append-only, readable
// and addable by anyone who can read the record.

// MaxCommentRunes is the longest comment, in characters.
const MaxCommentRunes = 1000

const (
	recentWindow = 7 * 24 * time.Hour
	recentMax    = 10
)

// CommentsDeps are what Comments needs; Info resolves a record to the
// RecordRef its read check uses.
type CommentsDeps struct {
	DB    *db.DB
	Auth  auth.Service
	Info  doctype.InfoSource
	Clock clock.Clock
}

// Comments is the comment service.
type Comments struct{ d CommentsDeps }

// NewComments returns the comment service.
func NewComments(d CommentsDeps) *Comments { return &Comments{d: d} }

// Comment is one comment. Display is the document's display, filled only by
// RecentComments.
type Comment struct {
	ID            int64
	DocType       string
	RecordID      int64
	Body          string
	CreatedBy     types.UserID
	CreatedByName string
	CreatedAt     time.Time
	Display       string
}

var (
	commentsMu  sync.RWMutex
	commentable = map[string]bool{}
)

// RegisterComments opts a DocType in to comments; any other DocType answers
// auth.ErrNotFound.
func RegisterComments(dt string) {
	commentsMu.Lock()
	defer commentsMu.Unlock()
	commentable[dt] = true
}

func registered(dt string) bool {
	commentsMu.RLock()
	defer commentsMu.RUnlock()
	return commentable[dt]
}

// commentError is a refused body: 422 with its catalogue message.
type commentError string

func (e commentError) Error() string { return string(e) }
func (e commentError) Status() int   { return http.StatusUnprocessableEntity }

// readable resolves the record and checks read on it in tx. An unregistered
// DocType, a missing record and one p can't read all answer auth.ErrNotFound
// (CD-245).
func (c *Comments) readable(ctx context.Context, tx *db.Tx, p auth.Principal, dt string, id int64) (doctype.Info, error) {
	if !registered(dt) {
		return doctype.Info{}, auth.ErrNotFound
	}
	info, err := c.d.Info.Info(ctx, tx, dt, id)
	if err != nil {
		return info, notFound(err)
	}
	ref := auth.RecordRef{
		DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
		EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
	}
	return info, notFound(c.d.Auth.CanTx(ctx, tx, p, dt, auth.RightRead, &ref))
}

// notFound turns any not-found or forbidden error, whatever its text, into
// AUTH-108's one 404.
func notFound(err error) error {
	if errors.Is(err, auth.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
		return auth.ErrNotFound
	}
	return err
}

// AddComment saves body, trimmed, on the record as p.
func (c *Comments) AddComment(ctx context.Context, p auth.Principal, dt string, id int64, body string) (Comment, error) {
	var out Comment
	err := c.d.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := c.readable(ctx, tx, p, dt, id); err != nil {
			return err
		}
		var err error
		out, err = c.AddTx(ctx, tx, p.ID, dt, id, body)
		return err
	})
	return out, err
}

// AddTx saves body, trimmed, on the record as by, through tx and without a
// read check: for kernel effect hooks that have already checked rights (GRN
// approve and cancel, purchase). An unregistered DocType answers
// auth.ErrNotFound, as in AddComment.
func (c *Comments) AddTx(ctx context.Context, tx *db.Tx, by types.UserID, dt string, id int64, body string) (Comment, error) {
	if !registered(dt) {
		return Comment{}, auth.ErrNotFound
	}
	body = strings.TrimSpace(body)
	switch {
	case body == "":
		return Comment{}, commentError(msgCommentEmpty)
	case utf8.RuneCountInString(body) > MaxCommentRunes:
		return Comment{}, commentError(msgCommentTooLong)
	}
	u, found, err := c.d.Auth.User(ctx, tx, by)
	if err != nil {
		return Comment{}, err
	}
	if !found {
		return Comment{}, fmt.Errorf("modkit: comment author %d not found", by)
	}
	at := types.TimestampOf(c.d.Clock.Now())
	cid, err := dbq.New(tx.SQL()).InsertComment(ctx, dbq.InsertCommentParams{
		Doctype: dt, RecordID: id, Body: body, CreatedBy: int64(by), CreatedAt: at.String(),
	})
	if err != nil {
		return Comment{}, err
	}
	return Comment{
		ID: cid, DocType: dt, RecordID: id, Body: body,
		CreatedBy: by, CreatedByName: u.Name, CreatedAt: at.Time(),
	}, nil
}

// ListComments returns the record's comments, oldest first.
func (c *Comments) ListComments(ctx context.Context, p auth.Principal, dt string, id int64) ([]Comment, error) {
	var out []Comment
	err := c.d.DB.Read(ctx, func(tx *db.Tx) error {
		if _, err := c.readable(ctx, tx, p, dt, id); err != nil {
			return err
		}
		rows, err := dbq.New(tx.SQL()).ListComments(ctx, dbq.ListCommentsParams{Doctype: dt, RecordID: id})
		if err != nil {
			return err
		}
		out, err = c.fill(ctx, tx, rows)
		return err
	})
	return out, err
}

// RecentComments returns at most 10 comments from the last 7 days, newest
// first, on registered documents p created and can read, each with the
// document's Display (§22, §25, CD-262).
func (c *Comments) RecentComments(ctx context.Context, p auth.Principal) ([]Comment, error) {
	var out []Comment
	err := c.d.DB.Read(ctx, func(tx *db.Tx) error {
		since := types.TimestampOf(c.d.Clock.Now().Add(-recentWindow)).String()
		rows, err := dbq.New(tx.SQL()).CommentsSince(ctx, since)
		if err != nil {
			return err
		}
		// Each record is resolved and checked once: many comments share one.
		type record struct {
			dt string
			id int64
		}
		shown := map[record]*string{} // the record's Display, or nil when p doesn't own or can't read it
		var mine []dbq.ModuleComment
		var display []string
		for _, r := range rows {
			if len(mine) == recentMax {
				break
			}
			key := record{r.Doctype, r.RecordID}
			d, seen := shown[key]
			if !seen {
				info, err := c.readable(ctx, tx, p, r.Doctype, r.RecordID)
				if err != nil && !errors.Is(err, auth.ErrNotFound) {
					return err
				}
				if err == nil && info.CreatedBy == p.ID {
					d = &info.Display
				}
				shown[key] = d
			}
			if d == nil {
				continue
			}
			mine = append(mine, r)
			display = append(display, *d)
		}
		out, err = c.fill(ctx, tx, mine)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Display = display[i]
		}
		return nil
	})
	return out, err
}

// fill turns rows into Comments with their authors' names, read in tx.
func (c *Comments) fill(ctx context.Context, tx *db.Tx, rows []dbq.ModuleComment) ([]Comment, error) {
	names := map[types.UserID]string{}
	out := make([]Comment, 0, len(rows))
	for _, r := range rows {
		by := types.UserID(r.CreatedBy)
		name, ok := names[by]
		if !ok {
			u, _, err := c.d.Auth.User(ctx, tx, by)
			if err != nil {
				return nil, err
			}
			name = u.Name
			names[by] = name
		}
		at, err := types.ParseTimestamp(r.CreatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, Comment{
			ID: r.ID, DocType: r.Doctype, RecordID: r.RecordID, Body: r.Body,
			CreatedBy: by, CreatedByName: name, CreatedAt: at.Time(),
		})
	}
	return out, nil
}
