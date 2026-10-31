package purchase

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// P6's stubs (request rows and order status). P6 deletes this file and adds
// its own.

// applyOrder: nil until P6 refreshes the linked requests on approve.
func applyOrder(context.Context, *db.Tx, int64) error { return nil }

// reverseOrder: nil until P6 refreshes the linked requests on cancel.
func reverseOrder(context.Context, *db.Tx, int64) error { return nil }

// closeOrder: nil until P6 refreshes the linked requests on close.
func closeOrder(context.Context, *db.Tx, int64) error { return nil }

// checkRequestRows is the §20.5 post-state check P2's Validate calls last;
// nil is "no extra rule" until P6.
func checkRequestRows(context.Context, *db.Tx, int64) []doctype.FieldError { return nil } //nolint:unused // called by P2, built by P6

// requestWarnings are the over-request warnings P2's OrderWarnings adds;
// none until P6.
func requestWarnings(context.Context, *db.Tx, int64) ([]string, error) { return nil, nil } //nolint:unused // called by P2, built by P6

// refreshClosedRequests refreshes a closed PO's requests from the accepted
// map; P4's RefreshReceiptStatus calls it. Nothing to do until P6.
func refreshClosedRequests(context.Context, *db.Tx, int64, map[int64]types.Qty) error { return nil } //nolint:unused // called by P4, built by P6
