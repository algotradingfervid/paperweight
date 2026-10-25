// Package db is the only way the application touches SQLite: Open, Write
// (BEGIN IMMEDIATE, all-or-nothing) and Read.
//
// STUB (WP0 Task 3 test step): bodies return errs.ErrNotImplemented or zero
// values until the implementer fills them in.
package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/vardhan/internaltools/internal/platform/errs"
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
	tx *sql.Tx
}

// SQL returns the transaction as a DBTX (CD-06).
func (tx *Tx) SQL() DBTX { return nil }

// Writable reports whether the transaction was opened by Write (CD-07).
func (tx *Tx) Writable() bool { return false }

// DB is an open SQLite database.
type DB struct {
	sql  *sql.DB
	path string
}

// ErrBusy is returned when the database stayed locked past the busy timeout.
// STUB: placeholder value; the real text is set by the implementer.
var ErrBusy = errors.New("db.ErrBusy stub: not implemented yet")

// Open opens the SQLite file at path.
func Open(path string, opts ...Option) (*DB, error) {
	return nil, errs.ErrNotImplemented
}

// Write runs fn inside BEGIN IMMEDIATE and commits if fn returns nil.
func (db *DB) Write(ctx context.Context, fn func(tx *Tx) error) error {
	return errs.ErrNotImplemented
}

// Read runs fn inside a read-only transaction.
func (db *DB) Read(ctx context.Context, fn func(tx *Tx) error) error {
	return errs.ErrNotImplemented
}

// Close closes the database. WP0 addition.
func (db *DB) Close() error { return errs.ErrNotImplemented }

// SQL returns the underlying pool, for goose and tests only. WP0 addition.
func (db *DB) SQL() *sql.DB { return nil }

// Path returns the file path the database was opened with. WP0 addition.
func (db *DB) Path() string { return "" }
