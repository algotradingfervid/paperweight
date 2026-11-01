package grnweb

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// Cell kinds for csvio.FormatCell, so the reports' cells read as the core
// export's do (ISO dates, plain numbers, formula-escaped text).
var (
	textCell  = doctype.Field{Type: doctype.Text}
	dateCell  = doctype.Field{Type: doctype.Date}
	qtyCell   = doctype.Field{Type: doctype.Qty}
	rateCell  = doctype.Field{Type: doctype.Rate}
	moneyCell = doctype.Field{Type: doctype.Money}
)

// utf8BOM starts every CSV, as the core export's does.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// The CSV headers: spec §12's columns, in its order.
var (
	billsHeader = []string{"GRN no.", "Receipt date", "Vendor", "GSTIN", "PO no.", "Challan no.", "Invoice no.", "Invoice date", "Invoice total", "Accepted value", "Photos"}
	itemsHeader = []string{"GRN no.", "Date", "Vendor", "PO no.", "Item", "HSN", "Received", "Accepted", "Rejected", "Reason", "UoM", "Rate", "Accepted value"}
)

// records is a report's header and data rows for p, filtered by q.
type records func(ctx context.Context, p auth.Principal, q url.Values) ([]string, [][]string, error)

// exportCSV answers GET /reports/grn/{report}/export.csv with recs's rows.
// AUTH's guard checks read; export is checked here (CD-145), before any
// filter is parsed or row read (CD-259).
func (h *handlers) exportCSV(report string, recs records) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.writeCSV(w, r, report, recs); err != nil {
			web.ErrorFrom(w, r, err)
		}
	}
}

// writeCSV writes the report's CSV, or returns the error before writing.
func (h *handlers) writeCSV(w http.ResponseWriter, r *http.Request, report string, records records) error {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := h.d.Service.CanExport(ctx, p); err != nil {
		return err
	}
	header, recs, err := records(ctx, p, r.URL.Query())
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
	w.Header().Set("Content-Disposition", `attachment; filename="grn-`+report+`.csv"`)
	_, _ = w.Write(buf.Bytes()) //nolint:gosec // G705: CSV cells through csvio.FormatCell, sent as an attachment
	return nil
}

// billsRecords is Bills to enter's header and rows, filtered by q's nobill=1,
// vendor and month.
func (h *handlers) billsRecords(ctx context.Context, p auth.Principal, q url.Values) ([]string, [][]string, error) {
	bf, err := billsFilter(q)
	if err != nil {
		return nil, nil, err
	}
	rows, err := h.d.Service.BillsReport(ctx, p, bf)
	var recs [][]string
	for _, r := range rows {
		var total any // nil: blank with no bill details
		if r.InvoiceTotal.Valid {
			total = r.InvoiceTotal.V
		}
		recs = append(recs, []string{
			cell(textCell, r.Number), dateText(r.ReceiptDate), cell(textCell, r.Vendor), cell(textCell, r.GSTIN),
			cell(textCell, r.PONumber), cell(textCell, r.ChallanNo), cell(textCell, r.InvoiceNo), dateText(r.InvoiceDate),
			cell(moneyCell, total), cell(moneyCell, r.AcceptedValue), strconv.Itoa(r.Photos),
		})
	}
	return billsHeader, recs, err
}

// itemsRecords is Received items' header and rows, filtered by q's
// rejected=1, vendor, item and month.
func (h *handlers) itemsRecords(ctx context.Context, p auth.Principal, q url.Values) ([]string, [][]string, error) {
	itf, err := itemsFilter(q)
	if err != nil {
		return nil, nil, err
	}
	rows, err := h.d.Service.ItemsReport(ctx, p, itf)
	var recs [][]string
	for _, r := range rows {
		recs = append(recs, []string{
			cell(textCell, r.Number), dateText(r.Date), cell(textCell, r.Vendor), cell(textCell, r.PONumber),
			cell(textCell, r.ItemName+" ("+r.ItemCode+")"), cell(textCell, r.HSN),
			cell(qtyCell, r.Received), cell(qtyCell, r.Accepted), cell(qtyCell, r.Rejected),
			cell(textCell, r.Reason), cell(textCell, r.UoM), cell(rateCell, r.Rate), cell(moneyCell, r.AcceptedValue),
		})
	}
	return itemsHeader, recs, err
}

// billsFilter is Bills to enter's filter in q: nobill=1, vendor and month.
// The page and the CSV both read it here, so they parse alike.
func billsFilter(q url.Values) (goodsreceipt.BillsFilter, error) {
	f := filters{q: q}
	bf := goodsreceipt.BillsFilter{NoBill: q.Get("nobill") == "1", VendorID: f.id("vendor", "Vendor"), Month: f.month()}
	return bf, f.err
}

// itemsFilter is Received items' filter in q: rejected=1, vendor, item and
// month, read alike by the page and the CSV.
func itemsFilter(q url.Values) (goodsreceipt.ItemsFilter, error) {
	f := filters{q: q}
	itf := goodsreceipt.ItemsFilter{VendorID: f.id("vendor", "Vendor"), ItemID: f.id("item", "Item"), Month: f.month(), RejectedOnly: q.Get("rejected") == "1"}
	return itf, f.err
}

// filters reads the report filters in q, as the kernel list reads its
// filters and the expense CSVs do; err is the first bad one, a
// list.QueryError (list's status).
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

func cell(f doctype.Field, v any) string { return csvio.FormatCell(f, v) }

// dateText is d's ISO text, blank for the zero Date.
func dateText(d types.Date) string {
	if d == (types.Date{}) {
		return ""
	}
	return cell(dateCell, d)
}

// tooMany is the generic export's refusal of more than csvio.MaxExportRows
// rows (csvio.Exporter.Export's text, which csvio doesn't export).
func tooMany(n int) error {
	return &list.QueryError{Msg: "Too many rows to export (" + types.FormatCount(int64(n)) + "). Narrow the filters to 50,000 rows or fewer."}
}
