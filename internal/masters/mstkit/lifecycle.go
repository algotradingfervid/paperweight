package mstkit

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// TODO(MST Task 1b, DOC-M1): CheckVersion(ctx, tx, k, id, version) returning
// DOC's stale-record error, called by SetDisabledTx and DeleteTx before they
// write (CD-94).

// Child is a table whose rows belong to a master row through FKColumn; their
// references block the master's delete too.
type Child struct {
	Kind     Kind
	FKColumn string
}

// SetDisabledTx disables or enables (k, id) and audits "disable" or "enable".
// A row already in the requested state is left alone: no version bump, no
// audit entry. A missing row is doctype.ErrNotFound. version is not checked
// yet (MST Task 1b, DOC-M1).
func (e Env) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64, version int, disabled bool) error {
	var was bool
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT disabled FROM "+k.Table+" WHERE id = ?", id).Scan(&was) //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.ErrNotFound
	}
	if err != nil {
		return err
	}
	if was == disabled {
		return nil
	}
	if _, err := tx.SQL().ExecContext(ctx,
		"UPDATE "+k.Table+" SET disabled = ?, version = version + 1, updated_at = ?, updated_by = ? WHERE id = ?", //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
		disabled, types.TimestampOf(e.Clock.Now()).String(), p.ID, id); err != nil {
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

// DeleteTx deletes (k, id) unless it, or one of its children's rows, is
// referenced; then it refuses with "Cannot delete {Label} {code}: it is used
// in {Display}. Disable it instead." A SQLite foreign-key failure gives the
// same refusal with "another record". A missing row is doctype.ErrNotFound.
// version is not checked yet (MST Task 1b, DOC-M1).
func (e Env) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64, version int, children ...Child) error {
	snap, err := snapshot(ctx, tx, k, id)
	if err != nil {
		return err
	}
	title := snap["name"]
	if k.HasCode {
		title = snap["code"]
	}
	refuse := func(display string) error {
		return &DeleteRefusedError{Message: "Cannot delete " + k.Label + " " + title + ": it is used in " + display + ". Disable it instead."}
	}
	// TODO(MST Task 1b, DOC-M1): CheckVersion here, before the reference
	// checks, so a stale page gets 409, not a refusal.
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
	if _, err := tx.SQL().ExecContext(ctx, "DELETE FROM "+k.Table+" WHERE id = ?", id); err != nil { //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
		// Becomes db.IsForeignKey(err) (SQLITE_CONSTRAINT_FOREIGNKEY, 787)
		// once platform/db offers it (proposed to the director).
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			return refuse("another record")
		}
		return err
	}
	return e.Audit(ctx, tx, p, k, id, "delete", snap, nil)
}

// UsedOutsideDrafts returns the first reference from a document that is not a
// draft. Master references (State "") don't count: they block a delete
// (MST-014) but not a lock.
func (e Env) UsedOutsideDrafts(ctx context.Context, tx *db.Tx, table string, id int64) (doc.Reference, bool, error) {
	// TODO(DOC-M1): confirm the real Engine.References handles a large limit without loading every row; else ask DOC for a first-non-draft query.
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
