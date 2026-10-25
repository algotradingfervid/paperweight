package mstkit

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// LookupFunc adapts a function to doctype.MasterLookup.
type LookupFunc func(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error)

// Lookup calls f.
func (f LookupFunc) Lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	return f(ctx, tx, id)
}

// StandardLookup reads code (code masters), name and disabled from k's table.
func StandardLookup(k Kind) doctype.MasterLookup {
	return LookupFunc(func(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
		snap, err := snapshot(ctx, tx, k, id)
		if errors.Is(err, doctype.ErrNotFound) {
			return doctype.MasterInfo{}, false, nil
		}
		if err != nil {
			return doctype.MasterInfo{}, false, err
		}
		return doctype.MasterInfo{Code: snap["code"], Name: snap["name"], Disabled: snap["disabled"] == "true"}, true, nil
	})
}

// RegisterMaster registers dt as a master (M1): no series, not submittable,
// approvable, printable or rounded, no hooks, Master = lookup and the
// production DefaultRights.
func RegisterMaster(dt doctype.DocType, lookup doctype.MasterLookup) {
	dt.Series = ""
	dt.Submittable = false
	dt.Approvable = false
	dt.Printable = false
	dt.HasRoundedTotal = false
	dt.Hooks = nil
	dt.Master = lookup
	dt.DefaultRights = DefaultRights(dt.Name)
	doctype.Register(dt)
}
