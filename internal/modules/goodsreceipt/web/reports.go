package grnweb

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// S4: the reports entry, Bills to enter and Received items (UX doc §5.10
// to §5.12). The rows are the G9 service's, filtered as the CSVs filter
// them (billsFilter, itemsFilter).

const reportsBase = "/reports/grn"

// reportsPageSize is the rows on one report page (UX doc §5.11).
const reportsPageSize = 25

// S4 copy (UX doc §5.10 to §5.12).
const (
	reportsTitle  = "Goods receipt reports"
	billsTitle    = "Bills to enter"
	billsLine     = "Receipts and their vendor bill details, for keying your accounts."
	itemsTitle    = "Received items"
	itemsLine     = "Every received line, with rejects and reasons."
	noBillYet     = "No bill details yet"
	rejectedOnly  = "Rejected only"
	billsNone     = "No approved goods receipts yet."
	billsAllBills = "Every receipt has its bill details."
	billsNoMatch  = "No receipts match these filters."
	itemsNone     = "No goods have been received yet."
	itemsNoReject = "Nothing has been rejected."
	itemsNoMatch  = "No lines match these filters."
	clearAll      = "Clear all"
	anyMonth      = "Any month"
	noMoreResults = "No more results."
	firstPage     = "Go to page 1"
	metaSep       = " · "
)

// reportKey is one id or month filter of a report: its query key, label,
// help text (its %s is the report's rows: receipts or lines) and example,
// and the master its Picker searches ("" for the month's Select).
type reportKey struct{ key, label, hint, example, docType, search string }

var (
	vendorKey = reportKey{"vendor", "Vendor", "Only %s from this vendor are listed.", "e.g. Shree Steel Traders", party.DocType, "/pick/" + party.DocType + "?role=vendor"}
	itemKey   = reportKey{"item", "Item", "Only %s of this item are listed.", "e.g. MS-ANGLE-50", item.DocType, "/pick/" + item.DocType}
	monthKey  = reportKey{key: "month", label: "Month", hint: "Only %s dated in this month are listed."}
)

// reportSpec is one report page's words, filters and toggle (the chip link
// in the bar: nobill or rejected).
type reportSpec struct {
	title, one, many, none, toggleNone, noMatch string
	toggle, toggleLabel                         string
	keys                                        []reportKey
	columns                                     []components.Column
}

var reportSpecs = map[string]reportSpec{
	"bills": {
		title: billsTitle, one: "receipt", many: "receipts", none: billsNone, toggleNone: billsAllBills, noMatch: billsNoMatch,
		toggle: "nobill", toggleLabel: noBillYet, keys: []reportKey{vendorKey, monthKey}, columns: billsColumns,
	},
	"items": {
		title: itemsTitle, one: "line", many: "lines", none: itemsNone, toggleNone: itemsNoReject, noMatch: itemsNoMatch,
		toggle: "rejected", toggleLabel: rejectedOnly, keys: []reportKey{vendorKey, itemKey, monthKey}, columns: itemsColumns,
	},
}

// The DataList columns (UX doc §5.11, §5.12); the CSVs have every column.
var (
	billsColumns = []components.Column{
		{Key: "grn", Label: "GRN no.", Priority: components.PTitle, Code: true, NoWrap: true},
		{Key: "value", Label: "Accepted value", Priority: components.PFigure, Num: true},
		{Key: "vendor", Label: "Vendor", Priority: components.PMeta},
		{Key: "receipt", Label: "Receipt date, PO no. and challan no.", Priority: components.PMeta},
		{Key: "bill", Label: "Bill details", Priority: components.PMeta},
		{Key: "photos", Label: "Photos", Priority: components.PMeta},
		{Key: "gstin", Label: "GSTIN", Priority: components.PLow, Code: true, NoWrap: true},
	}
	itemsColumns = []components.Column{
		{Key: "grn", Label: "GRN no.", Priority: components.PTitle, Code: true, NoWrap: true},
		{Key: "value", Label: "Accepted value", Priority: components.PFigure, Num: true},
		{Key: "item", Label: "Item", Priority: components.PMeta},
		{Key: "qty", Label: "Received, accepted and rejected", Priority: components.PMeta},
		{Key: "reason", Label: "Reject reason", Priority: components.PMeta},
		{Key: "vendor", Label: "Vendor", Priority: components.PMeta},
		{Key: "date_po", Label: "Date and PO no.", Priority: components.PLow},
		{Key: "hsn_rate", Label: "HSN and rate", Priority: components.PLow},
	}
)

