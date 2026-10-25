package doc

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Dynamic SQL for document tables. Table and column identifiers are checked
// at doctype.Register, and quoted here; every value is a ? argument.

func quote(ident string) string { return `"` + ident + `"` }

// columns returns the quoted Column of each field.
func columns(fields []Field) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = quote(f.Column)
	}
	return out
}

// storedFields are the fields a client writes: every user field at the level.
func storedFields(dt *DocType, onLine bool) []Field {
	var out []Field
	for _, f := range dt.Fields {
		if userField(f, onLine) {
			out = append(out, f)
		}
	}
	return out
}

// toSQL is the stored form of v: Text and Select are never NULL, Check is
// never NULL, dates and timestamps are TEXT.
func toSQL(f Field, v any) any {
	switch x := v.(type) {
	case nil:
		switch f.Type {
		case Text, Select:
			return ""
		case Check:
			return false
		}
		return nil
	case types.Date:
		return x.ISO()
	case time.Time:
		return x.UTC().Format(timestampLayout)
	}
	return v
}

// fromSQL is the typed value of a stored column (see Values); empty text is nil.
func fromSQL(f Field, src any) (any, error) {
	if src == nil {
		if f.Type == Check {
			return false, nil
		}
		return nil, nil
	}
	if b, ok := src.([]byte); ok {
		src = string(b)
	}
	switch f.Type {
	case Text, Select:
		if s, ok := src.(string); ok {
			if s == "" {
				return nil, nil
			}
			return s, nil
		}
	case Date:
		if s, ok := src.(string); ok {
			if s == "" {
				return nil, nil
			}
			return types.DateFromISO(s)
		}
	case Timestamp:
		if s, ok := src.(string); ok {
			if s == "" {
				return nil, nil
			}
			return time.Parse(timestampLayout, s)
		}
	default:
		n, ok := src.(int64)
		if !ok {
			break
		}
		switch f.Type {
		case Check:
			return n != 0, nil
		case Money:
			return types.Money(n), nil
		case Qty:
			return types.Qty(n), nil
		case Rate:
			return types.Rate(n), nil
		case Percent:
			return types.Percent(n), nil
		case Factor:
			return types.Factor(n), nil
		}
		return n, nil
	}
	return nil, fmt.Errorf("doc: column %q holds %T, not a %s value", f.Column, src, typeNames[f.Type])
}

// insertHeader inserts a new draft (version 1) with the user header values
// and returns its id.
func insertHeader(ctx context.Context, tx *db.Tx, dt *DocType, vals Values, at time.Time, by types.UserID) (int64, error) {
	fields := storedFields(dt, false)
	ts := at.UTC().Format(timestampLayout)
	cols := append([]string{"state", "version", "created_at", "created_by", "updated_at", "updated_by"}, columns(fields)...)
	args := []any{string(StateDraft), 1, ts, int64(by), ts, int64(by)}
	for _, f := range fields {
		args = append(args, toSQL(f, vals[f.Name]))
	}
	return insertRow(ctx, tx, dt.Table, cols, args)
}

// insertLine inserts one line of parent and returns its id.
func insertLine(ctx context.Context, tx *db.Tx, dt *DocType, parent int64, lineNo int, vals Values) (int64, error) {
	fields := storedFields(dt, true)
	cols := append([]string{"parent_id", "line_no"}, columns(fields)...)
	args := []any{parent, lineNo}
	for _, f := range fields {
		args = append(args, toSQL(f, vals[f.Name]))
	}
	return insertRow(ctx, tx, dt.LinesTable, cols, args)
}

func insertRow(ctx context.Context, tx *db.Tx, table string, cols []string, args []any) (int64, error) {
	q := "INSERT INTO " + quote(table) + " (" + strings.Join(cols, ", ") + ") VALUES (?" +
		strings.Repeat(", ?", len(cols)-1) + ") RETURNING id"
	var id int64
	if err := tx.SQL().QueryRowContext(ctx, q, args...).Scan(&id); err != nil {
		return 0, fmt.Errorf("doc: insert into %s: %w", table, err)
	}
	return id, nil
}

