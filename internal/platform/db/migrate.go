package db

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
)

// Migrate runs the goose migrations in fsys against d, recording versions in table.
func Migrate(ctx context.Context, d *DB, fsys fs.FS, table string) error {
	p, err := goose.NewProvider(goose.DialectSQLite3, d.sql, fsys, goose.WithTableName(table))
	if err != nil {
		return fmt.Errorf("set up migrations (table %s): %w", table, err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations (table %s): fix the failing migration and run again: %w", table, err)
	}
	return nil
}
