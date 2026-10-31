package purchaseweb

import (
	"bytes"
	"encoding/csv"
	"net/http"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
)

// pendingHeader is the CSV header: PO spec §13's columns in its order, with
// Expected on (§27.1).
var pendingHeader = []string{
	"PO number", "Order date", "Required by", "Expected on", "Vendor", "Project", "Site",
	"Item code", "Item name", "Ordered", "Received", "Pending", "UoM", "Net rate", "Pending value",
}

// pendingBOM starts the CSV, as the core export's does.
var pendingBOM = []byte{0xEF, 0xBB, 0xBF}

// Cell kinds for csvio.FormatCell, so the cells read as the core export's
// do (ISO dates, plain numbers, formula-escaped text).
var (
	pendingText  = doctype.Field{Type: doctype.Text}
	pendingDate  = doctype.Field{Type: doctype.Date}
	pendingQty   = doctype.Field{Type: doctype.Qty}
	pendingRate  = doctype.Field{Type: doctype.Rate}
	pendingMoney = doctype.Field{Type: doctype.Money}
)

// pendingExport answers GET /reports/purchase/pending/export.csv: every
// pending report row p may read that matches the query's vendor, project,
// site and item ids and overdue=1. AUTH's guard checks read; export is
// checked here (CD-145), before any row is read. An id that isn't a number
// is AUTH's bad form.
func (h *handlers) pendingExport(w http.ResponseWriter, r *http.Request) {
	if err := h.writePendingCSV(w, r); err != nil {
		web.ErrorFrom(w, r, err)
	}
}

// writePendingCSV writes the CSV, or returns the error before writing.
func (h *handlers) writePendingCSV(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	if err := h.d.Service.CanExport(ctx, p); err != nil {
		return err
	}
	q := r.URL.Query()
	f := purchase.PendingFilter{OverdueOnly: q.Get("overdue") == "1"}
	for key, dst := range map[string]*int64{"vendor": &f.VendorID, "project": &f.ProjectID, "site": &f.SiteID, "item": &f.ItemID} {
		v := q.Get(key)
		if v == "" {
			continue
		}
		id, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return auth.ErrBadForm
		}
		*dst = id
	}
	view, err := h.d.Service.PendingReport(ctx, p, f, 0)
	if err != nil {
		return err
	}
	if len(view.Rows) > csvio.MaxExportRows {
		return tooMany(len(view.Rows))
	}
	var buf bytes.Buffer
	buf.Write(pendingBOM)
	cw := csv.NewWriter(&buf)
	cw.UseCRLF = true
	_ = cw.Write(pendingHeader) // a bytes.Buffer doesn't fail
	for _, row := range view.Rows {
		_ = cw.Write([]string{
			cell(pendingText, row.PONumber), dateCell(row.OrderDate), dateCell(row.RequiredBy), dateCell(row.ExpectedOn),
			cell(pendingText, row.Vendor), cell(pendingText, row.Project), cell(pendingText, row.Site),
			cell(pendingText, row.ItemCode), cell(pendingText, row.ItemName),
			cell(pendingQty, row.Ordered), cell(pendingQty, row.Received), cell(pendingQty, row.Pending),
			cell(pendingText, row.UoM), cell(pendingRate, row.NetRate), cell(pendingMoney, row.PendingValue),
		})
	}
	cw.Flush()
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="purchase-pending.csv"`)
	_, _ = w.Write(buf.Bytes()) //nolint:gosec // G705: CSV cells through csvio.FormatCell, sent as an attachment
	return nil
}

// tooMany is the generic export's refusal of more than csvio.MaxExportRows
// rows (csvio.Exporter.Export's text, which csvio doesn't export).
func tooMany(n int) error {
	return &list.QueryError{Msg: "Too many rows to export (" + types.FormatCount(int64(n)) + "). Narrow the filters to 50,000 rows or fewer."}
}

func cell(f doctype.Field, v any) string { return csvio.FormatCell(f, v) }

// dateCell is d's ISO text, blank for the zero Date.
func dateCell(d types.Date) string {
	if d == (types.Date{}) {
		return ""
	}
	return cell(pendingDate, d)
}
