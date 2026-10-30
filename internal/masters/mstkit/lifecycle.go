package mstkit

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Child is a table whose rows belong to a master row through FKColumn; their
// document references block the master's delete too, but the rows themselves
// don't: they go with the master.
type Child struct {
	Kind     Kind
	FKColumn string
}

// SetDisabledTx disables or enables (k, id) and audits "disable" or "enable".
// A page at a stale version gets DOC's stale error (CD-94). A row already in
// the requested state is left alone: no version bump, no audit entry. A
// missing row is doctype.ErrNotFound.
func (e Env) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64, version int, disabled bool) error {
	snap, err := snapshot(ctx, tx, k, id)
	if err != nil {
		return err
	}
	if err := CheckVersion(ctx, tx, k, id, version); err != nil {
		return err
	}
	was := snap["disabled"] == "true"
	if was == disabled {
		return nil
	}
	res, err := tx.SQL().ExecContext(ctx,
		"UPDATE "+k.Table+" SET disabled = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?", //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
		disabled, types.TimestampOf(e.Clock.Now()).String(), p.ID, id, version)
	if err := changedOne(res, err, k, title(k, snap)); err != nil {
		return err
	}
	action := "enable"
	if disabled {
		action = "disable"
	}
	return e.Audit(ctx, tx, p, k, id, action,
		map[string]string{"disabled": strconv.FormatBool(was)},
		map[string]string{"disabled": strconv.FormatBool(disabled)})
}

// DeleteTx deletes (k, id) unless a document references it or one of its
// children's rows, or another master links to it (CD-167); then it refuses
// with "Cannot delete {Label} {code}: it is used in {Display}. Disable it
// instead." A SQLite foreign-key failure gives the same refusal with "another
// record". A page at a stale version gets DOC's stale error, before any
// reference check (CD-94). A missing row is doctype.ErrNotFound.
func (e Env) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64, version int, children ...Child) error {
	snap, err := snapshot(ctx, tx, k, id)
	if err != nil {
		return err
	}
	if err := CheckVersion(ctx, tx, k, id, version); err != nil {
		return err
	}
	refuse := func(display string) error {
		return &DeleteRefusedError{Message: "Cannot delete " + k.Label + " " + title(k, snap) + ": it is used in " + display + ". Disable it instead."}
	}
	if ref, ok, err := e.firstReference(ctx, tx, k.Table, id); err != nil {
		return err
	} else if ok {
		return refuse(ref.Display)
	}
	for _, c := range children {
		ids, err := childIDs(ctx, tx, c, id)
		if err != nil {
			return err
		}
		for _, cid := range ids {
			if ref, ok, err := e.firstReference(ctx, tx, c.Kind.Table, cid); err != nil {
				return err
			} else if ok {
				return refuse(ref.Display)
			}
		}
	}
	if display, ok, err := masterReference(ctx, tx, k, id, children); err != nil {
		return err
	} else if ok {
		return refuse(display)
	}
	res, err := tx.SQL().ExecContext(ctx, "DELETE FROM "+k.Table+" WHERE id = ? AND version = ?", id, version) //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
	// Becomes db.IsForeignKey(err) (SQLITE_CONSTRAINT_FOREIGNKEY, 787)
	// once platform/db offers it (proposed to the director).
	if err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
		return refuse("another record")
	}
	if err := changedOne(res, err, k, title(k, snap)); err != nil {
		return err
	}
	return e.Audit(ctx, tx, p, k, id, "delete", snap, nil)
}

// UsedOutsideDrafts returns the first reference from a document that is not a
// draft. Master references (State "") don't count: they block a delete
// (MST-014) but not a lock.
func (e Env) UsedOutsideDrafts(ctx context.Context, tx *db.Tx, table string, id int64) (doc.Reference, bool, error) {
	// DOC's References applies limit as a SQL LIMIT per linking field, so this reads every reference to the row.
	refs, err := e.References(ctx, tx, table, id, math.MaxInt32)
	if err != nil {
		return doc.Reference{}, false, err
	}
	for _, r := range refs {
		if r.State != "" && r.State != doctype.StateDraft {
			return r, true, nil
		}
	}
	return doc.Reference{}, false, nil
}

func (e Env) firstReference(ctx context.Context, tx *db.Tx, table string, id int64) (doc.Reference, bool, error) {
	refs, err := e.References(ctx, tx, table, id, 1)
	if err != nil || len(refs) == 0 {
		return doc.Reference{}, false, err
	}
	return refs[0], true, nil
}

// masterReference finds the first registered master that links to (k, id),
// through a header field or a line field in its LinesTable, and returns it as
// "{Label} {Code}", or "{Label} {Name}" for a master with no code (CD-68).
// Masters of the children's kinds are skipped: their rows go with k.
func masterReference(ctx context.Context, tx *db.Tx, k Kind, id int64, children []Child) (string, bool, error) {
	for _, dt := range doctype.All() {
		if !dt.IsMaster() || slices.ContainsFunc(children, func(c Child) bool { return c.Kind.Table == dt.Table }) {
			continue
		}
		for _, f := range dt.Fields {
			if f.Type != doctype.Link || f.Link != k.DocType {
				continue
			}
			q := "SELECT id FROM " + dt.Table + " WHERE " + f.Column + " = ? ORDER BY id LIMIT 1"
			if f.OnLine {
				q = "SELECT parent_id FROM " + dt.LinesTable + " WHERE " + f.Column + " = ? ORDER BY parent_id LIMIT 1"
			}
			var other int64
			err := tx.SQL().QueryRowContext(ctx, q, id).Scan(&other)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return "", false, err
			}
			info, _, err := dt.Master.Lookup(ctx, tx, other)
			if err != nil {
				return "", false, err
			}
			return dt.Label + " " + cmp.Or(info.Code, info.Name), true, nil
		}
	}
	return "", false, nil
}

// changedOne turns a version-conditional write that changed no row into
// DOC's stale error (CD-94).
func changedOne(res sql.Result, err error, k Kind, title string) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return staleError(k, title)
	}
	return nil
}

// title is the row's code (code masters) or name, as messages show it.
func title(k Kind, snap map[string]string) string {
	if k.HasCode {
		return snap["code"]
	}
	return snap["name"]
}

// snapshot reads the row's code (code masters), name and disabled.
func snapshot(ctx context.Context, tx *db.Tx, k Kind, id int64) (map[string]string, error) {
	var code, name string
	var disabled bool
	codeCol := "''"
	if k.HasCode {
		codeCol = "code"
	}
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT "+codeCol+", name, disabled FROM "+k.Table+" WHERE id = ?", id).Scan(&code, &name, &disabled) //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
	if errors.Is(err, sql.ErrNoRows) {
		return nil, doctype.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	snap := map[string]string{"name": name, "disabled": strconv.FormatBool(disabled)}
	if k.HasCode {
		snap["code"] = code
	}
	return snap, nil
}

func childIDs(ctx context.Context, tx *db.Tx, c Child, parentID int64) ([]int64, error) {
	rows, err := tx.SQL().QueryContext(ctx,
		"SELECT id FROM "+c.Kind.Table+" WHERE "+c.FKColumn+" = ?", parentID) //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
