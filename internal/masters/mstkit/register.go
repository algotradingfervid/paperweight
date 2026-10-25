package mstkit

import (
	"context"
	"errors"
	"slices"

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
// approvable, printable or rounded, no hooks, Master = lookup, the
// production DefaultRights and a Filterable disabled field (CD-167). The
// declared DefaultSort is kept without keys that aren't declared fields
// (such as "id": the list adds id as the tie-breaker); if nothing is left
// and dt has a name field, DefaultSort is [name]. It panics if dt has no
// disabled field.
func RegisterMaster(dt doctype.DocType, lookup doctype.MasterLookup) {
	dt.Fields = slices.Clone(dt.Fields)
	i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == "disabled" })
	if i < 0 {
		panic("mstkit: RegisterMaster(" + dt.Name + "): master has no \"disabled\" field")
	}
	dt.Fields[i].Filterable = true
	declared := func(name string) bool {
		return slices.ContainsFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == name })
	}
	dt.DefaultSort = slices.DeleteFunc(slices.Clone(dt.DefaultSort), func(k doctype.SortKey) bool { return !declared(k.Field) })
	if len(dt.DefaultSort) == 0 && declared("name") {
		dt.DefaultSort = []doctype.SortKey{{Field: "name"}}
	}
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
