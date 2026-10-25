package csvio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

type importEnv struct {
	db    *db.DB
	auth  *listtest.Auth
	audit *auditfake.Recorder
	im    *Importer
	p     auth.Principal
}

func newEnv(t *testing.T) *importEnv {
	t.Helper()
	calls = nil
	e := &importEnv{db: listtest.Setup(t), auth: listtest.NewAuth(), audit: auditfake.New()}
	e.im = NewImporter(e.db, e.auth, testkit.Clock(), e.audit)
	e.p = testkit.Principal(t, "admin@vardhan.test")
	return e
}

// seed loads the fixtures.
func (e *importEnv) seed(t *testing.T) listtest.IDs {
	t.Helper()
	return listtest.SeedFixtures(t, e.db)
}

func (e *importEnv) count(t *testing.T, table string) int {
	t.Helper()
	var n int
	err := e.db.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), "SELECT count(*) FROM "+table).Scan(&n)
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *importEnv) text(t *testing.T, q string, args ...any) string {
	t.Helper()
	var s string
	err := e.db.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), q, args...).Scan(&s)
	})
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return s
}

func errorRows(r Report) []int {
	var rows []int
	for _, e := range r.Errors {
		rows = append(rows, e.Row)
	}
	return rows
}

func errorTexts(r Report) []string {
	var s []string
	for _, e := range r.Errors {
		s = append(s, e.String())
	}
	return s
}

const partyHeader = "Code,Name,GSTIN,State,PIN,Phone,Remarks\r\n"

// partyFile builds n party rows (rows 2..n+1); edit changes a row's cells.
func partyFile(n int, edit func(row int, cells []string)) string {
	var b strings.Builder
	b.WriteString(partyHeader)
	for i := range n {
		row := i + 2
		cells := []string{fmt.Sprintf("P%03d", row), fmt.Sprintf("Party %d", row), "", "Maharashtra", "411001", "", ""}
		if edit != nil {
			edit(row, cells)
		}
		b.WriteString(strings.Join(cells, ",") + "\r\n")
	}
	return b.String()
}

func TestImport_Template(t *testing.T) { // NUM-140
	if got := string(Template(listtest.Item())); got != "\uFEFFCode,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled\r\n" {
		t.Fatalf("Template = %q", got)
	}
	if got := RequiredColumns(listtest.Item()); !reflect.DeepEqual(got, []string{"Code", "Name", "Type", "UoM"}) {
		t.Fatalf("RequiredColumns = %q", got)
	}
}

func TestImport_DryRunWritesNothing(t *testing.T) { // NUM-144
	e := newEnv(t)
	file := partyFile(120, func(row int, c []string) {
		switch row {
		case 7:
			c[1] = ""
		case 58:
			c[2] = "27AABCS1429B1Z5"
		}
	})
	r, err := e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if r.Rows != 120 || r.Ready != 118 || r.Imported != 0 {
		t.Fatalf("Rows %d Ready %d Imported %d", r.Rows, r.Ready, r.Imported)
	}
	if got := errorRows(r); !reflect.DeepEqual(got, []int{7, 58}) {
		t.Fatalf("error rows = %v (%q)", got, errorTexts(r))
	}
	if got := r.Summary(); got != "118 rows are ready, 2 rows have errors" {
		t.Fatalf("Summary = %q", got)
	}
	if n := e.count(t, "numt_party"); n != 0 {
		t.Fatalf("numt_party count = %d", n)
	}
	if got := e.audit.Entries(); len(got) != 0 {
		t.Fatalf("audit entries = %v", got)
	}
}

