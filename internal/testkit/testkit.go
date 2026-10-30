// Package testkit gives tests a migrated temp SQLite database, its path, a
// fixed clock, the canonical fixtures and a filled auth.Directory fake.
package testkit

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
	appmigrations "github.com/vardhan/internaltools/migrations"
)

var (
	pathsMu sync.Mutex
	paths   = map[testing.TB]string{}
)

// NewDB creates app.db in a temp dir, opens it with the default busy
// timeout and applies the production migrations (table goose_db_version), the
// master tables included. Cleanup closes the database and deletes the temp dir.
func NewDB(t testing.TB) *db.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	d, err := db.Open(path)
	if err != nil {
		t.Fatalf("testkit.NewDB: %v", err)
	}
	pathsMu.Lock()
	paths[t] = path
	pathsMu.Unlock()
	t.Cleanup(func() {
		pathsMu.Lock()
		delete(paths, t)
		pathsMu.Unlock()
		if err := d.Close(); err != nil {
			t.Errorf("testkit.NewDB: close %s: %v", path, err)
		}
	})
	if err := db.Migrate(context.Background(), d, appmigrations.FS, "goose_db_version"); err != nil {
		t.Fatalf("testkit.NewDB: %v", err)
	}
	return d
}

// DBPath returns the file path of the database NewDB created for t (CD-08).
func DBPath(t testing.TB) string {
	t.Helper()
	pathsMu.Lock()
	path, ok := paths[t]
	pathsMu.Unlock()
	if !ok {
		t.Fatalf("testkit.DBPath: no database for this test; call testkit.NewDB(t) first")
	}
	return path
}

// DataDir returns a temp directory for files, deleted when the test ends.
func DataDir(t testing.TB) string {
	t.Helper()
	return t.TempDir()
}

// Clock returns a fixed clock at 2026-10-24T05:00:00Z (24-10-2026 10:30 IST).
func Clock() *clock.Fixed {
	return clock.NewFixed(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC))
}
