package audit_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-163: auditfake refuses exactly what the audit store refuses.
func TestAudit_ValidateEntryParity(t *testing.T) {
	d := testkit.NewDB(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(e *audit.Entry)
		ok     bool
	}{
		{"valid", func(*audit.Entry) {}, true},
		{"nil Changes", func(e *audit.Entry) { e.Changes = nil }, true},
		{"Op add with LineID", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, LineID: 503, Op: audit.OpAdd, After: `{"qty":3000}`}}
		}, true},
		{"Op remove with LineID", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, LineID: 503, Op: audit.OpRemove, Before: `{"qty":3000}`}}
		}, true},
		{"empty DocType", func(e *audit.Entry) { e.DocType = "" }, false},
		{"empty Action", func(e *audit.Entry) { e.Action = "" }, false},
		{"zero At", func(e *audit.Entry) { e.At = time.Time{} }, false},
		{"empty Op", func(e *audit.Entry) {
			e.Changes = []audit.Change{{Field: "remarks", Before: `""`, After: `"x"`}}
		}, false},
		{"unknown Op", func(e *audit.Entry) {
			e.Changes = []audit.Change{{Field: "qty", LineNo: 2, LineID: 502, Op: "move"}}
		}, false},
		{"Op add with LineID 0", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, Op: audit.OpAdd, After: `{"qty":3000}`}}
		}, false},
		{"Op remove with LineID 0", func(e *audit.Entry) {
			e.Changes = []audit.Change{{LineNo: 3, Op: audit.OpRemove, Before: `{"qty":3000}`}}
		}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := validEntry()
			c.mutate(&e)

			valErr := audit.ValidateEntry(e)
			storeErr := d.Write(ctx, func(tx *db.Tx) error {
				return audit.Store{}.Append(ctx, tx, e)
			})
			rec := auditfake.New()
			fakeErr := rec.Append(ctx, nil, e)

			if (storeErr == nil) != c.ok {
				t.Fatalf("Store.Append(%+v) = %v, want ok=%v", e, storeErr, c.ok)
			}
			if (fakeErr == nil) != (storeErr == nil) {
				t.Fatalf("auditfake Append = %v, Store.Append = %v, want both to agree", fakeErr, storeErr)
			}
			if (valErr == nil) != (storeErr == nil) {
				t.Fatalf("ValidateEntry = %v, Store.Append = %v, want both to agree", valErr, storeErr)
			}
			if c.ok {
				if n := len(rec.Entries()); n != 1 {
					t.Errorf("auditfake recorded %d entries, want 1", n)
				}
				return
			}
			// The store has no sentinel errors, so the same check shows as the same message.
			if fakeErr.Error() != storeErr.Error() || valErr.Error() != storeErr.Error() {
				t.Errorf("errors differ: store %q, fake %q, ValidateEntry %q", storeErr, fakeErr, valErr)
			}
			if n := len(rec.Entries()); n != 0 {
				t.Errorf("auditfake recorded %d refused entries, want 0", n)
			}
		})
	}
}
