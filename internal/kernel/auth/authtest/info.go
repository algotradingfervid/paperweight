package authtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Info implements doctype.InfoSource over the probe tables.
type Info struct{}

// Info returns the record's info; a missing row or an unknown DocType wraps
// doctype.ErrNotFound (CD-54).
func (Info) Info(ctx context.Context, tx *db.Tx, name string, id int64) (doctype.Info, error) {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return doctype.Info{}, fmt.Errorf("authtest.Info: DocType %s: %w", name, doctype.ErrNotFound)
	}
	q := tx.SQL()
	out := doctype.Info{DocType: dt.Name, DocLabel: dt.Label, ID: id}
	var err error
	if dt.Master != nil {
		err = q.QueryRowContext(ctx, `SELECT code FROM `+dt.Table+` WHERE id = ?`, id).Scan(&out.Number) //nolint:gosec // G202: table is a probe constant
		out.Display = out.Number
	} else {
		var state string
		var createdBy int64
		err = q.QueryRowContext(ctx,
			`SELECT coalesce(number, ''), coalesce(state, ''), coalesce(created_by, 0), coalesce(employee_id, 0) FROM `+dt.Table+` WHERE id = ?`, id). //nolint:gosec // G202: table is a probe constant
			Scan(&out.Number, &state, &createdBy, &out.EmployeeID)
		out.State, out.CreatedBy = doctype.State(state), types.UserID(createdBy)
		out.Display = out.Number
		if out.Display == "" {
			out.Display = fmt.Sprintf("Draft %d", id)
		}
	}
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.Info{}, fmt.Errorf("authtest.Info: %s %d: %w", name, id, doctype.ErrNotFound)
	}
	if err != nil {
		return doctype.Info{}, err
	}
	if out.ProjectIDs, err = dimIDs(ctx, q, dt, dt.ProjectFields, id); err != nil {
		return doctype.Info{}, err
	}
	if out.SiteIDs, err = dimIDs(ctx, q, dt, dt.SiteFields, id); err != nil {
		return doctype.Info{}, err
	}
	return out, nil
}

// dimIDs returns the distinct non-null values of fields (header columns and
// "lines.<col>") for record id, sorted.
func dimIDs(ctx context.Context, q db.DBTX, dt *doctype.DocType, fields []string, id int64) ([]int64, error) {
	var out []int64
	for _, f := range fields {
		query := `SELECT ` + f + ` FROM ` + dt.Table + ` WHERE id = ? AND ` + f + ` IS NOT NULL`
		if col, ok := strings.CutPrefix(f, "lines."); ok {
			query = `SELECT ` + col + ` FROM ` + dt.LinesTable + ` WHERE parent_id = ? AND ` + col + ` IS NOT NULL`
		}
		rows, err := q.QueryContext(ctx, query, id) //nolint:gosec // G202: columns and tables are probe constants
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var v int64
			if err := rows.Scan(&v); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	slices.Sort(out)
	return out, nil
}
