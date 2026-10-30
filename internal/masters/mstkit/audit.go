package mstkit

import (
	"context"
	"slices"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/platform/db"
)

type importingKey struct{}

// Importing marks ctx as a CSV import, for unit tests that don't go through
// csvio (M12).
func Importing(ctx context.Context) context.Context {
	return context.WithValue(ctx, importingKey{}, true)
}

// IsImporting reports whether ctx is a CSV import: csvio's RowFunc context
// (CD-60) or Importing (M12).
func IsImporting(ctx context.Context) bool {
	on, _ := ctx.Value(importingKey{}).(bool)
	return on || csvio.IsImport(ctx)
}

// notAudited are the bookkeeping columns the diff skips.
var notAudited = map[string]bool{"version": true, "updated_at": true, "updated_by": true}

// Audit records action on (k, id) with the fields that differ between before
// and after. An empty "save" writes nothing, and a "create" during an import
// writes nothing (csvio writes that entry, M12).
func (e Env) Audit(ctx context.Context, tx *db.Tx, p auth.Principal, k Kind, id int64, action string, before, after map[string]string) error {
	if action == "create" && IsImporting(ctx) {
		return nil
	}
	changes := diff(before, after)
	if action == "save" && len(changes) == 0 {
		return nil
	}
	return e.appendAudit(ctx, tx, audit.Entry{
		DocType: k.DocType,
		DocID:   id,
		Action:  action,
		Changes: changes,
		By:      p.ID,
		At:      e.Clock.Now(),
	})
}

// Unchanged reports that a save of before as after changes nothing, so it
// writes nothing: no version bump, no updated_at, no audit entry (CD-208). A
// second tab at the same version then still saves. Both maps must hold every
// stored field and line.
func Unchanged(before, after map[string]string) bool {
	return len(diff(before, after)) == 0
}

// diff lists the changed fields in name order.
func diff(before, after map[string]string) []audit.Change {
	fields := make([]string, 0, len(before)+len(after))
	for f := range before {
		fields = append(fields, f)
	}
	for f := range after {
		if _, ok := before[f]; !ok {
			fields = append(fields, f)
		}
	}
	slices.Sort(fields)
	var out []audit.Change
	for _, f := range fields {
		if notAudited[f] || before[f] == after[f] {
			continue
		}
		out = append(out, audit.Change{Op: audit.OpSet, Field: f, Before: before[f], After: after[f]})
	}
	return out
}