// reportData is a report's rows for the request, each id filter's chip
// value from the rows' snapshots, and the months the rows fall in.
type reportData struct {
	rows   []components.Row
	labels map[string]string
	months []string // "YYYY-MM", newest first
}

// reportView is a report page.
type reportView struct {
	opts  layout.PageOpts
	bar   components.FilterBarProps
	list  components.DataListProps
	pages components.PageInfo
}

// filterView is a report's filter page: a GET form back to the report.
type filterView struct {
	opts           layout.PageOpts
	action, toggle string
	fields         []templ.Component
}

// reportLink is one report on the entry page.
type reportLink struct{ title, line, href string }

// reports answers GET /reports/grn (UX doc §5.10, CD-282 Q8): a card for
// each report with its Open link. AUTH's guard asks read on the GRN.
func (h *handlers) reports(w http.ResponseWriter, r *http.Request) {
	links := []reportLink{
		{billsTitle, billsLine, reportsBase + "/bills"},
		{itemsTitle, itemsLine, reportsBase + "/items"},
	}
	ui.Render(w, r, http.StatusOK, reportsPage(links), nil)
}

// report answers GET /reports/grn/{bills,items}: one page of the rows p may
// read that match the query, the filter bar (Export only with export,
// CD-145) and the pager. A bad filter is list's 422, as the CSV's; no read
// is AUTH's 403 (the service's Scope).
func (h *handlers) report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	name := path.Base(r.URL.Path)
	spec, base := reportSpecs[name], r.URL.Path
	data, err := h.load(ctx, p, name, r.URL.Query())
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	q := activeFilters(spec, r.URL.Query())
	v := reportView{
		opts: layout.PageOpts{Title: spec.title},
		bar:  components.FilterBarProps{Action: base, FilterURL: withQuery(base+"/filter", q), ClearURL: base},
		list: components.DataListProps{Label: spec.title, Columns: spec.columns},
	}
	if n := len(data.rows); n > 0 {
		v.bar.Count = counted(n, spec.one, spec.many)
	}
	for _, k := range spec.keys {
		if q.Has(k.key) {
			v.bar.Chips = append(v.bar.Chips, components.Chip{Label: k.label + ": " + chipValue(k.key, q.Get(k.key), data.labels), RemoveURL: withQuery(base, without(q, k.key))})
		}
	}
	if q.Has(spec.toggle) {
		v.bar.Chips = append(v.bar.Chips, components.Chip{Label: spec.toggleLabel, RemoveURL: withQuery(base, without(q, spec.toggle))})
	} else {
		on := without(q, spec.toggle)
		on.Set(spec.toggle, "1")
		v.bar.Links = []components.Link{{Label: spec.toggleLabel, Href: withQuery(base, on)}}
	}
	switch {
	case len(q) == 0:
		v.list.Empty = components.EmptyState(spec.none, nil)
	case len(q) == 1 && q.Has(spec.toggle):
		v.list.Empty = components.EmptyState(spec.toggleNone, nil)
	default:
		v.list.Empty = components.EmptyState(spec.noMatch, &components.Link{Label: clearAll, Href: base})
	}
	if h.d.Service.CanExport(ctx, p) == nil {
		v.bar.Export = &components.Link{Label: "Export", Href: withQuery(base+"/export.csv", q)}
	}
	// A page past the end is the one after the last: "No more results."
	page := min(pageNumber(r), (len(data.rows)+reportsPageSize-1)/reportsPageSize+1)
	from := min((page-1)*reportsPageSize, len(data.rows))
	v.list.Rows = data.rows[from:min(from+reportsPageSize, len(data.rows))]
	list := base + "?"
	if len(q) > 0 {
		list = withQuery(base, q) + "&"
	}
	if from == len(data.rows) && page > 1 {
		// Past the end: say so once, with the way back, not the empty text.
		v.list.Empty = components.EmptyState(noMoreResults, &components.Link{Label: firstPage, Href: list + "page=1"})
	} else {
		v.pages = pageInfo(list, spec.many, page, len(data.rows))
	}
	ui.Render(w, r, http.StatusOK, reportPage(v), reportMain(v))
}

