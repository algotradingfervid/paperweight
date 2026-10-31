package expenseweb_test

import (
	"bytes"
	"encoding/csv"
	"html"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
)

// The CSV headers: spec §13's columns, in its order.
var (
	e8OpenHeader     = []string{"Claim", "Employee", "Purpose", "Approved on", "Approved by", "Total", "Less advance", "To pay", "Lines without a receipt"}
	e8LinesHeader    = []string{"Claim", "Employee", "Date", "Type", "Project", "Site", "Note", "Amount", "GST", "Supplier GSTIN", "Invoice no.", "Billed-to GSTIN", "Receipt", "State"}
	e8AdvancesHeader = []string{"Advance", "Employee", "Date", "Purpose", "Amount", "Paid on", "Used", "Left", "Age in days"}
)

// AUTH's 403 for export (fixture text; CD-142).
const (
	e8NoExportClaims   = "You don't have permission to export Expense Claim. Ask your administrator for a role that allows it."
	e8NoExportAdvances = "You don't have permission to export Employee Advance. Ask your administrator for a role that allows it."
)

// e8Get serves path through the module's routes as the fixture user email.
// AUTH's route guard (read) is internal/app's; the handler checks export.
func e8Get(t *testing.T, e *expensetest.Env, email, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	expenseweb.Routes(expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files})(r)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), e.P(t, email)))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// e8CSV checks rec is a CSV download with header and returns its data rows
// sorted, each as one comma-joined string. A leading BOM is allowed.
func e8CSV(t *testing.T, rec *httptest.ResponseRecorder, header []string) []string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") {
		t.Errorf("Content-Disposition = %q, want an attachment", cd)
	}
	body := bytes.TrimPrefix(rec.Body.Bytes(), []byte(e8BOM))
	recs, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("read the CSV: %v; body %q", err, body)
	}
	if len(recs) == 0 {
		t.Fatalf("empty CSV, want the header %q", header)
	}
	if !slices.Equal(recs[0], header) {
		t.Errorf("header = %q, want %q", recs[0], header)
	}
	var rows []string
	for _, r := range recs[1:] {
		rows = append(rows, strings.Join(r, ","))
	}
	slices.Sort(rows)
	return rows
}

// e8BOM is the UTF-8 byte order mark, written as an escape (CLAUDE.md).
const e8BOM = "\ufeff"

// e8Denied checks rec is AUTH's 403 with text and carries no CSV.
func e8Denied(t *testing.T, rec *httptest.ResponseRecorder, text string, header []string) {
	t.Helper()
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rec.Code)
	}
	body := html.UnescapeString(rec.Body.String())
	if !strings.Contains(body, text) {
		t.Errorf("body = %q, want AUTH's %q", body, text)
	}
	if strings.Contains(body, strings.Join(header, ",")) {
		t.Errorf("body = %q: a refused export wrote the CSV", body)
	}
}

