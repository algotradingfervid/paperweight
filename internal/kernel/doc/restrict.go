package doc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// checkRestrictions calls Auth.CheckValues and Auth.CheckLink for the proposed
// values on create and save (CD-70), before any write. AUTH's errors are
// returned unchanged.
func (e *engine) checkRestrictions(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, n *normalized) error {
	proposed := Doc{Header: n.Header}
	for _, l := range n.Lines {
		proposed.Lines = append(proposed.Lines, Line{ID: l.ID, Values: l.Values})
	}
	values := map[string][]int64{}
	for _, c := range slices.Concat(dt.ProjectFields, dt.SiteFields) {
		if ids := columnIDs(dt, proposed, []string{c}); len(ids) > 0 {
			values[c] = ids
		}
	}
	if len(values) > 0 {
		if err := e.Auth.CheckValues(ctx, p, dt.Name, values); err != nil {
			return err
		}
	}

	type target struct {
		DocType string
		ID      int64
	}
	checked := map[target]bool{}
	checkLink := func(t target) error {
		if checked[t] {
			return nil
		}
		checked[t] = true
		return e.Auth.CheckLink(ctx, p, t.DocType, t.ID)
	}
	levels := []Values{n.Header}
	for _, l := range n.Lines {
		levels = append(levels, l.Values)
	}
	for i, vals := range levels {
		onLine := i > 0
		for _, f := range dt.Fields {
			id, isID := vals[f.Name].(int64)
			if f.OnLine != onLine || f.Type != Link || !isID || hasLinkSpec(dt, f.Name) {
				continue
			}
			if t, ok := Lookup(f.Link); ok && t.IsDocument() {
				if err := checkLink(target{f.Link, id}); err != nil {
					return err
				}
			}
		}
		for _, ls := range dt.Links {
			id, isID := intOf(vals[ls.Field])
			if ls.OnLine != onLine || !isID {
				continue
			}
			if ls.OnLine {
				var found bool
				var err error
				if id, found, err = parentOf(ctx, tx, ls.Target, id); err != nil {
					return err
				} else if !found {
					continue // reported by checkSources
				}
			}
			if err := checkLink(target{ls.Target, id}); err != nil {
				return err
			}
		}
	}
	return nil
}

// hasLinkSpec reports whether field name is the Field of one of dt's LinkSpecs.
func hasLinkSpec(dt *DocType, name string) bool {
	return slices.ContainsFunc(dt.Links, func(ls LinkSpec) bool { return ls.Field == name })
}

// parentOf returns the parent_id of line id of the DocType target.
func parentOf(ctx context.Context, tx *db.Tx, target string, id int64) (int64, bool, error) {
	t, ok := Lookup(target)
	if !ok {
		return 0, false, nil
	}
	var parent int64
	err := tx.SQL().QueryRowContext(ctx, "SELECT parent_id FROM "+quote(t.LinesTable)+" WHERE id = ?", id).Scan(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("doc: read %s %d: %w", t.LinesTable, id, err)
	}
	return parent, true, nil
}
