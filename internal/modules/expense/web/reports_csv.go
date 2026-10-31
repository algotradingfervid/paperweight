package expenseweb

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// Cell kinds for csvio.FormatCell, so the reports' cells read as the core
// export's do (money Plain, ISO dates, Yes/No, formula-escaped text).
var (
	textCell  = doctype.Field{Type: doctype.Text}
	moneyCell = doctype.Field{Type: doctype.Money}
	dateCell  = doctype.Field{Type: doctype.Date}
	checkCell = doctype.Field{Type: doctype.Check}
)

// utf8BOM starts every CSV, as the core export's does.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// The CSV headers: spec §13's columns, in its order.
var (
	openHeader     = []string{"Claim", "Employee", "Purpose", "Approved on", "Approved by", "Total", "Less advance", "To pay", "Lines without a receipt"}
	linesHeader    = []string{"Claim", "Employee", "Date", "Type", "Project", "Site", "Note", "Amount", "GST", "Supplier GSTIN", "Invoice no.", "Billed-to GSTIN", "Receipt", "State"}
	advancesHeader = []string{"Advance", "Employee", "Date", "Purpose", "Amount", "Paid on", "Used", "Left", "Age in days"}
)

// exportCSV answers GET /reports/expense/{open,lines,advances}/export.csv.
// AUTH's guard checks read; export is checked here (CD-145), before any row
// is read.
func (h *handlers) exportCSV(w http.ResponseWriter, r *http.Request) {
	if err := h.writeCSV(w, r); err != nil {
		web.ErrorFrom(w, r, err)
	}
}

// writeCSV writes the report's CSV, or returns the error before writing.
func (h *handlers) writeCSV(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	report := path.Base(path.Dir(r.URL.Path))
	dt := expense.ClaimDocType
	if report == "advances" {
		dt = expense.AdvanceDocType
	}
	if err := h.d.Auth.Can(ctx, p, dt, auth.RightExport, nil); err != nil {
		return err
	}
	header, recs, err := h.reportRecords(ctx, p, report, r.URL.Query())
	if err != nil {
		return err
	}
	if len(recs) > csvio.MaxExportRows {
		return tooMany(len(recs))
	}
	var buf bytes.Buffer
	buf.Write(utf8BOM)
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true
	_ = cw.Write(header) // a bytes.Buffer doesn't fail
	_ = cw.WriteAll(recs)
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="expense-`+report+`.csv"`)
	_, _ = w.Write(buf.Bytes()) //nolint:gosec // G705: CSV cells through csvio.FormatCell, sent as an attachment
	return nil
}

// reportRecords is report's header and data rows, filtered by q.
func (h *handlers) reportRecords(ctx context.Context, p auth.Principal, report string, q url.Values) ([]string, [][]string, error) {
	f := filters{q: q}
	var recs [][]string
	switch report {
	case "open":
		of := expense.OpenClaimsFilter{EmployeeID: f.id("employee", "Employee")}
		if f.err != nil {
			return nil, nil, f.err
		}
		rows, err := h.d.Service.OpenClaimsReport(ctx, p, of)
		for _, r := range rows {
			var on any // nil: blank with no approval history
			if r.ApprovedOn != (types.Date{}) {
				on = r.ApprovedOn
			}
			recs = append(recs, []string{
				cell(textCell, r.Claim), cell(textCell, r.Employee), cell(textCell, r.Purpose),
				cell(dateCell, on), cell(textCell, r.ApprovedBy),
				cell(moneyCell, r.Total), cell(moneyCell, r.LessAdvance), cell(moneyCell, r.ToPay),
				strconv.Itoa(r.NoReceipt),
			})
		}
		return openHeader, recs, err
	case "lines":
		lf := expense.LinesFilter{
			Month: f.month(), TypeID: f.id("type", "Type"), ProjectID: f.id("project", "Project"), EmployeeID: f.id("employee", "Employee"),
		}
		if f.err != nil {
			return nil, nil, f.err
		}
		rep, err := h.d.Service.ExpenseLinesReport(ctx, p, lf)
		for _, r := range rep.Rows {
			recs = append(recs, []string{
				cell(textCell, r.Claim), cell(textCell, r.Employee), cell(dateCell, r.Date),
				cell(textCell, r.Type), cell(textCell, r.Project), cell(textCell, r.Site), cell(textCell, r.Note),
				cell(moneyCell, r.Amount), cell(moneyCell, r.GST),
				cell(textCell, r.SupplierGSTIN), cell(textCell, r.InvoiceNo), cell(textCell, r.BilledToGSTIN),
				cell(checkCell, r.Receipt), cell(textCell, string(r.State)),
			})
		}
		return linesHeader, recs, err
	}
	af := expense.AdvancesFilter{EmployeeID: f.id("employee", "Employee")}
	if f.err != nil {
		return nil, nil, f.err
	}
	rows, err := h.d.Service.AdvancesReport(ctx, p, af)
	for _, r := range rows {
		var paid any // nil: blank while unpaid
		if r.PaidOn != nil {
			paid = *r.PaidOn
		}
		recs = append(recs, []string{
			cell(textCell, r.Advance), cell(textCell, r.Employee), cell(dateCell, r.Date), cell(textCell, r.Purpose),
			cell(moneyCell, r.Amount), cell(dateCell, paid), cell(moneyCell, r.Used), cell(moneyCell, r.Left),
			strconv.Itoa(r.AgeDays),
		})
	}
	return advancesHeader, recs, err
}

func cell(f doctype.Field, v any) string { return csvio.FormatCell(f, v) }

// tooMany is the generic export's refusal of more than csvio.MaxExportRows
// rows (csvio.Exporter.Export's text, which csvio doesn't export).
func tooMany(n int) error {
	return &list.QueryError{Msg: "Too many rows to export (" + types.FormatCount(int64(n)) + "). Narrow the filters to 50,000 rows or fewer."}
}

// filters reads the report filters in q, as the kernel list reads its
// filters; err is the first bad one, a list.QueryError (list's status).
type filters struct {
	q   url.Values
	err error
}

// id is the record id in key, labelled label; blank is 0, every row.
func (f *filters) id(key, label string) int64 {
	v := strings.TrimSpace(f.q.Get(key))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil && f.err == nil {
		f.err = &list.QueryError{Msg: "Choose a value for " + label + "."}
	}
	return n
}

// month is the "YYYY-MM" month; blank is every month. A bad month gets
// types.ParseDate's date text.
func (f *filters) month() string {
	m := strings.TrimSpace(f.q.Get("month"))
	if m == "" {
		return ""
	}
	_, err := types.ParseDate(m + "-01") // a date only when m is yyyy-mm
	if err == nil {
		return m
	}
	if _, bad := types.ParseDate(m); bad != nil {
		err = bad // the text names the month as typed
	}
	if f.err == nil {
		f.err = &list.QueryError{Msg: err.Error()}
	}
	return ""
}