// updateHeaderGuarded sets cols (quoted column names) to args on the header
// id at version, and bumps the version, updated_at and updated_by. ok is
// false when no row matched: the document changed or is gone (stale).
func updateHeaderGuarded(ctx context.Context, tx *db.Tx, dt *DocType, id int64, version int, cols []string, args []any, at time.Time, by types.UserID) (ok bool, err error) {
	set := ""
	for _, c := range cols {
		set += c + " = ?, "
	}
	q := "UPDATE " + quote(dt.Table) + " SET " + set + "version = version + 1, updated_at = ?, updated_by = ? WHERE id = ? AND version = ?"
	args = append(append([]any{}, args...), at.UTC().Format(timestampLayout), int64(by), id, version)
	res, err := tx.SQL().ExecContext(ctx, q, args...)
	if err != nil {
		return false, fmt.Errorf("doc: update %s %d: %w", dt.Table, id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("doc: update %s %d: %w", dt.Table, id, err)
	}
	return n == 1, nil
}

// selectHeader reads the header row id; a missing row is sql.ErrNoRows.
func selectHeader(ctx context.Context, tx *db.Tx, dt *DocType, id int64) (Doc, error) {
	fields := dt.HeaderFields()
	q := "SELECT id, COALESCE(number, ''), state, state_reason, COALESCE(amended_from_id, 0), COALESCE(amendment_root_id, 0)," +
		" amendment_no, version, created_at, created_by, updated_at, updated_by"
	for _, c := range columns(fields) {
		q += ", " + c
	}
	q += " FROM " + quote(dt.Table) + " WHERE id = ?"
	d := Doc{DocType: dt.Name, Header: Values{}}
	var createdAt, updatedAt string
	raw := make([]any, len(fields))
	dest := []any{&d.ID, &d.Number, &d.State, &d.StateReason, &d.AmendedFromID, &d.AmendmentRootID,
		&d.AmendmentNo, &d.Version, &createdAt, &d.CreatedBy, &updatedAt, &d.UpdatedBy}
	for i := range raw {
		dest = append(dest, &raw[i])
	}
	if err := tx.SQL().QueryRowContext(ctx, q, id).Scan(dest...); err != nil {
		return Doc{}, err
	}
	var err error
	if d.CreatedAt, err = time.Parse(timestampLayout, createdAt); err != nil {
		return Doc{}, fmt.Errorf("doc: %s %d created_at: %w", dt.Name, id, err)
	}
	if d.UpdatedAt, err = time.Parse(timestampLayout, updatedAt); err != nil {
		return Doc{}, fmt.Errorf("doc: %s %d updated_at: %w", dt.Name, id, err)
	}
	for i, f := range fields {
		if d.Header[f.Name], err = fromSQL(f, raw[i]); err != nil {
			return Doc{}, err
		}
	}
	return d, nil
}

// selectLines reads the lines of parent in line_no order.
func selectLines(ctx context.Context, tx *db.Tx, dt *DocType, parent int64) ([]Line, error) {
	fields := dt.LineFields()
	q := "SELECT id, line_no"
	for _, c := range columns(fields) {
		q += ", " + c
	}
	q += " FROM " + quote(dt.LinesTable) + " WHERE parent_id = ? ORDER BY line_no, id"
	rows, err := tx.SQL().QueryContext(ctx, q, parent)
	if err != nil {
		return nil, fmt.Errorf("doc: read %s: %w", dt.LinesTable, err)
	}
	defer func() { _ = rows.Close() }()
	var lines []Line
	for rows.Next() {
		l := Line{Values: Values{}}
		raw := make([]any, len(fields))
		dest := []any{&l.ID, &l.LineNo}
		for i := range raw {
			dest = append(dest, &raw[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("doc: read %s: %w", dt.LinesTable, err)
		}
		for i, f := range fields {
			if l.Values[f.Name], err = fromSQL(f, raw[i]); err != nil {
				return nil, err
			}
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}
