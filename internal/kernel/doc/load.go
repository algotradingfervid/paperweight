package doc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Doc is the typed internal view of a stored document.
type Doc struct {
	DocType                        string
	ID                             int64
	Number                         string
	State                          State
	StateReason                    string
	Version                        int
	AmendedFromID, AmendmentRootID int64
	AmendmentNo                    int
	CreatedBy, UpdatedBy           types.UserID
	CreatedAt, UpdatedAt           time.Time
	Header                         Values
	Lines                          []Line
}

// Line is one stored document line.
type Line struct {
	ID     int64
	LineNo int
	Values Values
}

// LoadDoc reads the document id of doctype with its lines in line_no order.
// An unknown DocType, a non-document or a missing row is KindNotFound.
func LoadDoc(ctx context.Context, tx *db.Tx, doctype string, id int64) (Doc, error) {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return Doc{}, errGone()
	}
	return loadDoc(ctx, tx, dt, id)
}

func loadDoc(ctx context.Context, tx *db.Tx, dt *DocType, id int64) (Doc, error) {
	d, err := selectHeader(ctx, tx, dt, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Doc{}, errGone()
	}
	if err != nil {
		return Doc{}, fmt.Errorf("doc: load %s %d: %w", dt.Name, id, err)
	}
	if d.Lines, err = selectLines(ctx, tx, dt, id); err != nil {
		return Doc{}, err
	}
	return d, nil
}

// Load returns the document as form values (CD-43), in the caller's tx and
// with no rights check: every header field by Name, and one map per line with
// every line field plus "id".
func (e *engine) Load(ctx context.Context, tx *db.Tx, doctype string, id int64) (Record, error) {
	d, err := LoadDoc(ctx, tx, doctype, id)
	if err != nil {
		return Record{}, err
	}
	dt, _ := Lookup(doctype)
	info, err := infoOf(ctx, tx, dt, d)
	if err != nil {
		return Record{}, err
	}
	rec := Record{Info: info, Version: d.Version, Values: map[string]string{}, Lines: make([]map[string]string, 0, len(d.Lines))}
	for _, f := range dt.HeaderFields() {
		rec.Values[f.Name] = FormatPlain(f, d.Header[f.Name])
	}
	for _, l := range d.Lines {
		m := map[string]string{"id": strconv.FormatInt(l.ID, 10)}
		for _, f := range dt.LineFields() {
			m[f.Name] = FormatPlain(f, l.Values[f.Name])
		}
		rec.Lines = append(rec.Lines, m)
	}
	return rec, nil
}
