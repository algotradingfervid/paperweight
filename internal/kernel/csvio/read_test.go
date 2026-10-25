package csvio

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
)

func TestImport_CSVMechanics(t *testing.T) { // NUM-152
	const header = "Code,Name,GSTIN,State,PIN,Phone,Address,Remarks\r\n"
	in := header +
		"SHREE,\"Shah, Rao & Co\",27AAACS1234F1Z5,Maharashtra,411001,,\"12 MG Road\r\nPune\",\"12\"\" pipe\"\r\n" +
		"  BHARAT  ,  Bharat Steels  ,,  Pune  ,,,,\r\n" +
		"\r\n\r\n\r\n"
	s, err := ReadSheet(strings.NewReader(in))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(s.Header) != 8 || s.Header[7] != "Remarks" {
		t.Fatalf("Header = %q", s.Header)
	}
	want := []SheetRow{
		{Num: 2, Cells: []string{"SHREE", "Shah, Rao & Co", "27AAACS1234F1Z5", "Maharashtra", "411001", "", "12 MG Road\nPune", "12\" pipe"}},
		{Num: 3, Cells: []string{"BHARAT", "Bharat Steels", "", "Pune", "", "", "", ""}},
	}
	if !reflect.DeepEqual(s.Rows, want) {
		t.Fatalf("Rows = %#v, want %#v", s.Rows, want)
	}

	var b strings.Builder
	b.WriteString(header)
	for range 10 { // rows 2–11
		b.WriteString("A,B,C,D,E,F,G,H\r\n")
	}
	b.WriteString("A,B,C,D,E,F,G,H,I\r\n") // row 12
	_, err = ReadSheet(strings.NewReader(b.String()))
	assertFileError(t, err, "Row 12 has 9 values but the header has 8. Check for an unquoted comma.")
}

func TestImport_Limits(t *testing.T) { // NUM-153
	var b strings.Builder
	b.WriteString("Code,Name\n")
	for range 6200 {
		b.WriteString("A,B\n")
	}
	_, err := ReadSheet(strings.NewReader(b.String()))
	assertFileError(t, err, "This file has 6,200 rows. Import at most 5,000 rows at a time.")

	b.Reset()
	b.WriteString("Code,Name\n")
	for range MaxRows {
		b.WriteString("A,B\n")
	}
	s, err := ReadSheet(strings.NewReader(b.String()))
	if err != nil || len(s.Rows) != MaxRows {
		t.Fatalf("5,000 rows: err = %v", err)
	}

	big := append([]byte("Code,Name\n"), bytes.Repeat([]byte("A,B\n"), MaxFileBytes/4)...)
	if len(big) != MaxFileBytes+10 {
		t.Fatalf("test file is %d bytes", len(big))
	}
	_, err = ReadSheet(bytes.NewReader(big[:MaxFileBytes+1]))
	assertFileError(t, err, "This file is larger than 5 MB. Split it into smaller files.")

	_, err = ReadSheet(strings.NewReader("Code,Name\r\n"))
	assertFileError(t, err, "The file has no rows to import.")
	_, err = ReadSheet(strings.NewReader("Code,Name\r\n,\r\n\r\n"))
	assertFileError(t, err, "The file has no rows to import.")
}

func TestReadSheet_EncodingAndBOM(t *testing.T) {
	_, err := ReadSheet(strings.NewReader("Code,Name\nP1,Caf\xe9\n"))
	assertFileError(t, err, "This file isn't saved as UTF-8. In Excel choose Save As → CSV UTF-8 (Comma delimited) and upload again.")

	s, err := ReadSheet(strings.NewReader("\uFEFFCode,Name\nP1,Śrī Gaṇeśa Traders ₹\n"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(s.Header, []string{"Code", "Name"}) || s.Rows[0].Cells[1] != "Śrī Gaṇeśa Traders ₹" {
		t.Fatalf("sheet = %+v", s)
	}
}

func TestImport_ExcelArtefacts(t *testing.T) { // Review Focus 1
	s, err := ReadSheet(strings.NewReader("Code,Name,\r\nA1,One,\r\n,,\r\n\r\nA2,Two,\r\n"))
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	want := []SheetRow{
		{Num: 2, Cells: []string{"A1", "One", ""}},
		{Num: 5, Cells: []string{"A2", "Two", ""}},
	}
	if !reflect.DeepEqual(s.Rows, want) {
		t.Fatalf("Rows = %#v, want %#v", s.Rows, want)
	}
	cols, warns, err := MapHeader(listtest.Party(), s.Header)
	if err != nil || len(warns) != 0 {
		t.Fatalf("MapHeader: err = %v, warns = %q", err, warns)
	}
	if got := colNames(cols); !reflect.DeepEqual(got, []string{"0:code", "1:name"}) {
		t.Fatalf("cols = %q", got)
	}
}

func TestReadSheet_TooManyRowsKeepsAtMostMaxRows(t *testing.T) {
	var b strings.Builder
	b.WriteString("C\n")
	for range 200_000 {
		b.WriteString("A\n")
	}
	_, err := ReadSheet(strings.NewReader(b.String()))
	assertFileError(t, err, "This file has 2,00,000 rows. Import at most 5,000 rows at a time.")

	s, total, err := readRows([]byte(b.String()))
	if err != nil || total != 200_000 || len(s.Rows) != MaxRows || cap(s.Rows) > 2*MaxRows {
		t.Fatalf("err = %v, total = %d, len = %d, cap = %d", err, total, len(s.Rows), cap(s.Rows))
	}

	// The cell-count check still runs on rows past the limit.
	b.WriteString("A,B\n")
	_, err = ReadSheet(strings.NewReader(b.String()))
	assertFileError(t, err, "Row 200002 has 2 values but the header has 1. Check for an unquoted comma.")
}

func TestImport_BadQuoteRow(t *testing.T) { // AD decision (no case ID)
	// Row 2 spans two file lines, so the bad quote on file line 4 is row 3.
	in := "Code,Name\r\nA1,\"Two\r\nlines\"\r\nA2,Bad\"quote\r\n"
	_, err := ReadSheet(strings.NewReader(in))
	assertFileError(t, err, `Row 3 has a quote mark (") in the wrong place. Save the file again as CSV and try once more.`)

	_, err = ReadSheet(strings.NewReader("Code,Name\r\nA1,\"open\r\n"))
	assertFileError(t, err, `Row 2 has a quote mark (") in the wrong place. Save the file again as CSV and try once more.`)
}
