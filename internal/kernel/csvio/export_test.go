package csvio

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// readExport checks the BOM and returns the exported records.
func readExport(t *testing.T, b []byte) [][]string {
	t.Helper()
	rest, ok := bytes.CutPrefix(b, []byte("\uFEFF"))
	if !ok {
		t.Fatalf("export does not start with the UTF-8 BOM: %q", b[:min(len(b), 20)])
	}
	recs, err := csv.NewReader(bytes.NewReader(rest)).ReadAll()
	if err != nil {
		t.Fatalf("export is not CSV: %v", err)
	}
	return recs
}

func TestExport_MatchesListAndPermissions(t *testing.T) { // NUM-159
	ctx := context.Background()
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	metro, tech := ids["PRJ-METRO"], ids["PRJ-TECHPARK"]
	n := 0
	po := func(party, status string, project int64, total int) {
		n++
		listtest.InsertPO(t, d, listtest.PORow{Number: fmt.Sprintf("PO/26-27/%05d", n), PartyName: party,
			Status: status, ProjectID: project, GrandTotal: types.Money(total * 100000)})
	}
	for i := range 61 {
		// Totals out of insertion order, so a missing sort shows.
		po("Shree Steel Traders", "approved", metro, (i*37)%61+1)
	}
	for i := range 5 {
		po("Bharat Steel", "approved", tech, 100+i)
		po("Shree Steel Traders", "draft", metro, 200+i)
		po("Kaveri Cement", "approved", metro, 300+i)
	}
	priya := testkit.Principal(t, "priya@vardhan.test")
	a := listtest.NewAuth()
	a.Pred = auth.Predicate{SQL: "numt_po.project_id = ?", Args: []any{metro}}
	q := list.Query{
		Filters: []list.Filter{{Field: "status", Op: list.OpEq, Values: []string{"approved"}}},
		Search:  "steel",
		Sort:    []doctype.SortKey{{Field: "grand_total", Desc: true}},
	}

	var buf bytes.Buffer
	got, err := NewExporter(d, a).Export(ctx, &buf, priya, listtest.PO(), q)
	if err != nil {
		t.Fatal(err)
	}
	if got != 61 {
		t.Errorf("Export returned %d rows, want 61", got)
	}
	if want := []auth.Right{"export", "read"}; !reflect.DeepEqual(a.Calls, want) {
		t.Errorf("auth calls = %v, want %v", a.Calls, want)
	}
	recs := readExport(t, buf.Bytes())
	if len(recs) != 62 {
		t.Fatalf("export has %d records, want 1 header + 61 rows", len(recs))
	}
	var numbers []string
	for _, r := range recs[1:] {
		numbers = append(numbers, r[0])
		if r[4] != "PRJ-METRO" {
			t.Errorf("row %s: project %q, want PRJ-METRO", r[0], r[4])
		}
	}

	q.Page, q.PageSize = 1, 500
	res, err := (&list.Service{DB: d, Auth: a}).List(ctx, priya, listtest.PO(), q)
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, r := range res.Rows {
		want = append(want, r.Values["number"].(string))
	}
	if !reflect.DeepEqual(numbers, want) {
		t.Errorf("export order = %v\nlist order   = %v", numbers, want)
	}
}

func TestExport_NeedsExportRight(t *testing.T) { // NUM-160
	d := listtest.Setup(t)
	listtest.InsertPO(t, d, listtest.PORow{Number: "PO/26-27/00001", Status: "approved"})
	a := listtest.NewAuth()
	a.Deny = map[auth.Right]bool{"export": true}
	var buf bytes.Buffer
	n, err := NewExporter(d, a).Export(context.Background(), &buf, testkit.Principal(t, "suresh@vardhan.test"), listtest.PO(), list.Query{})
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("err = %v, want auth.ErrForbidden", err)
	}
	want := auth.ErrForbidden.Error() + ": You don't have permission to export Purchase Order. Ask your administrator for a role that allows it."
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
	if n != 0 || buf.Len() != 0 {
		t.Errorf("Export wrote %d rows, %d bytes; want nothing", n, buf.Len())
	}
	if !reflect.DeepEqual(a.Calls, []auth.Right{"export"}) {
		t.Errorf("auth calls = %v, want [export]", a.Calls)
	}
}

