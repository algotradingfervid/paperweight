package expense_test

import (
	"context"
	"database/sql"
	"regexp"
	"testing"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// column is one row of PRAGMA table_info.
type column struct {
	name, typ string
	notNull   bool
	dflt      sql.NullString
}

// foreignKey is one row of PRAGMA foreign_key_list.
type foreignKey struct{ table, from, to, onDelete string }

func query(t *testing.T, d *db.DB, fn func(q db.DBTX) error) {
	t.Helper()
	if err := d.Read(context.Background(), func(tx *db.Tx) error { return fn(tx.SQL()) }); err != nil {
		t.Fatalf("read schema: %v", err)
	}
}

func tableColumns(t *testing.T, d *db.DB, table string) map[string]column {
	t.Helper()
	cols := map[string]column{}
	query(t, d, func(q db.DBTX) error {
		rows, err := q.QueryContext(context.Background(), `SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?)`, table)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var c column
			if err := rows.Scan(&c.name, &c.typ, &c.notNull, &c.dflt); err != nil {
				return err
			}
			cols[c.name] = c
		}
		return rows.Err()
	})
	if len(cols) == 0 {
		t.Fatalf("table %s doesn't exist after migrations", table)
	}
	return cols
}

func foreignKeys(t *testing.T, d *db.DB, table string) map[string]foreignKey {
	t.Helper()
	fks := map[string]foreignKey{}
	query(t, d, func(q db.DBTX) error {
		rows, err := q.QueryContext(context.Background(), `SELECT "table", "from", "to", on_delete FROM pragma_foreign_key_list(?)`, table)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var fk foreignKey
			var to sql.NullString
			if err := rows.Scan(&fk.table, &fk.from, &to, &fk.onDelete); err != nil {
				return err
			}
			fk.to = to.String
			fks[fk.from] = fk
		}
		return rows.Err()
	})
	return fks
}

var autoincrementID = regexp.MustCompile(`(?is)\(\s*id\s+INTEGER\s+PRIMARY\s+KEY\s+AUTOINCREMENT\b`)

// CD-156: every expense id is INTEGER PRIMARY KEY AUTOINCREMENT.
func TestExp_SchemaAutoincrement(t *testing.T) {
	d := testkit.NewDB(t)
	for _, table := range []string{"expense_claim", "expense_claim_line", "employee_advance", "employee_advance_line"} {
		var ddl string
		query(t, d, func(q db.DBTX) error {
			return q.QueryRowContext(context.Background(),
				`SELECT COALESCE((SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?), '')`, table).Scan(&ddl)
		})
		if ddl == "" {
			t.Errorf("table %s doesn't exist after migrations", table)
			continue
		}
		if !autoincrementID.MatchString(ddl) {
			t.Errorf("%s: id is not INTEGER PRIMARY KEY AUTOINCREMENT (CD-156):\n%s", table, ddl)
		}
	}
}

// CD-161: line tables have parent_id ON DELETE CASCADE and line_no.
func TestExp_SchemaLineTables(t *testing.T) {
	d := testkit.NewDB(t)
	for _, tc := range []struct{ lines, header string }{
		{"expense_claim_line", "expense_claim"},
		{"employee_advance_line", "employee_advance"},
	} {
		cols := tableColumns(t, d, tc.lines)
		for _, name := range []string{"parent_id", "line_no"} {
			if c, ok := cols[name]; !ok || !c.notNull || c.typ != "INTEGER" {
				t.Errorf("%s.%s = %+v (found %v), want INTEGER NOT NULL", tc.lines, name, c, ok)
			}
		}
		fk, ok := foreignKeys(t, d, tc.lines)["parent_id"]
		if !ok || fk.table != tc.header || fk.onDelete != "CASCADE" {
			t.Errorf("%s.parent_id foreign key = %+v (found %v), want REFERENCES %s ON DELETE CASCADE", tc.lines, fk, ok, tc.header)
		}
	}
}

// Nullable less_advance (CD-264 (6)) and paid_on (CD-271 G1), used_amount
// NOT NULL DEFAULT 0, Billed to (CD-271 G4) and the snapshot columns of plan E1.
func TestExp_SchemaColumns(t *testing.T) {
	d := testkit.NewDB(t)
	claim := tableColumns(t, d, "expense_claim")
	claimLine := tableColumns(t, d, "expense_claim_line")
	advance := tableColumns(t, d, "employee_advance")

	nullable := func(table string, cols map[string]column, name string) {
		t.Helper()
		if c, ok := cols[name]; !ok || c.notNull {
			t.Errorf("%s.%s = %+v (found %v), want a nullable column", table, name, c, ok)
		}
	}
	nullable("expense_claim", claim, "less_advance")
	nullable("employee_advance", advance, "paid_on")
	nullable("expense_claim_line", claimLine, "billed_to_id")

	if c, ok := advance["used_amount"]; !ok || !c.notNull || !c.dflt.Valid || c.dflt.String != "0" {
		t.Errorf("employee_advance.used_amount = %+v (found %v), want NOT NULL DEFAULT 0", c, ok)
	}
	if fk, ok := foreignKeys(t, d, "expense_claim_line")["billed_to_id"]; !ok || fk.table != "branch" {
		t.Errorf("expense_claim_line.billed_to_id foreign key = %+v (found %v), want REFERENCES branch", fk, ok)
	}

	for table, names := range map[string][]string{
		"expense_claim":      {"snap_employee_name", "snap_employee_code"},
		"expense_claim_line": {"snap_type_code", "snap_type_name", "snap_project_code", "snap_site_code", "snap_billed_gstin"},
	} {
		cols := map[string]map[string]column{"expense_claim": claim, "expense_claim_line": claimLine}[table]
		for _, name := range names {
			if _, ok := cols[name]; !ok {
				t.Errorf("%s has no %s column", table, name)
			}
		}
	}
}

