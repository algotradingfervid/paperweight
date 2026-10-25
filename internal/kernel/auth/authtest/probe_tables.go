package authtest

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CreateProbeTables creates the probe DocTypes' tables and fills probe_site
// and probe_project from fx (testkit.Seed's IDs; a site's name is its code,
// as in testkit).
func CreateProbeTables(t testing.TB, d *db.DB, fx testkit.Fixtures) {
	t.Helper()
	var stmts []string
	for _, dt := range docTypes() {
		if dt.Master != nil {
			stmts = append(stmts, `CREATE TABLE `+dt.Table+` (id INTEGER PRIMARY KEY, code TEXT NOT NULL UNIQUE, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, created_by INTEGER)`)
			continue
		}
		stmts = append(stmts,
			`CREATE TABLE `+dt.Table+` (id INTEGER PRIMARY KEY, number TEXT, state TEXT, created_by INTEGER, created_at TEXT, employee_id INTEGER, project_id INTEGER, site_id INTEGER, amount INTEGER NOT NULL DEFAULT 0, delivery_date TEXT)`,
			`CREATE TABLE `+dt.LinesTable+` (id INTEGER PRIMARY KEY, parent_id INTEGER, line_project_id INTEGER, line_site_id INTEGER)`)
	}
	ctx := context.Background()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		for _, s := range stmts {
			if _, err := q.ExecContext(ctx, s); err != nil {
				return err
			}
		}
		for _, s := range fx.Sites {
			if _, err := q.ExecContext(ctx, `INSERT INTO probe_site (id, code, name) VALUES (?, ?, ?)`, s.ID, s.Code, s.Code); err != nil {
				return err
			}
		}
		for _, p := range fx.Projects {
			if _, err := q.ExecContext(ctx, `INSERT INTO probe_project (id, code, name) VALUES (?, ?, ?)`, p.ID, p.Code, p.Name); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("authtest.CreateProbeTables: %v", err)
	}
}
