package csvio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

func TestImport_SemicolonHint(t *testing.T) { // NUM-157
	_, err := ReadSheet(strings.NewReader("Code;Name;UoM\r\nCEM-OPC53;Cement OPC 53;Bag\r\n"))
	assertFileError(t, err, "This file seems to use ';' between values. Save it as CSV (comma separated) and upload again.")

	// A semicolon inside a normal comma-separated header is not the hint.
	s, err := ReadSheet(strings.NewReader("Code,Name;Alias\r\nX,Y\r\n"))
	if err != nil {
		t.Fatalf("comma file with a ';' in a header cell: %v", err)
	}
	if !reflect.DeepEqual(s.Header, []string{"Code", "Name;Alias"}) {
		t.Errorf("Header = %q", s.Header)
	}

	e := newEnv(t)
	_, err = e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader("Code;Name;UoM\r\nA;B;C\r\n"))
	assertFileError(t, err, "This file seems to use ';' between values. Save it as CSV (comma separated) and upload again.")
}

func TestImport_ErrorRowsDownload(t *testing.T) { // NUM-158
	e := newEnv(t)
	file := partyFile(60, func(row int, c []string) {
		switch row {
		case 7:
			c[0], c[1], c[6] = "P003", "", "-urgent" // repeated code, no name, a formula trigger
		case 58:
			c[2] = "27AABCS1429B1Z5"
		}
	})
	// An extra, ignored column whose header starts with a formula trigger.
	file = strings.Replace(strings.ReplaceAll(file, "\r\n", ",\r\n"), "Remarks,\r\n", "Remarks,=Note\r\n", 1)
	sheet, err := ReadSheet(strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	r, err := e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got := errorRows(r); !reflect.DeepEqual(got, []int{7, 7, 58}) {
		t.Fatalf("error rows = %v (%q)", got, errorTexts(r))
	}

	recs := readExport(t, ErrorRowsCSV(sheet, r))
	want := [][]string{
		{"Code", "Name", "GSTIN", "State", "PIN", "Phone", "Remarks", "'=Note", "Error"},
		{"P003", "", "", "Maharashtra", "411001", "", "'-urgent", "",
			"Name is required.; Code P003 is repeated (also on row 3)."},
		{"P058", "Party 58", "27AABCS1429B1Z5", "Maharashtra", "411001", "", "", "",
			"GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."},
	}
	if !reflect.DeepEqual(recs, want) {
		t.Errorf("ErrorRowsCSV records =\n%q\nwant\n%q", recs, want)
	}
	if b := ErrorRowsCSV(sheet, Report{}); !bytes.Equal(b, []byte("\uFEFFCode,Name,GSTIN,State,PIN,Phone,Remarks,'=Note,Error\r\n")) {
		t.Errorf("no errors: %q, want the BOM and header only", b)
	}
}

func TestExport_RowLimit(t *testing.T) { // NUM-164
	ctx := context.Background()
	d := listtest.Setup(t)
	// 50,000 approved and 12,000 draft POs.
	err := d.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM n WHERE i < 62000)
			INSERT INTO numt_po (number, party_name, status, date, grand_total)
			SELECT printf('PO/26-27/%05d', i), 'Shree Steel Traders', CASE WHEN i <= 50000 THEN 'approved' ELSE 'draft' END,
			       '2026-04-01', i * 100 FROM n`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	ex := NewExporter(d, listtest.NewAuth())
	p := testkit.Principal(t, "admin@vardhan.test")

	var buf bytes.Buffer
	n, err := ex.Export(ctx, &buf, p, listtest.PO(), list.Query{})
	want := "Too many rows to export (62,000). Narrow the filters to 50,000 rows or fewer."
	if err == nil || err.Error() != want {
		t.Fatalf("62,000 rows: err = %v, want %q", err, want)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 422 {
		t.Errorf("err %v is not a 422 HTTPError", err)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("62,000 rows: wrote %d rows, %d bytes; want nothing", n, buf.Len())
	}

	cw := &countingWriter{}
	n, err = ex.Export(ctx, cw, p, listtest.PO(), list.Query{Filters: []list.Filter{{Field: "status", Op: list.OpEq, Values: []string{"approved"}}}})
	if err != nil {
		t.Fatal(err)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if n != MaxExportRows || MaxExportRows != 50000 {
		t.Errorf("50,000 rows: exported %d (MaxExportRows %d)", n, MaxExportRows)
	}
	if cw.n == 0 {
		t.Error("50,000 rows: nothing written")
	}
	if ms.HeapInuse >= 100<<20 {
		t.Errorf("HeapInuse after export = %d MB, want under 100", ms.HeapInuse>>20)
	}
}

type countingWriter struct{ n int }

func (w *countingWriter) Write(b []byte) (int, error) { w.n += len(b); return len(b), nil }

var _ io.Writer = (*countingWriter)(nil)