// filterPage answers GET /reports/grn/{report}/filter with the report's
// filters set to the request's values: a Picker for each id filter whose
// master p may read (so its search answers) and the months that have
// receipts, newest first; the toggle is kept.
func (h *handlers) filterPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	base := path.Dir(r.URL.Path)
	name := path.Base(base)
	spec := reportSpecs[name]
	query := r.URL.Query()
	data, err := h.load(ctx, p, name, query)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	all, err := h.load(ctx, p, name, nil) // a second read, which can fail on its own (busy, cancelled)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	q := activeFilters(spec, query)
	v := filterView{
		opts:   layout.PageOpts{Title: "Filter " + strings.ToLower(spec.title), Width: layout.Form, Back: &components.Link{Label: spec.title, Href: withQuery(base, q)}},
		action: base,
	}
	if q.Has(spec.toggle) {
		v.toggle = spec.toggle
	}
	for _, k := range spec.keys {
		field := components.FieldProps{ID: "f-" + k.key, Label: k.label, Hint: fmt.Sprintf(k.hint, spec.many), Placeholder: k.example, Optional: true}
		if k.docType == "" {
			field.Name, field.Value = k.key, q.Get(k.key)
			v.fields = append(v.fields, components.Select(field, monthOptions(all.months, q.Get(k.key))))
			continue
		}
		if h.d.Auth.Can(ctx, p, k.docType, auth.RightRead, nil) != nil {
			continue
		}
		pp := components.PickerProps{Field: field, SearchURL: k.search, HiddenName: k.key, ListboxID: "f-" + k.key + "-list"}
		if id, err := strconv.ParseInt(q.Get(k.key), 10, 64); err == nil {
			pp.Chosen = &components.PickRow{ID: id, Name: chipValue(k.key, q.Get(k.key), data.labels)}
		}
		v.fields = append(v.fields, components.Picker(pp))
	}
	ui.Render(w, r, http.StatusOK, filterScreen(v), nil)
}

// load reads report's rows for q through the service (its scope), after
// reading q's filters as the CSV does.
func (h *handlers) load(ctx context.Context, p auth.Principal, report string, q url.Values) (reportData, error) {
	d := reportData{labels: map[string]string{}}
	months := map[string]bool{}
	if report == "bills" {
		f, err := billsFilter(q)
		if err != nil {
			return d, err
		}
		rows, err := h.d.Service.BillsReport(ctx, p, f)
		for _, r := range rows {
			d.labels["vendor"] = r.Vendor
			months[monthOf(r.ReceiptDate)] = true
			d.rows = append(d.rows, billRow(r))
		}
		d.months = newestFirst(months)
		return d, err
	}
	f, err := itemsFilter(q)
	if err != nil {
		return d, err
	}
	rows, err := h.d.Service.ItemsReport(ctx, p, f)
	for _, r := range rows {
		d.labels["vendor"], d.labels["item"] = r.Vendor, joinSet(r.ItemName, r.ItemCode)
		months[monthOf(r.Date)] = true
		d.rows = append(d.rows, itemRow(r))
	}
	d.months = newestFirst(months)
	return d, err
}

// billRow is r as a Bills to enter row, linking to its GRN.
func billRow(r goodsreceipt.BillRow) components.Row {
	bill := noBillYet
	if r.InvoiceNo != "" { // required on every bill details row
		var total string
		if r.InvoiceTotal.Valid {
			total = types.FormatINR(r.InvoiceTotal.V)
		}
		bill = joinSet(r.InvoiceNo, types.FormatDate(r.InvoiceDate), total)
	}
	return components.Row{Href: grnURL(r.ID), Values: map[string]string{
		"grn": r.Number, "value": types.FormatINR(r.AcceptedValue), "vendor": r.Vendor,
		"receipt": joinSet(types.FormatDate(r.ReceiptDate), r.PONumber, r.ChallanNo), "bill": bill,
		"photos": photosText(r.Photos), "gstin": r.GSTIN,
	}}
}

