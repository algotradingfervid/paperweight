package types_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

func TestSchema_NoFloatColumns(t *testing.T) { // NUM-016
	d := testkit.NewDB(t)
	ctx := context.Background()

	type column struct{ table, name, declType string }
	var cols []column
	tables := 0
	err := d.Read(ctx, func(tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT name FROM sqlite_schema
			 WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
			   AND name NOT LIKE 'goose\_%' ESCAPE '\'
			 ORDER BY name`)
		if err != nil {
			return err
		}
		var names []string
		for rows.Next() {
			var n string
			if err := rows.Scan(&n); err != nil {
				_ = rows.Close()
				return err
			}
			names = append(names, n)
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		tables = len(names)
		for _, tbl := range names {
			info, err := tx.SQL().QueryContext(ctx, `SELECT name, type FROM pragma_table_info(?)`, tbl)
			if err != nil {
				return err
			}
			for info.Next() {
				var c column
				c.table = tbl
				if err := info.Scan(&c.name, &c.declType); err != nil {
					_ = info.Close()
					return err
				}
				cols = append(cols, c)
			}
			if err := info.Close(); err != nil {
				return err
			}
			if err := info.Err(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading schema: %v", err)
	}
	if tables == 0 {
		t.Fatal("no application tables found; the migrations did not run")
	}
	for _, c := range cols {
		switch strings.ToUpper(c.declType) {
		case "INTEGER", "TEXT", "BLOB":
		default:
			t.Errorf("%s.%s has declared type %s; use INTEGER, TEXT or BLOB", c.table, c.name, c.declType)
		}
	}
}

func TestSQL_SumIsExactInteger(t *testing.T) { // NUM-017
	d := testkit.NewDB(t)
	ctx := context.Background()

	var m types.Money
	if _, ok := any(&m).(sql.Scanner); !ok {
		t.Fatal("*types.Money does not implement sql.Scanner")
	}

	var sum types.Money
	var sumErr, totalErr error
	err := d.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if _, err := q.ExecContext(ctx, `CREATE TABLE numt_amounts(amount INTEGER NOT NULL)`); err != nil {
			return err
		}
		stmt, err := q.PrepareContext(ctx, `INSERT INTO numt_amounts(amount) VALUES (?)`)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for range 10000 {
			if _, err := stmt.ExecContext(ctx, types.Money(1)); err != nil {
				return err
			}
		}
		sumErr = q.QueryRowContext(ctx, `SELECT SUM(amount) FROM numt_amounts`).Scan(&sum)
		var total types.Money
		totalErr = q.QueryRowContext(ctx, `SELECT TOTAL(amount) FROM numt_amounts`).Scan(&total)
		return nil
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if sumErr != nil {
		t.Fatalf("SUM scan: %v", sumErr)
	}
	if sum != 10000 {
		t.Errorf("SUM(amount) = %d, want 10000", sum)
	}

	var ie *types.InputError
	if !errors.As(totalErr, &ie) {
		t.Fatalf("TOTAL scan err = %v (%T), want *types.InputError", totalErr, totalErr)
	}
	const want = "Money column returned a decimal value; use SUM on integer columns."
	if ie.Msg != want {
		t.Errorf("TOTAL scan Msg = %q, want %q", ie.Msg, want)
	}
}
