package expenseweb_test

import (
	"bytes"
	"context"
	"errors"
	"html"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// e8ImplExec runs fixture SQL q in one write.
func e8ImplExec(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	err := e.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("fixture SQL: %v", err)
	}
}

// The CSV starts with a BOM, ends every line with CRLF, and escapes a note
// that a spreadsheet would run as a formula, as csvio.FormatCell does.
func TestE8Impl_CSVFormat(t *testing.T) {
	e := expensetest.New(t)
	e.ApprovedClaim(t, e.P(t, "deepak@vardhan.test"), nil, expensetest.Values{"description": "=SUM(A1:A9)"})
	e.E8Fill(t)

	rec := e8Get(t, e, "meena@vardhan.test", "/reports/expense/lines/export.csv")
	body := rec.Body.Bytes()
	if !bytes.HasPrefix(body, []byte(e8BOM)) {
		t.Errorf("body starts %q, want the BOM", body[:min(len(body), 8)])
	}
	lines := strings.SplitAfter(string(body), "\n")
	if last := lines[len(lines)-1]; last != "" {
		t.Errorf("body ends %q, want a line end", last)
	}
	for _, l := range lines[:len(lines)-1] {
		if !strings.HasSuffix(l, "\r\n") {
			t.Errorf("line %q doesn't end with CRLF", l)
		}
	}
	rows := e8CSV(t, rec, e8LinesHeader)
	if len(rows) != 1 {
		t.Fatalf("rows = %q, want one", rows)
	}
	note := strings.Split(rows[0], ",")[6]
	if want := csvio.EscapeFormula("=SUM(A1:A9)"); note != want || want != "'=SUM(A1:A9)" {
		t.Errorf("note = %q, want %q", note, want)
	}
}

// An approved claim with no approval history exports blank approved on and
// by.
func TestE8Impl_OpenCSVNoHistory(t *testing.T) {
	e := expensetest.New(t)
	c := e.DraftClaim(t, e.P(t, "deepak@vardhan.test"), nil)
	e8ImplExec(t, e, `UPDATE expense_claim SET state = 'approved', number = 'EX/26-27/00042' WHERE id = ?`, c.ID)
	e.E8Fill(t)

	rows := e8CSV(t, e8Get(t, e, "meena@vardhan.test", "/reports/expense/open/export.csv"), e8OpenHeader)
	wantRows(t, rows, []string{"EX/26-27/00042,Deepak Nair,Pune Metro site visit,,,1000.00,0.00,1000.00,1"}, e8OpenHeader)
}

// A filter id that isn't a number, or a month that isn't yyyy-mm, answers as
// the kernel list does: list.QueryError's status and list's text (AD ruling,
// E8 review round 1).
func TestE8Impl_BadFilters(t *testing.T) {
	e := expensetest.New(t)
	dateText := func(s string) string {
		_, err := types.ParseDate(s)
		if err == nil {
			t.Fatalf("ParseDate(%q) = nil, want an error", s)
		}
		return err.Error()
	}
	for _, tc := range []struct{ path, want string }{
		{"/reports/expense/lines/export.csv?employee=abc", "Choose a value for Employee."},
		{"/reports/expense/lines/export.csv?type=FOOD", "Choose a value for Type."},
		{"/reports/expense/lines/export.csv?project=1.5", "Choose a value for Project."},
		{"/reports/expense/advances/export.csv?employee=x", "Choose a value for Employee."},
		{"/reports/expense/lines/export.csv?month=2026-13", dateText("2026-13")},
		{"/reports/expense/lines/export.csv?month=2026-10-24", dateText("2026-10-24-01")},
	} {
		t.Run(tc.path, func(t *testing.T) {
			rec := e8Get(t, e, "meena@vardhan.test", tc.path)
			if want := (&list.QueryError{}).Status(); rec.Code != want {
				t.Errorf("status = %d, want list's %d", rec.Code, want)
			}
			if body := html.UnescapeString(rec.Body.String()); !strings.Contains(body, tc.want) {
				t.Errorf("body = %q, want %q", body, tc.want)
			}
		})
	}
}

// More than csvio.MaxExportRows rows is the generic export's refusal, with
// its status and text: 50,001 claims for the generic export, 50,001 lines
// of approved claims for the expense lines CSV.
func TestE8Impl_TooManyRows(t *testing.T) {
	ctx := context.Background()
	e := expensetest.New(t)
	c := e.ApprovedClaim(t, e.P(t, "deepak@vardhan.test"), nil)
	n := csvio.MaxExportRows
	e8ImplExec(t, e, `WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO expense_claim (state, created_at, created_by, updated_at, updated_by)
		SELECT 'draft', created_at, created_by, updated_at, updated_by FROM k, expense_claim WHERE expense_claim.id = ?`, n, c.ID)
	e8ImplExec(t, e, `WITH RECURSIVE k(i) AS (SELECT 1 UNION ALL SELECT i + 1 FROM k WHERE i < ?)
		INSERT INTO expense_claim_line (parent_id, line_no, expense_date, amount)
		SELECT ?, i + 1, '2026-10-24', 1 FROM k`, n, c.ID)

	meena := e.P(t, "meena@vardhan.test")
	dt, _ := doctype.Lookup(expense.ClaimDocType)
	var buf bytes.Buffer
	_, want := csvio.NewExporter(e.DB, e.Auth).Export(ctx, &buf, meena, dt, list.Query{})
	var se interface{ Status() int }
	if want == nil || !errors.As(want, &se) {
		t.Fatalf("generic export err = %v, want its too-many-rows refusal", want)
	}

	rec := e8Get(t, e, "meena@vardhan.test", "/reports/expense/lines/export.csv")
	if rec.Code != se.Status() {
		t.Errorf("status = %d, want the generic export's %d", rec.Code, se.Status())
	}
	if body := html.UnescapeString(rec.Body.String()); !strings.Contains(body, want.Error()) {
		t.Errorf("body = %q, want the generic export's %q", body[:min(len(body), 200)], want.Error())
	}
	if slices.Contains(strings.Split(rec.Body.String(), "\n"), strings.Join(e8LinesHeader, ",")+"\r") {
		t.Errorf("a refused export wrote the CSV")
	}
}
