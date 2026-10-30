// Package db is the only way the application touches SQLite: Open, Write
// (BEGIN IMMEDIATE, all-or-nothing) and Read.
package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// DBTX is the method set sqlc generates (CD-06). It has no Commit or Rollback.
type DBTX interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	PrepareContext(context.Context, string) (*sql.Stmt, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Tx wraps a *sql.Tx.
type Tx struct {
	tx       *sql.Tx
	writable bool
}

// txSQL exposes only the DBTX methods of a *sql.Tx, so callers cannot commit
// or roll back, even by type assertion (CD-06).
type txSQL struct{ tx *sql.Tx }

func (s txSQL) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.tx.ExecContext(ctx, q, args...)
}

func (s txSQL) PrepareContext(ctx context.Context, q string) (*sql.Stmt, error) {
	return s.tx.PrepareContext(ctx, q)
}

func (s txSQL) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return s.tx.QueryContext(ctx, q, args...)
}

func (s txSQL) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return s.tx.QueryRowContext(ctx, q, args...)
}

// SQL returns the transaction as a DBTX (CD-06).
func (tx *Tx) SQL() DBTX { return txSQL{tx: tx.tx} }

// Writable reports whether the transaction was opened by Write (CD-07).
func (tx *Tx) Writable() bool { return tx.writable }

// DB is an open SQLite database.
type DB struct {
	sql  *sql.DB
	path string
}

// ErrBusy is returned when the database stayed locked past the busy timeout.
var ErrBusy = errors.New("The system is busy. Please try again in a moment.") //nolint:staticcheck // ST1005: user-facing sentence (CD-08)

// busyError reports ErrBusy's text while keeping the SQLite error as its cause.
type busyError struct{ cause error }

func (e busyError) Error() string        { return ErrBusy.Error() }
func (e busyError) Is(target error) bool { return target == ErrBusy }
func (e busyError) Unwrap() error        { return e.cause }

// mapBusy wraps err as ErrBusy if its SQLite primary result code is SQLITE_BUSY.
func mapBusy(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&0xff == sqlite3.SQLITE_BUSY {
		return busyError{cause: err}
	}
	return err
}

// Open opens the SQLite file at path. Every pooled connection gets WAL,
// the busy timeout, foreign keys on and synchronous NORMAL. Transactions
// begun on the pool are BEGIN IMMEDIATE unless read-only.
func Open(path string, opts ...Option) (*DB, error) {
	o := options{busyTimeout: defaultBusyTimeout}
	for _, opt := range opts {
		opt(&o)
	}
	q := url.Values{}
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", o.busyTimeout.Milliseconds()))
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Set("_txlock", "immediate")
	pool, err := sql.Open("sqlite", path+"?"+q.Encode())
	if err != nil {
		return nil, fmt.Errorf("open database %s: %w", path, err)
	}
	pool.SetMaxOpenConns(8)
	if err := pool.Ping(); err != nil {
		_ = pool.Close()
		return nil, fmt.Errorf("open database %s: check the path exists and is writable: %w", path, err)
	}
	return &DB{sql: pool, path: path}, nil
}

// Write runs fn inside BEGIN IMMEDIATE and commits if fn returns nil.
// It rolls back if fn returns an error or panics (the panic is re-raised).
func (db *DB) Write(ctx context.Context, fn func(tx *Tx) error) error {
	return db.run(ctx, nil, true, fn)
}

// Read runs fn inside a read-only transaction.
func (db *DB) Read(ctx context.Context, fn func(tx *Tx) error) error {
	return db.run(ctx, &sql.TxOptions{ReadOnly: true}, false, fn)
}

func (db *DB) run(ctx context.Context, txo *sql.TxOptions, writable bool, fn func(tx *Tx) error) (err error) {
	defer enterTx()()
	stx, err := db.sql.BeginTx(ctx, txo)
	if err != nil {
		return mapBusy(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = stx.Rollback()
		}
	}()
	if err := fn(&Tx{tx: stx, writable: writable}); err != nil {
		return mapBusy(err)
	}
	committed = true
	return mapBusy(stx.Commit())
}

// Close closes the database. WP0 addition.
func (db *DB) Close() error { return db.sql.Close() }

// SQL returns the underlying pool, for goose and tests only. WP0 addition.
func (db *DB) SQL() *sql.DB { return db.sql }

// Path returns the file path the database was opened with. WP0 addition.
func (db *DB) Path() string { return db.path }
