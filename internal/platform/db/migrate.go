package db

import (
	"context"
	"io/fs"

	"github.com/vardhan/internaltools/internal/platform/errs"
)

// Migrate runs the goose migrations in fsys against d, recording versions in table.
func Migrate(ctx context.Context, d *DB, fsys fs.FS, table string) error {
	return errs.ErrNotImplemented
}
