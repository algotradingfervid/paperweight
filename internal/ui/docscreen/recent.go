package docscreen

import (
	"context"
	"errors"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// recentDocs is how many of the user's newest documents give recent picks
// (UI v2 §5.8).
const recentDocs = 20

// recentFor is the document form a Deps renders: its DocType and viewer.
// ctxOf sets it, so header Links on a document's steps offer recent picks.
type recentFor struct {
	dt *doctype.DocType
	p  auth.Principal
}

// withRecent is d for header fields of dt seen by p.
func (d Deps) withRecent(dt *doctype.DocType, p auth.Principal) Deps {
	d.recent = recentFor{dt: dt, p: p}
	return d
}

// recentPicks are the distinct values of header Link f on the viewer's own
// last 20 documents of the form's DocType, newest first, read through
// Auth.Scope for read, keeping the first components.MaxRecent masters that
// LinkRead lets the viewer read, that are enabled and that hold the field's
// LinkRole (CD-218), so no pick names a record they can't see (CD-233
// family) or one the save would refuse. It is nil off a document form, for a line field or a link to a
// document, without LinkRead, and when the viewer can't read the DocType.
// Scope has no Tx twin, so it is asked before the read opens.
func (d Deps) recentPicks(ctx context.Context, f doctype.Field) ([]components.PickRow, error) {
	dt, p := d.recent.dt, d.recent.p
	if dt == nil || d.LinkRead == nil || d.DB == nil {
		return nil, nil
	}
	var hf doctype.Field // f as registered on the header; zero for a line field
	for _, h := range dt.HeaderFields() {
		if h.Name == f.Name {
			hf = h
		}
	}
	target, ok := doctype.Lookup(hf.Link)
	if !ok || target.Master == nil || (hf.LinkRole != "" && d.LinkRole == nil) {
		return nil, nil
	}
	col := hf.Column
	pred, err := d.Auth.Scope(ctx, p, dt.Name, auth.RightRead)
	if errors.Is(err, auth.ErrForbidden) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	where := "created_by = ?"
	args := []any{int64(p.ID)}
	if pred.SQL != "" {
		where += " AND (" + pred.SQL + ")"
		args = append(args, pred.Args...)
	}
	args = append(args, recentDocs)
	// The table and column come from the registry, never from the request.
	query := `SELECT "` + col + `" FROM "` + dt.Table + `" WHERE ` + where + ` ORDER BY created_at DESC, id DESC LIMIT ?` //nolint:gosec // G202: registered identifiers and AUTH's predicate, with its args
	var out []components.PickRow
	err = d.DB.Read(ctx, func(tx *db.Tx) error {
		ids, err := recentIDs(ctx, tx, query, args)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if len(out) == components.MaxRecent {
				break
			}
			err := d.LinkRead(ctx, tx, p, target, id)
			if errors.Is(err, doctype.ErrNotFound) || errors.Is(err, auth.ErrForbidden) {
				continue
			}
			if err != nil {
				return err
			}
			if hf.LinkRole != "" {
				has, err := d.LinkRole(ctx, tx, target, id, hf.LinkRole)
				if err != nil {
					return err
				}
				if !has {
					continue
				}
			}
			mi, found, err := target.Master.Lookup(ctx, tx, id)
			if err != nil {
				return err
			}
			if found && !mi.Disabled {
				out = append(out, components.PickRow{ID: id, Code: mi.Code, Name: mi.Name})
			}
		}
		return nil
	})
	return out, err
}

// recentIDs runs query and returns its non-null ids, each once, in order.
func recentIDs(ctx context.Context, tx *db.Tx, query string, args []any) ([]int64, error) {
	rows, err := tx.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id *int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id != nil && !slices.Contains(ids, *id) {
			ids = append(ids, *id)
		}
	}
	return ids, rows.Err()
}