// wantRows compares sorted CSV rows, one error per missing or extra row,
// and per differing column when one row of each is left.
func wantRows(t *testing.T, got, want []string, header []string) {
	t.Helper()
	want = slices.Clone(want)
	slices.Sort(want)
	if slices.Equal(got, want) {
		return
	}
	if len(got) == len(want) {
		for i := range want {
			g, w := strings.Split(got[i], ","), strings.Split(want[i], ",")
			for c := range w {
				switch {
				case c >= len(g):
					t.Errorf("row %d: no column %s, want %q", i+1, header[c], w[c])
				case g[c] != w[c]:
					t.Errorf("row %d: %s = %q, want %q", i+1, header[c], g[c], w[c])
				}
			}
			if len(g) > len(w) {
				t.Errorf("row %d has %d columns, want %d", i+1, len(g), len(w))
			}
		}
		return
	}
	t.Errorf("rows =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
}

// EXP-034: the open claims CSV writes every column of every row Meena
// (Director: read and export, all) may read, and needs export: Priya
// (Employee: read team, no export) gets AUTH's 403 (CD-145). The world is
// expensetest.E8OpenScenario.
func TestExp_OpenReportsCSV(t *testing.T) {
	e := expensetest.New(t)
	e.E8OpenScenario(t)

	rows := e8CSV(t, e8Get(t, e, "meena@vardhan.test", "/reports/expense/open/export.csv"), e8OpenHeader)
	wantRows(t, rows, []string{
		"EX/26-27/00001,Deepak Nair,Pune Metro site visit,2026-10-27,Priya Shah,2080.55,1000.00,1080.55,1",
		"EX/26-27/00002,Kavya Rao,Whitefield client meeting,2026-10-28,Meena Iyer,2000.01,0.00,2000.01,2",
	}, e8OpenHeader)

	e8Denied(t, e8Get(t, e, "priya@vardhan.test", "/reports/expense/open/export.csv"), e8NoExportClaims, e8OpenHeader)
}

// EXP-034: the open advances CSV, every column (paid on blank when unpaid),
// its employee filter, and export (AUTH's 403 for Priya).
func TestExp_AdvancesReportCSV(t *testing.T) {
	e := expensetest.New(t)
	e.E8OpenScenario(t)
	deepak := "EA/26-27/00001,Deepak Nair,2026-10-24,Pune Metro site visit,5000.00,2026-10-25,1000.00,4000.00,10"
	kavya := "EA/26-27/00002,Kavya Rao,2026-10-20,Bengaluru client visit,3000.50,,0.00,3000.50,14"

	rows := e8CSV(t, e8Get(t, e, "meena@vardhan.test", "/reports/expense/advances/export.csv"), e8AdvancesHeader)
	wantRows(t, rows, []string{deepak, kavya}, e8AdvancesHeader)

	path := "/reports/expense/advances/export.csv?employee=" + strconv.FormatInt(e.E8Employee(t, "EMP-006"), 10)
	rows = e8CSV(t, e8Get(t, e, "meena@vardhan.test", path), e8AdvancesHeader)
	wantRows(t, rows, []string{kavya}, e8AdvancesHeader)

	e8Denied(t, e8Get(t, e, "priya@vardhan.test", "/reports/expense/advances/export.csv"), e8NoExportAdvances, e8AdvancesHeader)
}

// EXP-035: the expense lines CSV writes every column of every line, and each
// filter (month, type, project, employee) reaches the report. Export is
// needed (AUTH's 403 for Priya). The world is expensetest.E8LinesScenario.
func TestExp_LinesReportCSV(t *testing.T) {
	e := expensetest.New(t)
	e.E8LinesScenario(t)
	hotel := "EX/26-27/00001,Deepak Nair,2026-09-30,Lodging,PRJ-METRO,METRO-SITE,Hotel Pune 2 nights,4720.00,720.00,27AABCS1429B1ZU,INV/778,29AAFCV2449G1Z3,Yes,approved"
	lunch := "EX/26-27/00001,Deepak Nair,2026-10-22,Food,PRJ-TECHPARK,TECH-SITE,Team lunch,830.55,0.00,,,,No,approved"
	fuel := "EX/26-27/00002,Kavya Rao,2026-10-23,Fuel,,,Diesel for site jeep,0.05,0.00,,,,No,closed"
	id := func(n int64) string { return strconv.FormatInt(n, 10) }

	for _, tc := range []struct {
		name, query string
		want        []string
	}{
		{"all", "", []string{hotel, lunch, fuel}},
		{"month", "?month=2026-09", []string{hotel}},
		{"type", "?type=" + id(e.E8Type(t, "FOOD")), []string{lunch}},
		{"project", "?project=" + id(e.E8Project(t, "PRJ-METRO")), []string{hotel}},
		{"employee", "?employee=" + id(e.E8Employee(t, "EMP-006")), []string{fuel}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := e8CSV(t, e8Get(t, e, "meena@vardhan.test", "/reports/expense/lines/export.csv"+tc.query), e8LinesHeader)
			wantRows(t, rows, tc.want, e8LinesHeader)
		})
	}

	e8Denied(t, e8Get(t, e, "priya@vardhan.test", "/reports/expense/lines/export.csv"), e8NoExportClaims, e8LinesHeader)
}
