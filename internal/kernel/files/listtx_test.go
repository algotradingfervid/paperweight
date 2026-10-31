package files_test

import (
	"context"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// listTx calls ListTx inside db.Write on a pool of one connection (CD-209,
// CD-213): a second connection fails testkit.NoSecondConn, and a nested
// transaction exits the binary under -tags nestcheck. p nil puts no
// principal on ctx, as in a hook.
func listTx(t *testing.T, e *nsFiles, p *auth.Principal, parent auth.RecordRef) ([]files.Meta, error) {
	t.Helper()
	var got []files.Meta
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		if p != nil {
			ctx = auth.WithPrincipal(ctx, *p)
		}
		return e.DB.Write(ctx, func(tx *db.Tx) (err error) {
			got, err = e.Store.ListTx(ctx, tx, parent)
			return err
		})
	})
	return got, err
}

// MOD-EXP X2, spec §3.3: ListTx reads the parent's attachments in the
// caller's transaction, newest first like List, with no rights check, as a
// non-admin on the real auth.Service.
func TestStore_ListTx_CallerTx(t *testing.T) {
	e := newNsFiles(t)
	first := e.seeded(t)
	e.Clock.Advance(5 * time.Minute)
	second, err := e.Attach(e.writer, e.parent, "taxi.pdf", filestest.PDF(200))
	if err != nil {
		t.Fatalf("setup Attach(taxi.pdf): %v", err)
	}
	e.Clock.Advance(5 * time.Minute)
	third, err := e.Attach(e.writer, e.parent, "hotel.jpg", filestest.JPEG(300))
	if err != nil {
		t.Fatalf("setup Attach(hotel.jpg): %v", err)
	}
	// Another document's attachment must not appear.
	e.Docs.SetInfo(doctype.Info{
		DocType: nsDocType, DocLabel: "Expense Claim", ID: 43, State: doctype.StateDraft, Display: "Draft 43", CreatedBy: e.writer.ID,
	})
	other := auth.RecordRef{DocType: nsDocType, ID: 43, CreatedBy: e.writer.ID}
	if _, err := e.Attach(e.writer, other, "other.pdf", filestest.PDF(400)); err != nil {
		t.Fatalf("setup Attach(other.pdf): %v", err)
	}

	// ListTx returns the same order as List and e.Rows: newest first.
	rows := e.Rows(e.parent)
	if len(rows) != 3 || rows[0].ID != third || rows[1].ID != second || rows[2].ID != first {
		t.Fatalf("setup rows = %+v, want ids %d, %d, %d newest first", rows, third, second, first)
	}
	want := rows

	for _, tc := range []struct {
		name string
		p    *auth.Principal
	}{
		{"non-admin writer", &e.writer},
		{"non-admin reader", &e.reader},
		{"no principal (hook)", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := listTx(t, e, tc.p, e.parent)
			if err != nil {
				t.Fatalf("ListTx = %v, want nil", err)
			}
			wantMetas(t, got, want)
		})
	}

	t.Run("no attachments", func(t *testing.T) {
		e.Docs.SetInfo(doctype.Info{
			DocType: nsDocType, DocLabel: "Expense Claim", ID: 44, State: doctype.StateDraft, Display: "Draft 44", CreatedBy: e.writer.ID,
		})
		got, err := listTx(t, e, &e.writer, auth.RecordRef{DocType: nsDocType, ID: 44, CreatedBy: e.writer.ID})
		if err != nil || len(got) != 0 {
			t.Errorf("ListTx(empty parent) = %+v, %v; want no rows, nil", got, err)
		}
	})
}

// wantMetas compares got with want field by field, in order.
func wantMetas(t *testing.T, got, want []files.Meta) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("ListTx = %d rows %+v, want %d %+v", len(got), got, len(want), want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if g.ID != w.ID || g.Name != w.Name || g.MIME != w.MIME || g.Size != w.Size ||
			g.SHA256 != w.SHA256 || g.UploadedBy != w.UploadedBy || !g.UploadedAt.Equal(w.UploadedAt) {
			t.Errorf("ListTx[%d] = %+v, want %+v", i, g, w)
		}
	}
}
