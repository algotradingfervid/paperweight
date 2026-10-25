package msttest_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/platform/db"
	appmigrations "github.com/vardhan/internaltools/migrations"
)

// noIDTables are WP2 tables keyed by something other than an id; nothing
// refers to them by a generated id, so CD-156 doesn't apply.
var noIDTables = map[string]bool{
	"gst_state": true, // keyed by the GST state code
}

// noReuseBehaviourSkip are tables the behaviour half can't exercise; the
// schema half (AUTOINCREMENT) still applies to them.
var noReuseBehaviourSkip = map[string]bool{
	"company": true, // CHECK (id = 1): a singleton, its only possible id is 1
}

// TestMasters_IDsNeverReused has no case ID (CD-156, CD-166): every table the
// WP2 migrations (01000–01999) create and keep, if it has an id column, is
// INTEGER PRIMARY KEY AUTOINCREMENT, and deleting the newest row never hands
// its id to the next insert.
//
// The WP2 tables are found from migrated schemas, not by parsing SQL: the
// production migrations numbered below 01000 and below 02000 are applied to
// two scratch databases by goose itself, and the WP2 tables are the
// difference of their sqlite_master. Any way a migration creates (or later
// drops or renames) a table is covered, and a new WP2 migration is picked up
// without touching this test. The checks then run on msttest.New's database.
func TestMasters_IDsNeverReused(t *testing.T) {
	env := msttest.New(t)
	ctx := context.Background()

	before := migratedTables(t, 1000)
	upToWP2 := migratedTables(t, 2000)
	var wp2 []string
	for _, name := range upToWP2 {
		if !slices.Contains(before, name) {
			wp2 = append(wp2, name)
		}
	}
	for _, want := range []string{"gst_state", "company", "branch"} {
		if !slices.Contains(wp2, want) {
			t.Fatalf("WP2 tables = %v, want %s among them", wp2, want)
		}
	}

	for _, table := range wp2 {
		t.Run(table, func(t *testing.T) {
			hasID, err := hasIDColumn(ctx, env.DB, table)
			if err != nil {
				t.Fatalf("table_info(%s): %v", table, err)
			}
			if !hasID {
				if !noIDTables[table] {
					t.Fatalf("%s has no id column; key it by INTEGER PRIMARY KEY AUTOINCREMENT id, or list it in noIDTables with the reason", table)
				}
				return
			}
			if noIDTables[table] {
				t.Fatalf("%s has an id column but is listed in noIDTables", table)
			}
			if err := checkAutoincrement(ctx, env.DB, table); err != nil {
				t.Error(err)
			}
			if noReuseBehaviourSkip[table] {
				return
			}
			if err := checkNoReuse(ctx, env.DB, table); err != nil {
				t.Error(err)
			}
		})
	}

	// The checkers bite: a table with a plain INTEGER PRIMARY KEY fails both,
	// one with AUTOINCREMENT passes both.
	t.Run("checkers", func(t *testing.T) {
		if err := env.DB.Write(ctx, func(tx *db.Tx) error {
			_, err := tx.SQL().ExecContext(ctx, `
				CREATE TABLE ids_probe_plain (id INTEGER PRIMARY KEY, name TEXT NOT NULL) STRICT;
				CREATE TABLE ids_probe_auto (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL) STRICT;`)
			return err
		}); err != nil {
			t.Fatalf("create probe tables: %v", err)
		}
		if err := checkAutoincrement(ctx, env.DB, "ids_probe_plain"); err == nil {
			t.Error("checkAutoincrement(ids_probe_plain) = nil, want an error")
		}
		if err := checkNoReuse(ctx, env.DB, "ids_probe_plain"); err == nil {
			t.Error("checkNoReuse(ids_probe_plain) = nil, want an error (plain INTEGER PRIMARY KEY reuses the deleted id)")
		}
		if err := checkAutoincrement(ctx, env.DB, "ids_probe_auto"); err != nil {
			t.Errorf("checkAutoincrement(ids_probe_auto) = %v, want nil", err)
		}
		if err := checkNoReuse(ctx, env.DB, "ids_probe_auto"); err != nil {
			t.Errorf("checkNoReuse(ids_probe_auto) = %v, want nil", err)
		}
	})
}

