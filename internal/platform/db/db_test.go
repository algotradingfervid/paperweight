package db_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite" // driver "sqlite" for the raw lock-holder connection

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/migrations"
)

// Review Focus 3: the DBTX method set is exactly what *sql.Tx and *sql.DB offer,
// so sqlc's dbq.New accepts tx.SQL().
var (
	_ db.DBTX = (*sql.Tx)(nil)
	_ db.DBTX = (*sql.DB)(nil)
)

const busyText = "The system is busy. Please try again in a moment."

// Note: these tests measure real elapsed time only where the plan names a
// duration (busy timeout). No business value reads the wall clock.

func openDB(t *testing.T, opts ...db.Option) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.Open(path, opts...)
	if err != nil {
		t.Fatalf("db.Open(%q) error = %v", path, err)
	}
	if d == nil {
		t.Fatalf("db.Open(%q) returned a nil *DB", path)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return d
}

func createTable(t *testing.T, d *db.DB) {
	t.Helper()
	pool := d.SQL()
	if pool == nil {
		t.Fatal("(*DB).SQL() returned nil")
	}
	if _, err := pool.ExecContext(context.Background(),
		`CREATE TABLE t (id INTEGER PRIMARY KEY, v TEXT NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func countRows(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.SQL().QueryRowContext(context.Background(), `SELECT count(*) FROM t`).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func insertOne(ctx context.Context, tx *db.Tx, v string) error {
	s := tx.SQL()
	if s == nil {
		return errors.New("tx.SQL() returned nil")
	}
	_, err := s.ExecContext(ctx, `INSERT INTO t (v) VALUES (?)`, v)
	return err
}

// pragmasOnTwoConns reads the given pragmas on two distinct pooled connections
// held at the same time.
func pragmasOnTwoConns(t *testing.T, d *db.DB, names ...string) [2]map[string]string {
	t.Helper()
	ctx := context.Background()
	pool := d.SQL()
	if pool == nil {
		t.Fatal("(*DB).SQL() returned nil")
	}
	c1, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("first Conn: %v", err)
	}
	defer func() { _ = c1.Close() }()
	c2, err := pool.Conn(ctx)
	if err != nil {
		t.Fatalf("second Conn: %v", err)
	}
	defer func() { _ = c2.Close() }()
	var out [2]map[string]string
	for i, c := range []*sql.Conn{c1, c2} {
		out[i] = map[string]string{}
		for _, name := range names {
			var v string
			if err := c.QueryRowContext(ctx, "PRAGMA "+name).Scan(&v); err != nil {
				t.Fatalf("connection %d: PRAGMA %s: %v", i+1, name, err)
			}
			out[i][name] = v
		}
	}
	return out
}

func TestDB_OpenSetsPragmas(t *testing.T) {
	d := openDB(t)
	want := map[string]string{
		"journal_mode": "wal",
		"busy_timeout": "5000",
		"foreign_keys": "1",
		"synchronous":  "1",
	}
	got := pragmasOnTwoConns(t, d, "journal_mode", "busy_timeout", "foreign_keys", "synchronous")
	for i := range got {
		for name, w := range want {
			if g := got[i][name]; g != w {
				t.Errorf("connection %d: PRAGMA %s = %q, want %q", i+1, name, g, w)
			}
		}
	}
}

func TestDB_WithBusyTimeout(t *testing.T) {
	d := openDB(t, db.WithBusyTimeout(200*time.Millisecond))
	got := pragmasOnTwoConns(t, d, "busy_timeout")
	for i := range got {
		if g := got[i]["busy_timeout"]; g != "200" {
			t.Errorf("connection %d: PRAGMA busy_timeout = %q, want %q", i+1, g, "200")
		}
	}
}

func TestDB_WriteCommits(t *testing.T) {
	d := openDB(t)
	createTable(t, d)
	ctx := context.Background()
	called := false
	err := d.Write(ctx, func(tx *db.Tx) error {
		called = true
		return insertOne(ctx, tx, "a")
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !called {
		t.Fatal("Write() did not call fn")
	}
	if n := countRows(t, d); n != 1 {
		t.Fatalf("rows after committed Write = %d, want 1", n)
	}
}

func TestDB_WriteRollsBackOnError(t *testing.T) {
	d := openDB(t)
	createTable(t, d)
	ctx := context.Background()
	errFail := errors.New("fn failed")
	inserted := false
	err := d.Write(ctx, func(tx *db.Tx) error {
		if err := insertOne(ctx, tx, "a"); err != nil {
			return err
		}
		inserted = true
		return errFail
	})
	if !inserted {
		t.Fatalf("insert inside Write did not run (Write error = %v)", err)
	}
	if !errors.Is(err, errFail) {
		t.Fatalf("Write() error = %v, want it to wrap %v", err, errFail)
	}
	if n := countRows(t, d); n != 0 {
		t.Fatalf("rows after failed Write = %d, want 0", n)
	}
}

func TestDB_WriteRollsBackOnPanic(t *testing.T) {
	d := openDB(t)
	createTable(t, d)
	ctx := context.Background()
	inserted := false
	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = d.Write(ctx, func(tx *db.Tx) error {
			if err := insertOne(ctx, tx, "a"); err != nil {
				return err
			}
			inserted = true
			panic("boom")
		})
		return nil
	}()
	if !inserted {
		t.Fatal("insert inside Write did not run before the panic")
	}
	if recovered != "boom" {
		t.Fatalf("recovered = %v, want the re-raised panic %q", recovered, "boom")
	}
	if n := countRows(t, d); n != 0 {
		t.Fatalf("rows after panicking Write = %d, want 0", n)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error { return insertOne(ctx, tx, "b") }); err != nil {
		t.Fatalf("Write() after panic error = %v, want nil", err)
	}
	if n := countRows(t, d); n != 1 {
		t.Fatalf("rows after the following Write = %d, want 1", n)
	}
}

func TestDB_ConcurrentWritesSerialise(t *testing.T) {
	d := openDB(t)
	createTable(t, d)
	ctx := context.Background()
	const perWriter = 200
	var wg sync.WaitGroup
	errsOut := make([]error, 2)
	for w := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range perWriter {
				err := d.Write(ctx, func(tx *db.Tx) error {
					if err := insertOne(ctx, tx, fmt.Sprintf("w%d-%d", w, i)); err != nil {
						return err
					}
					time.Sleep(time.Millisecond)
					return nil
				})
				if err != nil {
					errsOut[w] = fmt.Errorf("writer %d, call %d: %w", w, i, err)
					return
				}
			}
		}()
	}
	wg.Wait()
	for w, err := range errsOut {
		if err != nil {
			t.Errorf("writer %d: %v (ErrBusy: %v)", w, err, errors.Is(err, db.ErrBusy))
		}
	}
	if n := countRows(t, d); n != 2*perWriter {
		t.Fatalf("rows = %d, want %d", n, 2*perWriter)
	}
}

func TestDB_WriteBusyReturnsErrBusy(t *testing.T) {
	ctx := context.Background()
	d := openDB(t, db.WithBusyTimeout(200*time.Millisecond))
	createTable(t, d)
	path := d.Path()
	if path == "" {
		t.Fatal("(*DB).Path() returned an empty string")
	}

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("raw sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	holder, err := raw.Conn(ctx)
	if err != nil {
		t.Fatalf("raw Conn: %v", err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	if _, err := holder.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		t.Fatalf("holder BEGIN IMMEDIATE: %v", err)
	}

	start := time.Now()
	err = d.Write(ctx, func(tx *db.Tx) error { return insertOne(ctx, tx, "a") })
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Write() with the lock held = nil error, want db.ErrBusy")
	}
	if !errors.Is(err, db.ErrBusy) {
		t.Errorf("errors.Is(%v, db.ErrBusy) = false, want true", err)
	}
	if got := err.Error(); got != busyText {
		t.Errorf("err.Error() = %q, want %q", got, busyText)
	}
	if db.ErrBusy == nil || db.ErrBusy.Error() != busyText {
		t.Errorf("db.ErrBusy = %v, want text %q", db.ErrBusy, busyText)
	}
	if elapsed < 200*time.Millisecond || elapsed >= 2*time.Second {
		t.Errorf("Write() returned after %v, want >= 200ms and < 2s", elapsed)
	}

	if _, err := holder.ExecContext(ctx, "ROLLBACK"); err != nil {
		t.Fatalf("holder ROLLBACK: %v", err)
	}
	if n := countRows(t, d); n != 0 {
		t.Fatalf("rows after busy Write = %d, want 0", n)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error { return insertOne(ctx, tx, "b") }); err != nil {
		t.Fatalf("Write() after the holder rolled back error = %v, want nil", err)
	}
	if n := countRows(t, d); n != 1 {
		t.Fatalf("rows after the following Write = %d, want 1", n)
	}
}

func TestDB_TxWritable(t *testing.T) {
	d := openDB(t)
	ctx := context.Background()
	var inWrite, inRead, wCalled, rCalled bool
	if err := d.Write(ctx, func(tx *db.Tx) error {
		wCalled = true
		inWrite = tx.Writable()
		return nil
	}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := d.Read(ctx, func(tx *db.Tx) error {
		rCalled = true
		inRead = tx.Writable()
		return nil
	}); err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if !wCalled || !rCalled {
		t.Fatalf("fn called: Write %v, Read %v; want both", wCalled, rCalled)
	}
	if !inWrite {
		t.Error("tx.Writable() inside Write = false, want true")
	}
	if inRead {
		t.Error("tx.Writable() inside Read = true, want false")
	}
}

func TestDB_SQLCannotCommit(t *testing.T) {
	d := openDB(t)
	createTable(t, d)
	ctx := context.Background()
	called := false
	err := d.Write(ctx, func(tx *db.Tx) error {
		called = true
		s := tx.SQL()
		if s == nil {
			return errors.New("tx.SQL() returned nil")
		}
		if _, ok := s.(interface{ Commit() error }); ok {
			t.Error("tx.SQL() satisfies interface{ Commit() error }; it must not")
		}
		if _, ok := s.(interface{ Rollback() error }); ok {
			t.Error("tx.SQL() satisfies interface{ Rollback() error }; it must not")
		}
		if _, ok := s.(*sql.Tx); ok {
			t.Error("tx.SQL() is a *sql.Tx; it must be a wrapper")
		}
		// The wrapper still works as a DBTX.
		_, err := s.ExecContext(ctx, `INSERT INTO t (v) VALUES ('a')`)
		return err
	})
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if !called {
		t.Fatal("Write() did not call fn")
	}
	if n := countRows(t, d); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
}

func TestDB_MigrateAppliesEmbedded(t *testing.T) {
	d := openDB(t)
	ctx := context.Background()
	if err := db.Migrate(ctx, d, migrations.FS, "goose_db_version"); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	var v string
	if err := d.SQL().QueryRowContext(ctx,
		`SELECT value FROM app_meta WHERE key = 'schema_conventions'`).Scan(&v); err != nil {
		t.Fatalf("read app_meta: %v", err)
	}
	if v != "1" {
		t.Errorf("app_meta schema_conventions = %q, want %q", v, "1")
	}
	var n int
	if err := d.SQL().QueryRowContext(ctx,
		`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'sessions'`).Scan(&n); err != nil {
		t.Fatalf("look for sessions table: %v", err)
	}
	if n != 0 {
		t.Errorf("sessions tables = %d, want 0 (CD-14)", n)
	}
}

func TestDB_SQLiteVersionAtLeast(t *testing.T) {
	d := openDB(t)
	var v string
	if err := d.SQL().QueryRowContext(context.Background(), `select sqlite_version()`).Scan(&v); err != nil {
		t.Fatalf("select sqlite_version(): %v", err)
	}
	if !versionAtLeast(t, v, [3]int{3, 51, 3}) {
		t.Fatalf("sqlite_version() = %s, want >= 3.51.3", v)
	}
}

func versionAtLeast(t *testing.T, v string, min [3]int) bool {
	t.Helper()
	parts := strings.Split(v, ".")
	if len(parts) < 3 {
		t.Fatalf("sqlite_version() = %q, want MAJOR.MINOR.PATCH", v)
	}
	for i := range 3 {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			t.Fatalf("sqlite_version() = %q: %v", v, err)
		}
		if n != min[i] {
			return n > min[i]
		}
	}
	return true
}
