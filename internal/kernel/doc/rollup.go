package doc

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// actionEffect is the audit action of a roll-up change on a target document.
const actionEffect = "effect"

// recomputeRollUps recomputes, for each of dt's roll-ups, every target line
// that d's lines point at: the sum of SourceField over the approved and
// closed source lines (recompute, never increment; D-DOC-13). Each target
// document with a changed line gets one version bump and one "effect" audit
// row caused by d's action cause. The target's state is untouched.
func (e *engine) recomputeRollUps(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, number, cause string) error {
	type target struct {
		dt      *DocType
		changes []audit.Change
	}
	changed := map[int64]*target{} // by target header id
	for _, r := range dt.RollUps {
		t, ok := Lookup(r.TargetDocType)
		if !ok {
			return fmt.Errorf("doc: roll-up of %s targets unknown DocType %q", dt.Name, r.TargetDocType)
		}
		tf, _ := t.Field(r.TargetField)
		sf, _ := dt.Field(r.SourceField)
		jf, _ := dt.Field(r.JoinField)
		ids, err := joinIDs(ctx, tx, dt, quote(jf.Column), d.ID)
		if err != nil {
			return err
		}
		for _, id := range ids {
			var before, after int64
			var lineNo int
			var parent int64
			q := "SELECT COALESCE(" + quote(tf.Column) + ", 0), line_no, parent_id FROM " + quote(t.LinesTable) + " WHERE id = ?"
			if err := tx.SQL().QueryRowContext(ctx, q, id).Scan(&before, &lineNo, &parent); err != nil {
				return fmt.Errorf("doc: roll-up read %s %d: %w", t.LinesTable, id, err)
			}
			q = "SELECT COALESCE(SUM(sl." + quote(sf.Column) + "), 0) FROM " + quote(dt.LinesTable) + " sl JOIN " +
				quote(dt.Table) + " sh ON sh.id = sl.parent_id WHERE sl." + quote(jf.Column) + " = ? AND sh.state IN (?, ?)"
			if err := tx.SQL().QueryRowContext(ctx, q, id, string(StateApproved), string(StateClosed)).Scan(&after); err != nil {
				return fmt.Errorf("doc: roll-up sum %s: %w", dt.LinesTable, err)
			}
			if before == after {
				continue
			}
			q = "UPDATE " + quote(t.LinesTable) + " SET " + quote(tf.Column) + " = ? WHERE id = ?"
			if _, err := tx.SQL().ExecContext(ctx, q, after, id); err != nil {
				return fmt.Errorf("doc: roll-up update %s %d: %w", t.LinesTable, id, err)
			}
			if changed[parent] == nil {
				changed[parent] = &target{dt: t}
			}
			changed[parent].changes = append(changed[parent].changes, audit.Change{
				Field: r.TargetField, LineNo: lineNo, LineID: id,
				Before: strconv.FormatInt(before, 10), After: strconv.FormatInt(after, 10), Op: audit.OpSet,
			})
		}
	}
	parents := make([]int64, 0, len(changed))
	for id := range changed {
		parents = append(parents, id)
	}
	slices.Sort(parents)
	now := e.Clock.Now().UTC()
	for _, id := range parents {
		t := changed[id]
		q := "UPDATE " + quote(t.dt.Table) + " SET version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?"
		if _, err := tx.SQL().ExecContext(ctx, q, now.Format(timestampLayout), int64(p.ID), id); err != nil {
			return fmt.Errorf("doc: roll-up update %s %d: %w", t.dt.Table, id, err)
		}
		if err := e.Audit.Append(ctx, tx, audit.Entry{
			DocType: t.dt.Name, DocID: id, Action: actionEffect, Changes: t.changes, By: p.ID, At: now,
			CausedBy: &audit.Ref{DocType: dt.Name, DocID: d.ID, Number: number, Action: cause},
		}); err != nil {
			return err
		}
	}
	return nil
}

// joinIDs returns the distinct non-null values of the quoted line column col
// on parent's lines.
func joinIDs(ctx context.Context, tx *db.Tx, dt *DocType, col string, parent int64) ([]int64, error) {
	q := "SELECT DISTINCT " + col + " FROM " + quote(dt.LinesTable) + " WHERE parent_id = ? AND " + col + " IS NOT NULL ORDER BY " + col
	rows, err := tx.SQL().QueryContext(ctx, q, parent)
	if err != nil {
		return nil, fmt.Errorf("doc: roll-up read %s: %w", dt.LinesTable, err)
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("doc: roll-up read %s: %w", dt.LinesTable, err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
