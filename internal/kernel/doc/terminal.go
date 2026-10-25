package doc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Close, cancel, delete and amend (Task 10).

// Task 10 texts. msgAmended: the source's display name, the amendment's (CD-27).
const (
	msgCloseReason  = "Enter a reason for closing (at least 3 characters)."
	msgCancelReason = "Enter a reason for cancelling (at least 3 characters)."
	msgReasonLong   = "Keep the reason under 500 characters."
	msgAmended      = "%s has already been amended as %s. Open that document instead."
)

// reason trims in and checks its length in characters (runes): fewer than
// min gives short, more than max gives long.
func reason(in string, min, max int, short, long string) (string, *Error) {
	s := strings.TrimSpace(in)
	msg := ""
	switch n := utf8.RuneCountInString(s); {
	case n < min:
		msg = short
	case n > max:
		msg = long
	default:
		return s, nil
	}
	return "", &Error{Kind: KindInvalid, Message: msg, Fields: []FieldError{{Field: "reason", Message: msg}}}
}

// setState moves d to state s with reason why, bumping the version (guarded
// by d.Version). It returns d as it is after the update.
func (e *engine) setState(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, s State, why string) (Doc, error) {
	now := e.Clock.Now().UTC()
	ok, err := updateHeaderGuarded(ctx, tx, dt, d.ID, d.Version, []string{"state", "state_reason"}, []any{string(s), why}, now, p.ID)
	if err != nil {
		return Doc{}, err
	}
	if !ok {
		return Doc{}, stale(naming.Label(d.Number, d.ID))
	}
	d.State, d.StateReason, d.Version, d.UpdatedBy, d.UpdatedAt = s, why, d.Version+1, p.ID, now
	return d, nil
}

// transition appends the audit row of a state change from before to after.
func (e *engine) transition(ctx context.Context, tx *db.Tx, p auth.Principal, a Action, before, after Doc) error {
	return e.Audit.Append(ctx, tx, audit.Entry{
		DocType: before.DocType, DocID: before.ID, Action: string(a), From: string(before.State), To: string(after.State),
		Reason: after.StateReason, By: p.ID, At: after.UpdatedAt,
	})
}

func (e *engine) close(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, in ActionInput) (Result, error) {
	why, rerr := reason(in.Reason, 3, 500, msgCloseReason, msgReasonLong)
	if rerr != nil {
		return Result{}, rerr
	}
	after, err := e.setState(ctx, tx, p, dt, d, StateClosed, why)
	if err != nil {
		return Result{}, err
	}
	if err := e.transition(ctx, tx, p, ActionClose, d, after); err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID, State: after.State, Number: d.Number, Version: after.Version}, nil
}

func (e *engine) cancel(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, in ActionInput) (Result, error) {
	why, rerr := reason(in.Reason, 3, 500, msgCancelReason, msgReasonLong)
	if rerr != nil {
		return Result{}, rerr
	}
	if blocked := e.checkBlockers(ctx, tx, dt, d, naming.Label(d.Number, d.ID)); blocked != nil {
		return Result{}, blocked // D-DOC-05: before any write
	}
	after, err := e.setState(ctx, tx, p, dt, d, StateCancelled, why)
	if err != nil {
		return Result{}, err
	}
	if err := dt.Hooks.ReverseEffects(ctx, tx, d.ID); err != nil {
		return Result{}, err
	}
	if err := e.recomputeRollUps(ctx, tx, p, dt, after, d.Number, string(ActionCancel)); err != nil {
		return Result{}, err
	}
	if err := e.transition(ctx, tx, p, ActionCancel, d, after); err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID, State: after.State, Number: d.Number, Version: after.Version}, nil
}

func (e *engine) delete(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, _ ActionInput) (Result, error) {
	snap, err := snapshot(dt, d)
	if err != nil {
		return Result{}, err
	}
	if err := e.Files.DeleteAll(ctx, tx, *recordRef(dt, d)); err != nil {
		return Result{}, err
	}
	q := "DELETE FROM " + quote(dt.Table) + " WHERE id = ? AND version = ?"
	res, err := tx.SQL().ExecContext(ctx, q, d.ID, d.Version)
	if err != nil {
		return Result{}, fmt.Errorf("doc: delete %s %d: %w", dt.Table, d.ID, err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Result{}, fmt.Errorf("doc: delete %s %d: %w", dt.Table, d.ID, err)
	} else if n != 1 {
		return Result{}, stale(naming.Label(d.Number, d.ID))
	}
	err = e.Audit.Append(ctx, tx, audit.Entry{
		DocType: dt.Name, DocID: d.ID, Action: string(ActionDelete), From: string(d.State),
		Changes: []audit.Change{{Field: "snapshot", Op: audit.OpSet, Before: snap}},
		By:      p.ID, At: e.Clock.Now().UTC(),
	})
	if err != nil {
		return Result{}, err
	}
	return Result{ID: d.ID}, nil
}

// snapshot is the JSON of d with every value in its stored form, so dates
// and timestamps keep their text.
func snapshot(dt *DocType, d Doc) (string, error) {
	stored := func(fields []Field, vals Values) Values {
		out := Values{}
		for _, f := range fields {
			out[f.Name] = toSQL(f, vals[f.Name])
		}
		return out
	}
	d.Header = stored(dt.HeaderFields(), d.Header)
	lines := make([]Line, len(d.Lines))
	for i, l := range d.Lines {
		l.Values = stored(dt.LineFields(), l.Values)
		lines[i] = l
	}
	d.Lines = lines
	b, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("doc: snapshot %s %d: %w", dt.Name, d.ID, err)
	}
	return string(b), nil
}

