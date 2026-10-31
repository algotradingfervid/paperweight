package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// P5's stubs (Request rules). P5 deletes this file and adds its own.

// validateRequest: nil is "no extra rule" until P5.
func validateRequest(context.Context, *db.Tx, int64) []doctype.FieldError { return nil }

// beforeSubmitRequest: nil is "no extra rule" until P5.
func beforeSubmitRequest(context.Context, *db.Tx, int64) error { return nil }
