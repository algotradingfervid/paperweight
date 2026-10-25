// Package csvio imports and exports CSV (spec §5, CD-32). NUM owns it.
package csvio

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

type RowFunc func(ctx context.Context, tx *db.Tx, p auth.Principal, row map[string]string) (id int64, errs []doctype.FieldError, err error)

func RegisterImporter(doctype string, fn RowFunc) {}