// Every declared field has its column at its level in the migrated schema.
func TestExp_SchemaMatchesRegistry(t *testing.T) {
	if err := doctype.CheckRegistry(); err != nil {
		t.Fatalf("doctype.CheckRegistry() = %v, want nil", err)
	}
	d := testkit.NewDB(t)
	for _, name := range expenseDocTypes {
		dt := lookupDT(t, name)
		header, lines := tableColumns(t, d, dt.Table), tableColumns(t, d, dt.LinesTable)
		for _, f := range dt.Fields {
			cols, level := header, dt.Table
			if f.OnLine {
				cols, level = lines, dt.LinesTable
			}
			if _, ok := cols[f.Column]; !ok {
				t.Errorf("%s field %s: no column %s on %s", name, f.Name, f.Column, level)
			}
		}
	}
}

// Both series render 14 characters in FY 2026-27 through the real naming
// service (spec §7, CD-113/115).
func TestExp_SchemaSeries(t *testing.T) {
	ctx := context.Background()
	d := testkit.NewDB(t)
	n := naming.New(d, testkit.Clock(), nil, audit.Default)
	day := types.Date{Year: 2026, Month: 10, Day: 24}
	for _, tc := range []struct{ name, want string }{
		{expense.ClaimDocType, "EX/26-27/00001"},
		{expense.AdvanceDocType, "EA/26-27/00001"},
	} {
		dt := lookupDT(t, tc.name)
		if err := n.ValidateTemplate(dt.Series); err != nil {
			t.Errorf("%s: ValidateTemplate(%q) = %v", tc.name, dt.Series, err)
			continue
		}
		if err := n.EnsureSeries(ctx, dt.Name, dt.Label, dt.Series); err != nil {
			t.Fatalf("%s: EnsureSeries(%q) = %v", tc.name, dt.Series, err)
		}
		got, err := n.Preview(ctx, dt.Name, day)
		if err != nil || got != tc.want || utf8.RuneCountInString(got) != 14 {
			t.Errorf("%s: Preview(2026-10-24) = %q, %v; want %q (14 characters)", tc.name, got, err, tc.want)
		}
	}
}

func fallbackRoles(t *testing.T, d *db.DB) (n int, name string) {
	t.Helper()
	query(t, d, func(q db.DBTX) error {
		return q.QueryRowContext(context.Background(),
			`SELECT COUNT(*), COALESCE(MAX(name), '') FROM roles WHERE name_key = lower(?)`, roleFallbackApprover).Scan(&n, &name)
	})
	return n, name
}

// Migration 10370 creates Fallback Approver once; an administrator's delete
// survives a later SeedDefaults (CD-158, CD-264 (5)).
func TestExp_FallbackApproverRole(t *testing.T) {
	ctx := context.Background()
	d := testkit.NewDB(t)
	if n, name := fallbackRoles(t, d); n != 1 || name != roleFallbackApprover {
		t.Fatalf("after migrations: %d roles named %q, want exactly one %q", n, name, roleFallbackApprover)
	}
	var isAdmin bool
	query(t, d, func(q db.DBTX) error {
		return q.QueryRowContext(context.Background(), `SELECT is_admin FROM roles WHERE name_key = lower(?)`, roleFallbackApprover).Scan(&isAdmin)
	})
	if isAdmin {
		t.Errorf("%s is_admin = true, want false", roleFallbackApprover)
	}

	fx := testkit.Seed(t, d)
	a := auth.New(auth.Deps{
		DB: d, Clock: testkit.Clock(), Directory: testkit.Directory(fx), Audit: audit.Default,
		Refs: &authtest.Refs{}, Info: authtest.Info{},
	})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("first SeedDefaults: %v", err)
	}
	if n, _ := fallbackRoles(t, d); n != 1 {
		t.Fatalf("after the first SeedDefaults: %d Fallback Approver roles, want 1", n)
	}
	for _, name := range expenseDocTypes {
		got, err := a.RoleRights(ctx, roleFallbackApprover, name)
		want := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll}
		if err != nil || len(got) != len(want) || got[auth.RightRead] != auth.ScopeAll || got[auth.RightApprove] != auth.ScopeAll {
			t.Errorf("RoleRights(%s, %s) = %v, %v; want %v", roleFallbackApprover, name, got, err, want)
		}
	}

	if err := a.DeleteRole(ctx, testkit.Principal(t, "admin@vardhan.test"), roleFallbackApprover); err != nil {
		t.Fatalf("admin deletes %s: %v", roleFallbackApprover, err)
	}
	if err := a.SeedDefaults(ctx, auth.SeedOptions{LinkedMasterRead: true}); err != nil {
		t.Fatalf("SeedDefaults after the delete: %v", err)
	}
	if n, _ := fallbackRoles(t, d); n != 0 {
		t.Errorf("after delete and SeedDefaults: %d Fallback Approver roles, want 0 (CD-158: a deleted role stays deleted)", n)
	}
}
