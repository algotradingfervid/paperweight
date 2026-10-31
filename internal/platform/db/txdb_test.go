package db_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-284: a Tx reports the *DB it was opened on, inside Read and Write.
func TestTx_DB(t *testing.T) {
	d := openDB(t)
	other := openDB(t)
	ctx := context.Background()
	check := func(name string, tx *db.Tx) {
		t.Helper()
		if got := tx.DB(); got != d {
			t.Errorf("%s: tx.DB() = %p, want %p", name, got, d)
		}
		if tx.DB() == other {
			t.Errorf("%s: tx.DB() is the other database", name)
		}
	}
	if err := d.Read(ctx, func(tx *db.Tx) error { check("Read", tx); return nil }); err != nil {
		t.Fatalf("Read error = %v", err)
	}
	if err := d.Write(ctx, func(tx *db.Tx) error { check("Write", tx); return nil }); err != nil {
		t.Fatalf("Write error = %v", err)
	}
}
