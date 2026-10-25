package doc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

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

// restrictedRequired returns the Required errors a draft save keeps (CD-178):
// an empty Required field in ProjectFields or SiteFields when p is restricted
// on that dimension (Auth.Restricted, CD-174), since the draft would hide from
// p (auth.recordAllowed). create calls it before Auth.Can, which would refuse
// such a draft as not found; save after the pipeline's Can on the stored
// record. Either way the save fails and nothing is written.
func (e *engine) restrictedRequired(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, n *normalized) ([]FieldError, error) {
	restricted := map[auth.Dimension]bool{}
	isRestricted := func(c string) (bool, error) {
		for d, cols := range map[auth.Dimension][]string{auth.DimProject: dt.ProjectFields, auth.DimSite: dt.SiteFields} {
			if !slices.Contains(cols, c) {
				continue
			}
			r, ok := restricted[d]
			if !ok {
				var err error
				if r, err = e.Auth.Restricted(ctx, tx, p, string(d)); err != nil {
					return false, err
				}
				restricted[d] = r
			}
			if r {
				return true, nil
			}
		}
		return false, nil
	}
	var errs []FieldError
	check := func(vals Values, unread map[string]bool, line int, onLine bool) error {
		for _, f := range dt.Fields {
			c := f.Column
			if onLine {
				c = "lines." + c
			}
			if !f.Required || !userField(f, onLine) || unread[f.Name] || !isEmpty(vals[f.Name]) {
				continue
			}
			r, err := isRestricted(c)
			if err != nil {
				return err
			}
			if r {
				errs = append(errs, fieldErr(f, line, msgRequired))
			}
		}
		return nil
	}
	if err := check(n.Header, n.unread, 0, false); err != nil {
		return nil, err
	}
	for i, l := range n.Lines {
		if err := check(l.Values, l.unread, i+1, true); err != nil {
			return nil, err
		}
	}
	return errs, nil
}

// isEmpty reports whether v is nil or blank text.
func isEmpty(v any) bool {
	s, isText := v.(string)
	return v == nil || (isText && strings.TrimSpace(s) == "")
}