func (e *engine) amend(ctx context.Context, tx *db.Tx, p auth.Principal, dt *DocType, d Doc, _ ActionInput) (Result, error) {
	x := naming.Label(d.Number, d.ID)
	var prevID int64
	var prevNumber sql.NullString
	q := "SELECT id, number FROM " + quote(dt.Table) + " WHERE amended_from_id = ?"
	switch err := tx.SQL().QueryRowContext(ctx, q, d.ID).Scan(&prevID, &prevNumber); {
	case err == nil:
		return Result{}, Refused(fmt.Sprintf(msgAmended, x, naming.Label(prevNumber.String, prevID)))
	case !errors.Is(err, sql.ErrNoRows):
		return Result{}, fmt.Errorf("doc: find amendment of %s %d: %w", dt.Name, d.ID, err)
	}

	k := d.AmendmentNo + 1
	root, rootNumber := d.AmendmentRootID, d.Number
	if root == 0 {
		root = d.ID
	} else {
		q := "SELECT COALESCE(number, '') FROM " + quote(dt.Table) + " WHERE id = ?"
		if err := tx.SQL().QueryRowContext(ctx, q, root).Scan(&rootNumber); err != nil {
			return Result{}, fmt.Errorf("doc: read %s %d number: %w", dt.Name, root, err)
		}
	}
	if err := e.Numbers.CheckAmendment(rootNumber, k, dt.Label); err != nil {
		return Result{}, &Error{Kind: KindRefused, Message: err.Error(), Err: err}
	}

	now := e.Clock.Now().UTC()
	n := Doc{DocType: dt.Name, State: StateDraft, Version: 1, AmendedFromID: d.ID, AmendmentRootID: root,
		AmendmentNo: k, CreatedBy: p.ID, UpdatedBy: p.ID, CreatedAt: now, UpdatedAt: now, Header: d.Header}
	ts := now.Format(timestampLayout)
	fields := storedFields(dt, false)
	cols := append([]string{"state", "version", "state_reason", "amended_from_id", "amendment_root_id", "amendment_no",
		"created_at", "created_by", "updated_at", "updated_by"}, columns(fields)...)
	args := []any{string(StateDraft), 1, "", d.ID, root, k, ts, int64(p.ID), ts, int64(p.ID)}
	for _, f := range fields {
		args = append(args, toSQL(f, d.Header[f.Name]))
	}
	var err error
	if n.ID, err = insertRow(ctx, tx, dt.Table, cols, args); err != nil {
		return Result{}, err
	}
	for _, l := range d.Lines {
		id, err := insertAmendedLine(ctx, tx, dt, n.ID, l)
		if err != nil {
			return Result{}, err
		}
		n.Lines = append(n.Lines, Line{ID: id, LineNo: l.LineNo, Values: l.Values})
	}

	if err := e.Files.CopyAll(ctx, tx, *recordRef(dt, d), *recordRef(dt, n)); err != nil {
		return Result{}, err
	}
	links := normalized{links: map[linkKey]MasterInfo{}}
	if err := storedLinks(ctx, tx, dt, n, &links); err != nil {
		return Result{}, err
	}
	err = e.Audit.Append(ctx, tx, audit.Entry{
		DocType: dt.Name, DocID: n.ID, Action: "create", To: string(StateDraft), By: p.ID, At: now,
		Changes: append(createChanges(dt, n, links.links),
			audit.Change{Field: "amended_from", Op: audit.OpSet, Before: "null", After: strconv.FormatInt(d.ID, 10)}),
	})
	if err != nil {
		return Result{}, err
	}
	err = e.Audit.Append(ctx, tx, audit.Entry{
		DocType: dt.Name, DocID: d.ID, Action: "amended", By: p.ID, At: now,
		Changes: []audit.Change{{Field: "amended_to", Op: audit.OpSet, Before: "null", After: strconv.FormatInt(n.ID, 10)}},
	})
	if err != nil {
		return Result{}, err
	}
	return Result{ID: n.ID, State: StateDraft, Version: 1}, nil
}

// insertAmendedLine copies line l to parent with the same line_no, its user
// values and every roll-up set to zero (the copy has received nothing yet).
func insertAmendedLine(ctx context.Context, tx *db.Tx, dt *DocType, parent int64, l Line) (int64, error) {
	cols := []string{"parent_id", "line_no"}
	args := []any{parent, l.LineNo}
	for _, f := range dt.LineFields() {
		switch {
		case f.RollUp:
			cols, args = append(cols, quote(f.Column)), append(args, 0)
		case !f.System:
			cols, args = append(cols, quote(f.Column)), append(args, toSQL(f, l.Values[f.Name]))
		}
	}
	return insertRow(ctx, tx, dt.LinesTable, cols, args)
}