// itemRow is r as a Received items row, linking to its GRN.
func itemRow(r goodsreceipt.ItemRow) components.Row {
	var reason string
	if r.Rejected > 0 && r.Reason != "" {
		reason = "Rejected: " + r.Reason
	}
	var hsn string
	if r.HSN != "" {
		hsn = "HSN " + r.HSN
	}
	return components.Row{Href: grnURL(r.GRNID), Values: map[string]string{
		"grn": r.Number, "value": types.FormatINR(r.AcceptedValue), "item": joinSet(r.ItemName, r.ItemCode),
		"qty": "Received " + types.FormatQty(r.Received) + metaSep + "Accepted " + types.FormatQty(r.Accepted) +
			metaSep + "Rejected " + types.FormatQtyUoM(r.Rejected, r.UoM),
		"reason": reason, "vendor": r.Vendor, "date_po": joinSet(types.FormatDate(r.Date), r.PONumber),
		"hsn_rate": joinSet(hsn, "₹"+types.FormatRate(r.Rate)),
	}}
}

func grnURL(id int64) string {
	return "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10)
}

// photosText is "No photos", "1 photo" or "{n} photos".
func photosText(n int) string {
	if n == 0 {
		return "No photos"
	}
	return counted(n, "photo", "photos")
}

// monthOf is d's "YYYY-MM".
func monthOf(d types.Date) string { return d.ISO()[:7] }

// newestFirst is the months in set, newest first.
func newestFirst(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	slices.Sort(out)
	slices.Reverse(out)
	return out
}

// monthOptions is the Month Select: any month, then months; a chosen month
// with no receipts is kept, so Show results doesn't drop it.
func monthOptions(months []string, chosen string) []components.Option {
	if chosen != "" && !slices.Contains(months, chosen) {
		months = append([]string{chosen}, months...)
	}
	opts := []components.Option{{Value: "", Label: anyMonth}}
	for _, m := range months {
		opts = append(opts, components.Option{Value: m, Label: monthLabel(m)})
	}
	return opts
}

// joinSet joins the non-blank parts with " · ".
func joinSet(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), metaSep)
}

// counted is "1 {one}" or "{n} {many}".
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return types.FormatCount(int64(n)) + " " + many
}

// activeFilters is q's non-blank filters of spec, its toggle among them.
func activeFilters(spec reportSpec, q url.Values) url.Values {
	out := url.Values{}
	for _, k := range spec.keys {
		if v := strings.TrimSpace(q.Get(k.key)); v != "" {
			out.Set(k.key, v)
		}
	}
	if q.Get(spec.toggle) == "1" {
		out.Set(spec.toggle, "1")
	}
	return out
}

func withQuery(p string, q url.Values) string {
	if len(q) == 0 {
		return p
	}
	return p + "?" + q.Encode()
}

// without is q less key.
func without(q url.Values, key string) url.Values {
	out := url.Values{}
	for k, v := range q {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// chipValue is how a filter value reads: a month as "October 2026", an id
// as the rows' snapshot label, else the id itself (no row names it;
// CD-233).
func chipValue(key, v string, labels map[string]string) string {
	if key == "month" {
		return monthLabel(v)
	}
	if l, ok := labels[key]; ok {
		return l
	}
	return v
}

// monthLabel is "YYYY-MM" as "October 2026".
func monthLabel(m string) string {
	t, _ := time.Parse("2006-01", m) // the filter parse has read it
	return t.Format("January 2006")
}

// pageNumber is the request's page, 1 when absent or bad.
func pageNumber(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// pageInfo builds the pager; list is the report's URL up to its page
// parameter, ending in "?" or "&".
func pageInfo(list, noun string, page, total int) components.PageInfo {
	return components.PageInfo{
		Page: page, From: (page-1)*reportsPageSize + 1, To: min(page*reportsPageSize, total), Total: total, Noun: noun,
		NextURL: list + "page=" + strconv.Itoa(page+1), PrevURL: list + "page=" + strconv.Itoa(page-1), // shown only past page 1
	}
}