func TestExport_ValueFormats(t *testing.T) { // NUM-161
	d := listtest.Setup(t)
	ids := listtest.SeedFixtures(t, d)
	date, err := types.DateFromISO("2025-04-01")
	if err != nil {
		t.Fatal(err)
	}
	listtest.InsertPO(t, d, listtest.PORow{
		Number: "PO/26-27/00001", PartyName: "Shree Steel Traders", Status: "approved", Date: date,
		ProjectID: ids["PRJ-METRO"], GrandTotal: 1411200000, Qty: 2500, Rate: 123450, GSTRate: 18000,
		Remarks: "=SUM(A1)", CreatedAt: "2025-04-01T04:30:00.000Z",
	})
	var buf bytes.Buffer
	n, err := NewExporter(d, listtest.NewAuth()).Export(context.Background(), &buf, testkit.Principal(t, "ravi@vardhan.test"), listtest.PO(), list.Query{})
	if err != nil {
		t.Fatal(err)
	}
	want := "\uFEFFNumber,Party,Status,Date,Project,Branch,Grand Total,Qty,Rate,GST,Remarks,Created\r\n" +
		"PO/26-27/00001,Shree Steel Traders,approved,2025-04-01,PRJ-METRO,,14112000.00,2.5,12.345,18,'=SUM(A1),2025-04-01 10:00:00\r\n"
	if n != 1 || buf.String() != want {
		t.Errorf("Export = %d rows\n%q\nwant 1 row\n%q", n, buf.String(), want)
	}
}

func TestExport_FormulaEscapeTextOnly(t *testing.T) { // NUM-162
	text := doctype.Field{Name: "remarks", Label: "Remarks", Type: doctype.Text}
	for in, want := range map[string]string{
		`=HYPERLINK("http://x")`: `'=HYPERLINK("http://x")`,
		"+91 98765 43210":        "'+91 98765 43210",
		"-5 units":               "'-5 units",
		"@SUM(1)":                "'@SUM(1)",
		"Steel rods":             "Steel rods",
	} {
		if got := FormatCell(text, in); got != want {
			t.Errorf("FormatCell(text, %q) = %q, want %q", in, got, want)
		}
	}
	money := doctype.Field{Name: "rounding", Label: "Rounding", Type: doctype.Money}
	if got := FormatCell(money, types.Money(-49)); got != "-0.49" {
		t.Errorf("FormatCell(money, -49) = %q, want -0.49", got)
	}
	check := doctype.Field{Name: "disabled", Label: "Disabled", Type: doctype.Check}
	if got := FormatCell(check, true) + "," + FormatCell(check, false); got != "Yes,No" {
		t.Errorf("FormatCell(check, true/false) = %q, want Yes,No", got)
	}
	if got := FormatCell(text, nil); got != "" {
		t.Errorf("FormatCell(text, nil) = %q, want empty", got)
	}
}

func TestExportImport_RoundTrip(t *testing.T) { // NUM-163
	ctx := context.Background()
	gstins := []string{"27AABCS1429B1ZU", "29AABCT1332L1ZA", "24AAACR5055K1ZD", "33AAACT2727Q1Z3", "07AABCS1429B1ZW"}
	names := []string{"श्री गणेश ट्रेडर्स", "Café Ölund"}
	src := listtest.Setup(t)
	for i := range 50 {
		j := (i * 17) % 50 // insert out of name order
		listtest.InsertParty(t, src, listtest.PartyRow{
			Code: fmt.Sprintf("P%03d", j), Name: fmt.Sprintf("%s %02d", names[j%2], j), GSTIN: gstins[j%5],
			State: "Maharashtra", PIN: fmt.Sprintf("04%04d", j), Phone: "+91 98765 432" + fmt.Sprintf("%02d", j),
			Remarks: "=1+1", Disabled: j%7 == 0,
		})
	}
	a := listtest.NewAuth()
	admin := testkit.Principal(t, "admin@vardhan.test")
	var first bytes.Buffer
	if n, err := NewExporter(src, a).Export(ctx, &first, admin, listtest.Party(), list.Query{}); err != nil || n != 50 {
		t.Fatalf("Export = %d, %v; want 50 rows", n, err)
	}
	if !strings.Contains(first.String(), ",'=1+1,") || !strings.Contains(first.String(), ",'+91 98765 43200,") {
		t.Errorf("export does not escape the remarks and phone cells:\n%s", first.String())
	}

	dst := listtest.Setup(t)
	calls = nil
	rep, err := NewImporter(dst, a, testkit.Clock(), auditfake.New()).Import(ctx, admin, listtest.Party(), bytes.NewReader(first.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Imported != 50 || len(rep.Errors) != 0 {
		t.Fatalf("Import: %d imported, errors %q", rep.Imported, errorTexts(rep))
	}

	values := func(d *db.DB) []map[string]any {
		res, err := (&list.Service{DB: d, Auth: a}).List(ctx, admin, listtest.Party(), list.Query{Page: 1, PageSize: 500})
		if err != nil {
			t.Fatal(err)
		}
		var vs []map[string]any
		for _, r := range res.Rows {
			vs = append(vs, r.Values)
		}
		return vs
	}
	if got, want := values(dst), values(src); !reflect.DeepEqual(got, want) {
		t.Errorf("imported parties differ:\ngot  %v\nwant %v", got, want)
	}
	var second bytes.Buffer
	if _, err := NewExporter(dst, a).Export(ctx, &second, admin, listtest.Party(), list.Query{}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Errorf("second export differs:\n%s\nfirst:\n%s", second.String(), first.String())
	}
}
