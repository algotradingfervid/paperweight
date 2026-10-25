package printtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// TableDocType is the DocType TableView and TableInfo serve.
const TableDocType = "test.PurchaseOrder"

// CreateTable creates the test parent table print_test_po, which holds the
// party name and address saved on each document, and the party master
// print_test_party, which TableView never reads (FILE-063).
func CreateTable(t testing.TB, d *db.DB) {
	t.Helper()
	err := d.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `
CREATE TABLE print_test_po (
	id            INTEGER PRIMARY KEY,
	state         TEXT NOT NULL,
	number        TEXT NOT NULL DEFAULT '',
	party_name    TEXT NOT NULL,
	party_address TEXT NOT NULL,
	version       INTEGER NOT NULL,
	updated_at    TEXT NOT NULL
);
CREATE TABLE print_test_party (
	id      INTEGER PRIMARY KEY,
	name    TEXT NOT NULL,
	address TEXT NOT NULL
);`)
		return err
	})
	if err != nil {
		t.Fatalf("printtest.CreateTable: %v", err)
	}
}

// TableView is a print.View for TableDocType. It reads only print_test_po and
// prints ReferencePO with the party name and address saved on the row, on
// PUNE's letterhead from LetterheadFor.
func TableView(ctx context.Context, tx *db.Tx, id int64) (templ.Component, error) {
	var name, address string
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT party_name, party_address FROM print_test_po WHERE id = ?`, id).Scan(&name, &address)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("printtest: print_test_po %d: %w", id, doctype.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	lh, err := print.LetterheadFor(ctx, tx, PuneID)
	if err != nil {
		return nil, err
	}
	d := ReferencePO()
	d.Letterhead = lh
	d.Party.Name = name
	d.Party.Address = []string{address}
	return print.TradeDocument(d), nil
}

// TableInfo is a doctype.InfoSource over print_test_po: DocLabel
// "Purchase Order", Display the number or "Draft {id}".
type TableInfo struct{}

// Info returns the row's facts, or doctype.ErrNotFound for another DocType or
// a missing row.
func (TableInfo) Info(ctx context.Context, tx *db.Tx, dt string, id int64) (doctype.Info, error) {
	if dt != TableDocType {
		return doctype.Info{}, fmt.Errorf("printtest: DocType %s: %w", dt, doctype.ErrNotFound)
	}
	var state, number string
	err := tx.SQL().QueryRowContext(ctx,
		`SELECT state, number FROM print_test_po WHERE id = ?`, id).Scan(&state, &number)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.Info{}, fmt.Errorf("printtest: print_test_po %d: %w", id, doctype.ErrNotFound)
	}
	if err != nil {
		return doctype.Info{}, err
	}
	display := number
	if display == "" {
		display = fmt.Sprintf("Draft %d", id)
	}
	return doctype.Info{
		DocType: dt, DocLabel: "Purchase Order", ID: id,
		State: doctype.State(state), Number: number, Display: display,
	}, nil
}

var _ doctype.InfoSource = TableInfo{}