func TestImport_AllOrNothing(t *testing.T) { // NUM-145
	e := newEnv(t)
	ctx := context.Background()
	bad := partyFile(100, func(row int, c []string) {
		if row == 57 {
			c[2] = "27AABCS1429B1Z5"
		}
	})
	r, err := e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if r.Imported != 0 || e.count(t, "numt_party") != 0 {
		t.Fatalf("Imported %d, count %d", r.Imported, e.count(t, "numt_party"))
	}
	if got := r.Summary(); got != "Nothing was imported. Fix 1 row and upload again." {
		t.Fatalf("Summary = %q", got)
	}
	if len(r.Errors) != 1 || r.Errors[0].String() != "Row 57: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes." {
		t.Fatalf("Errors = %q", errorTexts(r))
	}
	if got := e.audit.Entries(); len(got) != 0 {
		t.Fatalf("audit entries after failed import = %d", len(got))
	}

	calls = nil
	r, err = e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(partyFile(100, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Imported != 100 || e.count(t, "numt_party") != 100 || len(r.Errors) != 0 {
		t.Fatalf("Imported %d, count %d, errors %q", r.Imported, e.count(t, "numt_party"), errorTexts(r))
	}
	if got := r.Summary(); got != "Imported 100 Parties." {
		t.Fatalf("Summary = %q", got)
	}
	entries := e.audit.Entries()
	if len(entries) != 100 {
		t.Fatalf("audit entries = %d", len(entries))
	}
	for _, en := range entries {
		want := audit.Entry{DocType: listtest.NameParty, DocID: en.DocID, Action: "create", Reason: "created by import", By: e.p.ID, At: en.At}
		if !reflect.DeepEqual(en, want) || !en.At.Equal(testkit.Clock().Now()) || en.DocID == 0 {
			t.Fatalf("entry = %+v", en)
		}
	}
	if len(calls) != 100 {
		t.Fatalf("RowFunc calls = %d", len(calls))
	}
	for _, c := range calls {
		if c.Tx != calls[0].Tx || !c.IsImport {
			t.Fatalf("call %+v: not the same transaction or IsImport false", c)
		}
	}
	if IsImport(ctx) {
		t.Fatal("IsImport true outside a RowFunc")
	}
}

func TestImport_RowValidationAllErrors(t *testing.T) { // NUM-146
	e := newEnv(t)
	e.seed(t)
	file := "Code,Name,Type,UoM\r\n" +
		"N-1,,Goods,kg\r\n" +
		"N-2,,Goods,kg\r\n" +
		"N-3,Three,Goods,\r\n" +
		"N-4,Four,Goods,kg\r\n"
	r, err := e.im.Preview(context.Background(), e.p, listtest.Item(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 2: Name is required.", "Row 3: Name is required.", "Row 4: UoM is required."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
	if len(calls) != 1 || calls[0].Row["code"] != "N-4" {
		t.Fatalf("RowFunc calls = %+v", calls)
	}
}

func TestImport_DuplicateCodes(t *testing.T) { // NUM-147
	e := newEnv(t)
	e.seed(t)
	file := partyFile(10, func(row int, c []string) {
		switch row {
		case 5, 9:
			c[0] = "KAVERI"
		case 11:
			c[0] = "SHREE"
		}
	})
	r, err := e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 9: Code KAVERI is repeated (also on row 5).", "Row 11: Code SHREE already exists."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}

func TestImport_DuplicateCodesIgnoreCase(t *testing.T) { // Review Focus 4
	e := newEnv(t)
	e.seed(t)
	file := partyFile(10, func(row int, c []string) {
		switch row {
		case 5:
			c[0] = "kaveri"
		case 9:
			c[0] = "KAVERI"
		case 11:
			c[0] = "shree"
		}
	})
	r, err := e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 9: Code KAVERI is repeated (also on row 5).", "Row 11: Code shree already exists."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}

func TestImport_LinkByCode(t *testing.T) { // NUM-148
	e := newEnv(t)
	ctx := context.Background()
	ids := e.seed(t)
	err := e.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, "UPDATE numt_uom SET disabled = 1 WHERE code = 'Box'")
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	err = e.db.Read(ctx, func(tx *db.Tx) error {
		mi, ok, err := listtest.UoM().Master.Lookup(ctx, tx, ids["Box"])
		if err != nil || !ok || !mi.Disabled {
			t.Fatalf("Box lookup = %+v, %v, %v", mi, ok, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("Code,Name,Type,UoM\r\n")
	for row := 2; row <= 8; row++ {
		uom := "kg"
		switch row {
		case 6:
			uom = "Kgs"
		case 8:
			uom = "Box"
		}
		fmt.Fprintf(&b, "N-%d,Item %d,Goods,%s\r\n", row, row, uom)
	}
	r, err := e.im.Preview(ctx, e.p, listtest.Item(), strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Row 6: UoM 'Kgs' not found. Use an existing UoM code such as kg.",
		"Row 8: UoM 'Box' is disabled. Enable it or use another UoM.",
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
	if len(calls) != 5 || calls[0].Row["uom"] != fmt.Sprint(ids["kg"]) {
		t.Fatalf("calls = %+v", calls)
	}
}

func TestImport_NumbersIndianFormat(t *testing.T) { // NUM-149
	e := newEnv(t)
	ctx := context.Background()
	e.seed(t)
	const header = "Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled\r\n"
	rows := "N-2,Two,Goods,kg,7216,GST18,\"1,41,120.50\",No\r\n" +
		"N-3,Three,Goods,kg,7216,GST18,141120.5,No\r\n"
	bad := "N-4,Four,Goods,kg,7216,GST18,\"1.234,56\",No\r\n" +
		"N-5,Five,Goods,kg,7216,GST18,12.12345,No\r\n"
	r, err := e.im.Preview(ctx, e.p, listtest.Item(), strings.NewReader(header+rows+bad))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`Row 4, column 'Standard Rate': "1.234,56" is not a valid number. Write it like 1234.50.`,
		"Row 5, column 'Standard Rate': Rate can have at most 4 decimal places.",
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
	r, err = e.im.Import(ctx, e.p, listtest.Item(), strings.NewReader(header+rows))
	if err != nil || r.Imported != 2 {
		t.Fatalf("err = %v, report = %+v", err, r)
	}
	for _, code := range []string{"N-2", "N-3"} {
		if got := e.text(t, "SELECT standard_rate FROM numt_item WHERE code = ?", code); got != "1411205000" {
			t.Fatalf("%s standard_rate = %s", code, got)
		}
	}
}

func TestImport_DatesDayFirst(t *testing.T) { // NUM-150
	e := newEnv(t)
	ctx := context.Background()
	const header = "Code,Name,Date of Joining\r\n"
	good := []string{"01-04-2025", "1/4/2025", "2025-04-01", "01-Apr-2025", "04/05/2025"}
	var b strings.Builder
	for i, d := range good {
		fmt.Fprintf(&b, "E-%d,Emp %d,%s\r\n", i+2, i+2, d)
	}
	r, err := e.im.Preview(ctx, e.p, listtest.Employee(), strings.NewReader(header+b.String()+"E-7,Emp 7,31-02-2025\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 7, column 'Date of Joining': 31-02-2025 is not a valid date. Use dd-mm-yyyy, for example 31-03-2025."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
	r, err = e.im.Import(ctx, e.p, listtest.Employee(), strings.NewReader(header+b.String()))
	if err != nil || r.Imported != 5 {
		t.Fatalf("err = %v, report = %+v", err, r)
	}
	for row, wantDate := range map[int]string{2: "2025-04-01", 3: "2025-04-01", 4: "2025-04-01", 5: "2025-04-01", 6: "2025-05-04"} {
		if got := e.text(t, "SELECT date_of_joining FROM numt_employee WHERE code = ?", fmt.Sprintf("E-%d", row)); got != wantDate {
			t.Fatalf("row %d date = %s, want %s", row, got, wantDate)
		}
	}
}

func TestImport_EncodingUTF8Only(t *testing.T) { // NUM-151
	ctx := context.Background()
	file := partyHeader + "SGT,Śrī Gaṇeśa Traders,,Maharashtra,411001,,₹\r\n"
	for _, in := range []string{file, "\uFEFF" + file} {
		e := newEnv(t)
		r, err := e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(in))
		if err != nil || r.Imported != 1 {
			t.Fatalf("err = %v, report = %+v", err, r)
		}
		if got := e.text(t, "SELECT name FROM numt_party WHERE code = 'SGT'"); got != "Śrī Gaṇeśa Traders" {
			t.Fatalf("name = %q", got)
		}
		if got := e.text(t, "SELECT remarks FROM numt_party WHERE code = 'SGT'"); got != "₹" {
			t.Fatalf("remarks = %q", got)
		}
	}
	e := newEnv(t)
	_, err := e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(partyHeader+"SGT,Caf\xe9,,,,,\r\n"))
	assertFileError(t, err, "This file isn't saved as UTF-8. In Excel choose Save As → CSV UTF-8 (Comma delimited) and upload again.")
}

func TestImport_TextPreserved(t *testing.T) { // NUM-154
	e := newEnv(t)
	ctx := context.Background()
	r, err := e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(partyHeader+"00123,Zero Traders,,Maharashtra,400001,09876543210,\r\n"))
	if err != nil || r.Imported != 1 {
		t.Fatalf("err = %v, report = %+v", err, r)
	}
	if got := e.text(t, "SELECT code || '|' || pin || '|' || phone FROM numt_party"); got != "00123|400001|09876543210" {
		t.Fatalf("stored = %q", got)
	}
	r, err = e.im.Preview(ctx, e.p, listtest.Party(), strings.NewReader(partyHeader+"P2,Two,2.7E+14,,,,\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 2: GSTIN 2.7E+14 must have 15 characters. Excel may have changed this value. Format the column as Text and retype it."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}

// failReader fails the test if anything reads it.
type failReader struct{ t *testing.T }

func (f failReader) Read([]byte) (int, error) {
	f.t.Error("file was read")
	return 0, io.EOF
}

func TestImport_NeedsImportRight(t *testing.T) { // NUM-156 (AUTH-081)
	ctx := context.Background()
	e := newEnv(t)
	e.p = testkit.Principal(t, "priya@vardhan.test")
	e.auth.Deny = map[auth.Right]bool{"import": true}
	const msg = "You don't have permission to import Item. Ask your administrator for a role that allows it."
	for name, run := range map[string]func(context.Context, auth.Principal, *doctype.DocType, io.Reader) (Report, error){
		"Preview": e.im.Preview, "Import": e.im.Import,
	} {
		e.auth.Calls = nil
		_, err := run(ctx, e.p, listtest.Item(), failReader{t})
		if !errors.Is(err, auth.ErrForbidden) || !strings.Contains(err.Error(), msg) {
			t.Fatalf("%s: err = %v", name, err)
		}
		if !reflect.DeepEqual(e.auth.Calls, []auth.Right{"import"}) {
			t.Fatalf("%s: Calls = %v", name, e.auth.Calls)
		}
	}

	e = newEnv(t)
	e.seed(t)
	before := e.count(t, "numt_item")
	e.auth.Deny = map[auth.Right]bool{"create": true}
	file := "Code,Name,Type,UoM\r\nN-2,Two,Goods,kg\r\nN-3,Three,Goods,kg\r\nN-4,Four,Goods,kg\r\n"
	r, err := e.im.Import(ctx, e.p, listtest.Item(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if r.Imported != 0 || e.count(t, "numt_item") != before || len(calls) != 0 {
		t.Fatalf("Imported %d, count %d (was %d), calls %d", r.Imported, e.count(t, "numt_item"), before, len(calls))
	}
	want := []string{
		"Row 2: you don't have permission to create Item.",
		"Row 3: you don't have permission to create Item.",
		"Row 4: you don't have permission to create Item.",
	}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}

func TestImport_NoImporterRegistered(t *testing.T) { // CD-32 (no case ID)
	e := newEnv(t)
	_, err := e.im.Preview(context.Background(), e.p, listtest.PO(), failReader{t})
	if !errors.Is(err, ErrNoImporter) {
		t.Fatalf("err = %v", err)
	}
	if n := e.count(t, "numt_po"); n != 0 || len(calls) != 0 {
		t.Fatalf("count %d, calls %d", n, len(calls))
	}
}

func TestImport_FormulaUnescaped(t *testing.T) { // NUM-155 (CD-60)
	e := newEnv(t)
	file := partyHeader +
		"P2,Two,,,,,'=SUM(A1)\r\n" +
		"P3,Three,,,,,'+91 98765 43210\r\n" +
		"P4,Four,,,,,'hello\r\n"
	r, err := e.im.Import(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil || r.Imported != 3 {
		t.Fatalf("err = %v, report = %+v", err, r)
	}
	want := map[string]string{"P2": "=SUM(A1)", "P3": "+91 98765 43210", "P4": "'hello"}
	for code, remarks := range want {
		if got := e.text(t, "SELECT remarks FROM numt_party WHERE code = ?", code); got != remarks {
			t.Fatalf("%s stored remarks = %q, want %q", code, got, remarks)
		}
	}
	if len(calls) != 3 {
		t.Fatalf("calls = %+v", calls)
	}
	for _, c := range calls {
		if got := c.Row["remarks"]; got != want[c.Row["code"]] {
			t.Fatalf("%s RowFunc remarks = %q, want %q", c.Row["code"], got, want[c.Row["code"]])
		}
	}
}

func TestImport_FailedRowRolledBack(t *testing.T) { // CD-32 per-row savepoint (no case ID)
	e := newEnv(t)
	file := partyHeader +
		"P2,Two,27AABCS1429B1Z5,,,,\r\n" +
		"P3,Three,,,,,\r\n"
	r, err := e.im.Preview(context.Background(), e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if r.Ready != 1 || len(r.Errors) != 1 || r.Errors[0].Row != 2 {
		t.Fatalf("report = %+v", r)
	}
	if len(calls) != 2 || calls[1].Parties != 0 {
		t.Fatalf("row 3 saw %d numt_party rows; row 2's insert wasn't rolled back (calls %+v)", calls[1].Parties, calls)
	}
}

func TestImport_LinkToEarlierRow(t *testing.T) { // CD-32 one transaction (no case ID)
	e := newEnv(t)
	ctx := context.Background()
	err := e.db.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, categoryTable)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	dt, _ := doctype.Lookup(nameCategory)
	file := "Code,Name,Parent\r\nTOOLS,Tools,\r\nDRILLS,Drills,TOOLS\r\n"
	r, err := e.im.Import(ctx, e.p, dt, strings.NewReader(file))
	if err != nil || r.Imported != 2 {
		t.Fatalf("err = %v, report = %+v (%q)", err, r, errorTexts(r))
	}
	got := e.text(t, "SELECT p.code FROM numt_csvio_category c JOIN numt_csvio_category p ON p.id = c.parent WHERE c.code = 'DRILLS'")
	if got != "TOOLS" {
		t.Fatalf("DRILLS parent = %q", got)
	}
}

func TestImport_CheckNotYesOrNo(t *testing.T) { // plan Task 11 step 2 Check cells (no case ID)
	e := newEnv(t)
	e.seed(t)
	file := "Code,Name,Type,UoM,Disabled\r\nN-2,Two,Goods,kg,maybe\r\nN-3,Three,Goods,kg,YES\r\n"
	r, err := e.im.Preview(context.Background(), e.p, listtest.Item(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{`Row 2, column 'Disabled': "maybe" is not Yes or No.`}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}

func TestImport_CodesUpperCasedASCIIOnly(t *testing.T) { // matches SQLite upper() (no case ID)
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.im.Import(ctx, e.p, listtest.Party(), strings.NewReader(partyHeader+"śri,One,,,,,\r\n")); err != nil {
		t.Fatal(err)
	}
	file := partyHeader + "Śri,Two,,,,,\r\nŚRI,Three,,,,,\r\nśRI,Four,,,,,\r\n"
	r, err := e.im.Preview(ctx, e.p, listtest.Party(), strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Row 3: Code ŚRI is repeated (also on row 2).", "Row 4: Code śRI already exists."}
	if got := errorTexts(r); !reflect.DeepEqual(got, want) {
		t.Fatalf("Errors = %q", got)
	}
}