// migratedTables applies the production migrations numbered below limit to a
// scratch database and returns its table names (without SQLite's and goose's).
func migratedTables(t *testing.T, limit int) []string {
	t.Helper()
	ctx := context.Background()
	sub := fstest.MapFS{}
	entries, err := fs.ReadDir(appmigrations.FS, ".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		if !ok || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := strconv.Atoi(num)
		if err != nil {
			t.Fatalf("migration %s: version: %v", e.Name(), err)
		}
		if v >= limit {
			continue
		}
		b, err := fs.ReadFile(appmigrations.FS, e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		sub[e.Name()] = &fstest.MapFile{Data: b}
	}
	d, err := db.Open(filepath.Join(t.TempDir(), "scratch-"+strconv.Itoa(limit)+".db"))
	if err != nil {
		t.Fatalf("open scratch db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := db.Migrate(ctx, d, sub, "goose_db_version"); err != nil {
		t.Fatalf("migrate below %d: %v", limit, err)
	}
	var names []string
	if err := d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'goose_db_version' ORDER BY name`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				return err
			}
			names = append(names, n)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("list tables below %d: %v", limit, err)
	}
	return names
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

type column struct {
	name, typ string
	notNull   bool
	hasDflt   bool
	pk        int
}

func tableColumns(ctx context.Context, q db.DBTX, table string) ([]column, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value IS NOT NULL, pk FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ, &c.notNull, &c.hasDflt, &c.pk); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s not in the schema", table)
	}
	return cols, nil
}

func hasIDColumn(ctx context.Context, d *db.DB, table string) (bool, error) {
	var has bool
	err := d.Read(ctx, func(tx *db.Tx) error {
		cols, err := tableColumns(ctx, tx.SQL(), table)
		if err != nil {
			return err
		}
		has = slices.ContainsFunc(cols, func(c column) bool { return c.name == "id" })
		return nil
	})
	return has, err
}

// checkAutoincrement is the schema half: the table's CREATE statement in
// sqlite_master says AUTOINCREMENT.
func checkAutoincrement(ctx context.Context, d *db.DB, table string) error {
	var ddl string
	if err := d.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl)
	}); err != nil {
		return fmt.Errorf("%s: read CREATE sql: %w", table, err)
	}
	if !strings.Contains(strings.ToUpper(ddl), "AUTOINCREMENT") {
		return fmt.Errorf("%s: id is not INTEGER PRIMARY KEY AUTOINCREMENT (CD-156); CREATE sql:\n%s", table, ddl)
	}
	return nil
}

// checkNoReuse is the behaviour half: insert a row (now the newest), delete
// it, insert again; the second id must differ from (and exceed) the first.
func checkNoReuse(ctx context.Context, d *db.DB, table string) error {
	seq := 0
	insert := func() (int64, error) {
		var id int64
		err := d.Write(ctx, func(tx *db.Tx) error {
			var err error
			id, err = insertGenericRow(ctx, tx.SQL(), table, &seq, 0)
			return err
		})
		return id, err
	}
	deleted, err := insert()
	if err != nil {
		return fmt.Errorf("%s: insert a minimal row: %w (extend insertGenericRow for this table's constraints)", table, err)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `DELETE FROM `+quoteIdent(table)+` WHERE id = ?`, deleted) //nolint:gosec // G202: table from sqlite_master, quoted
		return err
	}); err != nil {
		return fmt.Errorf("%s: delete id %d: %w", table, deleted, err)
	}
	next, err := insert()
	if err != nil {
		return fmt.Errorf("%s: insert after delete: %w", table, err)
	}
	if next <= deleted {
		return fmt.Errorf("%s: deleted id %d, next insert got id %d; ids must never be reused (CD-156)", table, deleted, next)
	}
	return nil
}

// insertGenericRow inserts a row giving a value only to NOT NULL columns
// without a default (other than id): a parent's key for a foreign key (a
// generic parent row is inserted when the parent is empty), otherwise a
// unique value of the column's type. It returns the new row's id.
func insertGenericRow(ctx context.Context, q db.DBTX, table string, seq *int, depth int) (int64, error) {
	if depth > 3 {
		return 0, errors.New("foreign keys nest too deep for a generic row")
	}
	cols, err := tableColumns(ctx, q, table)
	if err != nil {
		return 0, err
	}
	fks, err := foreignKeys(ctx, q, table)
	if err != nil {
		return 0, err
	}
	var names, marks []string
	var args []any
	for _, c := range cols {
		if c.name == "id" || !c.notNull || c.hasDflt {
			continue
		}
		var v any
		if fk, ok := fks[c.name]; ok {
			v, err = parentKey(ctx, q, fk, seq, depth)
			if err != nil {
				return 0, fmt.Errorf("column %s: parent %s: %w", c.name, fk.table, err)
			}
		} else {
			*seq++
			v = genericValue(c.typ, *seq)
		}
		names = append(names, quoteIdent(c.name))
		marks = append(marks, "?")
		args = append(args, v)
	}
	stmt := `INSERT INTO ` + quoteIdent(table) + ` DEFAULT VALUES`
	if len(names) > 0 {
		stmt = `INSERT INTO ` + quoteIdent(table) + ` (` + strings.Join(names, ", ") + `) VALUES (` + strings.Join(marks, ", ") + `)`
	}
	res, err := q.ExecContext(ctx, stmt, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func genericValue(typ string, n int) any {
	t := strings.ToUpper(typ)
	switch {
	case strings.Contains(t, "INT"):
		return int64(1)
	case strings.Contains(t, "REAL"), strings.Contains(t, "FLOA"), strings.Contains(t, "DOUB"):
		return 1.0
	case strings.Contains(t, "BLOB"):
		return []byte{byte(n)}
	default:
		return "X" + strconv.Itoa(n)
	}
}

type foreignKey struct{ table, to string }

func foreignKeys(ctx context.Context, q db.DBTX, table string) (map[string]foreignKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT "table", "from", coalesce("to", '') FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]foreignKey{}
	for rows.Next() {
		var from string
		var fk foreignKey
		if err := rows.Scan(&fk.table, &from, &fk.to); err != nil {
			return nil, err
		}
		out[from] = fk
	}
	return out, rows.Err()
}

// parentKey returns an existing key of the parent table, inserting a generic
// parent row when there is none.
func parentKey(ctx context.Context, q db.DBTX, fk foreignKey, seq *int, depth int) (any, error) {
	to := fk.to
	if to == "" {
		cols, err := tableColumns(ctx, q, fk.table)
		if err != nil {
			return nil, err
		}
		for _, c := range cols {
			if c.pk == 1 {
				to = c.name
			}
		}
		if to == "" {
			to = "rowid"
		}
	}
	sel := `SELECT ` + quoteIdent(to) + ` FROM ` + quoteIdent(fk.table) + ` ORDER BY rowid LIMIT 1` //nolint:gosec // G202: names from the schema, quoted
	var v any
	err := q.QueryRowContext(ctx, sel).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := insertGenericRow(ctx, q, fk.table, seq, depth+1); err != nil {
			return nil, err
		}
		err = q.QueryRowContext(ctx, sel).Scan(&v)
	}
	return v, err
}
