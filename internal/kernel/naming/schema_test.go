package naming

import (
	"context"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const issuedInsert = `INSERT INTO naming_issued(doctype,doc_id,number,prefix,seq,root,amendment_no,issued_at)
	VALUES(?,?,?,?,?,?,?,?)`

func execWrite(ctx context.Context, d *db.DB, q string, args ...any) error {
	return d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, q, args...)
		return err
	})
}

func countIssued(t *testing.T, ctx context.Context, d *db.DB) (n int, number string) {
	t.Helper()
	err := d.Read(ctx, func(tx *db.Tx) error {
		if err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM naming_issued`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			return nil
		}
		return tx.SQL().QueryRowContext(ctx, `SELECT number FROM naming_issued WHERE doc_id = 1`).Scan(&number)
	})
	if err != nil {
		t.Fatalf("reading naming_issued: %v", err)
	}
	return n, number
}

func TestMigration_NamingTables(t *testing.T) {
	ctx := context.Background()
	d := testkit.NewDB(t)

	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `INSERT INTO naming_issued(doctype,doc_id,number,prefix,seq,root,amendment_no,issued_at)
			VALUES('fixture.Order',1,'PO/26-27/00001','PO/26-27/',1,'PO/26-27/00001',0,'2026-10-24T05:00:00.000Z')`)
		return err
	})
	if err != nil {
		t.Fatalf("insert into naming_issued: got %v, want nil", err)
	}

	const appendOnly = "naming_issued is append-only"
	upd := execWrite(ctx, d, `UPDATE naming_issued SET number='X'`)
	if upd == nil || !strings.Contains(upd.Error(), appendOnly) {
		t.Errorf("UPDATE naming_issued: got %v, want an error containing %q", upd, appendOnly)
	}
	del := execWrite(ctx, d, `DELETE FROM naming_issued`)
	if del == nil || !strings.Contains(del.Error(), appendOnly) {
		t.Errorf("DELETE FROM naming_issued: got %v, want an error containing %q", del, appendOnly)
	}
	if n, number := countIssued(t, ctx, d); n != 1 || number != "PO/26-27/00001" {
		t.Errorf("after UPDATE/DELETE: naming_issued has %d rows, doc 1 number %q; want 1 row, %q", n, number, "PO/26-27/00001")
	}

	// UNIQUE(doctype, doc_id): same doctype and doc_id, every other unique column different.
	dupDoc := execWrite(ctx, d, issuedInsert,
		"fixture.Order", 1, "PO/26-27/00002", "PO/26-27/", 2, "PO/26-27/00002", 0, "2026-10-24T05:00:00.000Z")
	if dupDoc == nil || !strings.Contains(dupDoc.Error(), "naming_issued.doctype, naming_issued.doc_id") {
		t.Errorf("second insert for fixture.Order doc 1: got %v, want UNIQUE(doctype, doc_id) violation", dupDoc)
	}

	// UNIQUE(number): same number, different doc_id and root.
	dupNumber := execWrite(ctx, d, issuedInsert,
		"fixture.Order", 2, "PO/26-27/00001", "PO/26-27/", 1, "PO/26-27/00003", 0, "2026-10-24T05:00:00.000Z")
	if dupNumber == nil || !strings.Contains(dupNumber.Error(), "naming_issued.number") {
		t.Errorf("second insert of number PO/26-27/00001: got %v, want UNIQUE(number) violation", dupNumber)
	}

	if n, _ := countIssued(t, ctx, d); n != 1 {
		t.Errorf("after rejected inserts: naming_issued has %d rows, want 1", n)
	}
}
