package expenseweb

import (
	"context"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// reportFilter is one filter of a report: its query key and label.
type reportFilter struct{ key, label string }

// reportSpec is one report page's words, DocType and filters (UX §5.13 to
// §5.15). none is the empty state with no filter, noMatch with one.
type reportSpec struct {
	title, one, many, none, noMatch, docType string
	filters                                  []reportFilter
}

var byEmployee = reportFilter{"employee", "Employee"}

var reportSpecs = map[string]reportSpec{
	"open": {
		title: "Open claims", one: "claim", many: "claims", docType: expense.ClaimDocType,
		none: "No approved claims are waiting to be closed.", noMatch: "No approved claims are waiting to be closed.",
		filters: []reportFilter{byEmployee},
	},
	"lines": {
		title: "Expense lines", one: "line", many: "lines", docType: expense.ClaimDocType,
		none: "No expense lines yet.", noMatch: "No expense lines match these filters.",
		filters: []reportFilter{{"month", "Month"}, {"type", "Type"}, {"project", "Project"}, byEmployee},
	},
	"advances": {
		title: "Open advances", one: "advance", many: "advances", docType: expense.AdvanceDocType,
		none: "No open advances.", noMatch: "No open advances.",
		filters: []reportFilter{byEmployee},
	},
}

// The DataList columns of each report (UX §5.13 to §5.15). NoWrap (at most
// 3): record numbers, GSTINs, project and site codes and a bare date; never
// a cell that also holds free text (a name or purpose could widen the track).
var (
	openColumns = []components.Column{
		{Key: "claim", Label: "Claim", Priority: components.PTitle, Code: true, NoWrap: true},
		{Key: "to_pay", Label: "To pay", Priority: components.PFigure, Num: true},
		{Key: "employee", Label: "Employee", Priority: components.PMeta},
		{Key: "purpose", Label: "Purpose", Priority: components.PMeta},
		{Key: "approved", Label: "Approved", Priority: components.PMeta},
		{Key: "no_receipt", Label: "Without a receipt", Priority: components.PMeta},
		{Key: "totals", Label: "Total and less advance", Priority: components.PLow},
	}
	linesColumns = []components.Column{
		{Key: "line", Label: "Claim and line", Priority: components.PTitle, Code: true, NoWrap: true},
		{Key: "amount", Label: "Amount", Priority: components.PFigure, Num: true},
		{Key: "type_date", Label: "Type and date", Priority: components.PMeta},
		{Key: "employee", Label: "Employee", Priority: components.PMeta},
		{Key: "receipt", Label: "Receipt", Priority: components.PMeta},
		{Key: "place", Label: "Project and site", Priority: components.PLow, Code: true, NoWrap: true},
		{Key: "gst", Label: "GST details", Priority: components.PLow}, // holds the invoice no.: free text
		{Key: "state", Label: "State", Priority: components.PState},
	}
	advancesColumns = []components.Column{
		{Key: "advance", Label: "Advance", Priority: components.PTitle, Code: true, NoWrap: true},
		{Key: "left", Label: "Left", Priority: components.PFigure, Num: true},
		{Key: "employee", Label: "Employee", Priority: components.PMeta},
		{Key: "date_purpose", Label: "Date and purpose", Priority: components.PMeta},
		{Key: "paid", Label: "Paid on", Priority: components.PMeta, NoWrap: true},
		{Key: "amounts", Label: "Amount and used", Priority: components.PLow},
		{Key: "age", Label: "Age", Priority: components.PMeta},
	}
)

// reportData is a report's rows for the request, and each filter's chip
// value by key, from the rows' snapshots.
type reportData struct {
	columns []components.Column
	rows    []components.Row
	labels  map[string]string
}

// reportView is a report page.
type reportView struct {
	opts layout.PageOpts
	bar  components.FilterBarProps
	list components.DataListProps
}

// filterView is a report's filter page: a GET form back to the report.
type filterView struct {
	opts   layout.PageOpts
	action string
	fields []templ.Component
}

// report answers GET /reports/expense/{open,lines,advances}. A bad filter is
// list's 422 and a user without read AUTH's 403 (the service's Scope).
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
	var c *expense.LinesChoices
	if q.Has("type") {
		lc, err := h.d.Service.LinesChoices(ctx, p)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		c = &lc
	}
	if err := h.name(ctx, p, q, &data, c); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := reportView{
		opts: layout.PageOpts{Title: spec.title},
		bar: components.FilterBarProps{
			Action: base, FilterURL: withQuery(base+"/filter", q), ClearURL: base,
			Chips: chips(spec, base, q, data.labels), Count: countText(spec, len(data.rows)),
		},
		list: components.DataListProps{Label: spec.title, Columns: data.columns, Rows: data.rows},
	}
	if len(q) == 0 {
		v.list.Empty = components.EmptyState(spec.none, nil)
	} else {
		v.list.Empty = components.EmptyState(spec.noMatch, &components.Link{Label: "Clear all", Href: base})
	}
	if h.d.Auth.Can(ctx, p, spec.docType, auth.RightExport, nil) == nil {
		v.bar.Export = &components.Link{Label: "Export", Href: withQuery(base+"/export.csv", q)}
	}
	ui.Render(w, r, http.StatusOK, reportPage(v), reportMain(v))
}

// filterPage answers GET /reports/expense/{report}/filter with the
// report's filters set to the request's values.
func (h *handlers) filterPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	name := path.Base(path.Dir(r.URL.Path))
	spec, base := reportSpecs[name], path.Dir(r.URL.Path)
	query := r.URL.Query()
	data, err := h.load(ctx, p, name, query)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	q := activeFilters(spec, query)
	v := filterView{
		opts:   layout.PageOpts{Title: "Filter " + strings.ToLower(spec.title), Width: layout.Form, Back: &components.Link{Label: spec.title, Href: withQuery(base, q)}},
		action: base,
	}
	var c *expense.LinesChoices
	if name == "lines" {
		lc, err := h.d.Service.LinesChoices(ctx, p)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		c = &lc
	}
	if err := h.name(ctx, p, q, &data, c); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	if c != nil {
		months := []components.Option{{Value: "", Label: "Any month"}}
		for _, m := range c.Months {
			months = append(months, components.Option{Value: m, Label: monthLabel(m)})
		}
		v.fields = append(v.fields, components.Select(components.FieldProps{ID: "f-month", Name: "month", Label: "Month", Value: q.Get("month"), Clearable: true}, months))
		if c.Types != nil {
			kinds := []components.Option{{Value: "", Label: "Any type"}}
			for _, t := range c.Types {
				kinds = append(kinds, components.Option{Value: strconv.FormatInt(t.ID, 10), Label: t.Name})
			}
			v.fields = append(v.fields, components.Select(components.FieldProps{ID: "f-type", Name: "type", Label: "Type", Value: q.Get("type"), Clearable: true}, kinds))
		}
		if h.canRead(ctx, p, project.DocType) {
			v.fields = append(v.fields, picker("project", "Project", project.DocType, q, data.labels))
		}
	}
	if h.canRead(ctx, p, employee.DocType) {
		v.fields = append(v.fields, picker("employee", "Employee", employee.DocType, q, data.labels))
	}
	ui.Render(w, r, http.StatusOK, filterScreen(v), nil)
}

// canRead reports whether p may read master dt, so its Picker's search
// (/pick) answers.
func (h *handlers) canRead(ctx context.Context, p auth.Principal, dt string) bool {
	return h.d.Auth.Can(ctx, p, dt, auth.RightRead, nil) == nil
}

// filterMasters are the master behind each id filter key.
var filterMasters = map[string]string{"employee": employee.DocType, "project": project.DocType}

// name sets d's labels for q's active id filters from their records: a type
// from c's choices, an employee (its name) or a project (its code) from the
// master when p may read that row. A record p can't read keeps the rows'
// snapshot, and with no row the raw id (CD-233).
func (h *handlers) name(ctx context.Context, p auth.Principal, q url.Values, d *reportData, c *expense.LinesChoices) error {
	if c != nil {
		for _, t := range c.Types {
			if strconv.FormatInt(t.ID, 10) == q.Get("type") {
				d.labels["type"] = t.Name
			}
		}
	}
	for key, dt := range filterMasters {
		id, err := strconv.ParseInt(q.Get(key), 10, 64)
		if err != nil {
			continue // not set: load has refused a bad id
		}
		info, ok, err := h.d.Service.MasterLabel(ctx, p, dt, id)
		switch {
		case err != nil:
			return err
		case ok && key == "project":
			d.labels[key] = info.Code
		case ok:
			d.labels[key] = info.Name
		}
	}
	return nil
}

// picker is the filter key's master Picker, its chosen row labelled as the
// chip is.
func picker(key, label, docType string, q url.Values, labels map[string]string) templ.Component {
	pp := components.PickerProps{
		Field:     components.FieldProps{ID: "f-" + key, Label: label, Clearable: true},
		SearchURL: "/pick/" + docType, HiddenName: key, ListboxID: "f-" + key + "-list",
	}
	if id, err := strconv.ParseInt(q.Get(key), 10, 64); err == nil {
		pp.Chosen = &components.PickRow{ID: id, Name: chipValue(key, q.Get(key), labels)}
	}
	return components.Picker(pp)
}

// load reads report's rows for q through the service (its scope), after
// reading q's filters as the CSV does.
func (h *handlers) load(ctx context.Context, p auth.Principal, report string, q url.Values) (reportData, error) {
	f := filters{q: q}
	d := reportData{labels: map[string]string{}}
	switch report {
	case "open":
		of := expense.OpenClaimsFilter{EmployeeID: f.id("employee", "Employee")}
		if f.err != nil {
			return d, f.err
		}
		rows, err := h.d.Service.OpenClaimsReport(ctx, p, of)
		d.columns = openColumns
		for _, r := range rows {
			d.labels["employee"] = r.Employee
			d.rows = append(d.rows, components.Row{Href: docURL(expense.ClaimDocType, r.ID), Values: map[string]string{
				"claim": r.Claim, "to_pay": types.FormatINR(r.ToPay), "employee": r.Employee, "purpose": r.Purpose,
				"approved":   approvedText(r),
				"totals":     "Total " + types.FormatINR(r.Total) + " · Less advance " + types.FormatINR(r.LessAdvance),
				"no_receipt": noReceiptText(r.NoReceipt),
			}})
		}
		return d, err
	case "lines":
		lf := expense.LinesFilter{
			Month: f.month(), TypeID: f.id("type", "Type"), ProjectID: f.id("project", "Project"), EmployeeID: f.id("employee", "Employee"),
		}
		if f.err != nil {
			return d, f.err
		}
		rep, err := h.d.Service.ExpenseLinesReport(ctx, p, lf)
		d.columns = linesColumns
		claimType, _ := doctype.Lookup(expense.ClaimDocType)
		for _, r := range rep.Rows {
			d.labels["employee"], d.labels["project"], d.labels["type"] = r.Employee, r.Project, r.Type
			d.rows = append(d.rows, components.Row{
				Href: docURL(expense.ClaimDocType, r.ClaimID), State: r.State, StateLabel: claimType.StateLabels[r.State],
				Values: map[string]string{
					"line": r.Claim + " · line " + strconv.Itoa(r.LineNo), "amount": types.FormatINR(r.Amount),
					"type_date": r.Type + " · " + types.FormatDate(r.Date), "employee": r.Employee,
					"receipt": "Receipt: " + yesNo(r.Receipt), "place": joinSet(r.Project, r.Site), "gst": gstText(r),
				},
			})
		}
		return d, err
	}
	af := expense.AdvancesFilter{EmployeeID: f.id("employee", "Employee")}
	if f.err != nil {
		return d, f.err
	}
	rows, err := h.d.Service.AdvancesReport(ctx, p, af)
	d.columns = advancesColumns
	for _, r := range rows {
		d.labels["employee"] = r.Employee
		paid := ""
		if r.PaidOn != nil {
			paid = "Paid on " + types.FormatDate(*r.PaidOn)
		}
		d.rows = append(d.rows, components.Row{Href: docURL(expense.AdvanceDocType, r.ID), Values: map[string]string{
			"advance": r.Advance, "left": types.FormatINR(r.Left), "employee": r.Employee,
			"date_purpose": types.FormatDate(r.Date) + " · " + r.Purpose, "paid": paid,
			"amounts": "Amount " + types.FormatINR(r.Amount) + " · Used " + types.FormatINR(r.Used),
			"age":     plural(r.AgeDays, "day", "days"),
		}})
	}
	return d, err
}

func docURL(docType string, id int64) string {
	return "/doc/" + docType + "/" + strconv.FormatInt(id, 10)
}

// approvedText is "Approved {date} by {name}", blank with no approval
// history.
func approvedText(r expense.OpenClaimRow) string {
	if r.ApprovedOn == (types.Date{}) {
		return ""
	}
	return "Approved " + types.FormatDate(r.ApprovedOn) + " by " + r.ApprovedBy
}

// noReceiptText is "{n} lines without a receipt", blank for none.
func noReceiptText(n int) string {
	if n == 0 {
		return ""
	}
	return plural(n, "line", "lines") + " without a receipt"
}

// gstText is a company bill's GST details: GST, supplier GSTIN, invoice and
// "Billed to {GSTIN}" from the line's snapshot (CD-271 G4); blank for any
// other line.
func gstText(r expense.LineRow) string {
	if r.SupplierGSTIN == "" && r.BilledToGSTIN == "" {
		return ""
	}
	billed := ""
	if r.BilledToGSTIN != "" {
		billed = "Billed to " + r.BilledToGSTIN
	}
	return joinSet("GST "+types.FormatINR(r.GST), r.SupplierGSTIN, r.InvoiceNo, billed)
}

// joinSet joins the non-blank parts with " · ".
func joinSet(parts ...string) string {
	var set []string
	for _, s := range parts {
		if s != "" {
			set = append(set, s)
		}
	}
	return strings.Join(set, " · ")
}

func yesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// plural is "1 {one}" or "{n} {many}".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return types.FormatCount(int64(n)) + " " + many
}

// countText is "14 claims", blank with no rows.
func countText(spec reportSpec, n int) string {
	if n == 0 {
		return ""
	}
	return plural(n, spec.one, spec.many)
}

// activeFilters is q's non-blank values of spec's filters.
func activeFilters(spec reportSpec, q url.Values) url.Values {
	out := url.Values{}
	for _, f := range spec.filters {
		if v := strings.TrimSpace(q.Get(f.key)); v != "" {
			out.Set(f.key, v)
		}
	}
	return out
}

func withQuery(p string, q url.Values) string {
	if len(q) == 0 {
		return p
	}
	return p + "?" + q.Encode()
}

// chips shows each active filter; its remove URL is the report without it.
func chips(spec reportSpec, base string, q url.Values, labels map[string]string) []components.Chip {
	var out []components.Chip
	for _, f := range spec.filters {
		if !q.Has(f.key) {
			continue
		}
		rest := url.Values{}
		for k, v := range q {
			if k != f.key {
				rest[k] = v
			}
		}
		out = append(out, components.Chip{Label: f.label + ": " + chipValue(f.key, q.Get(f.key), labels), RemoveURL: withQuery(base, rest)})
	}
	return out
}

// chipValue is how a filter value reads: a month as "October 2026", an id
// as the rows' snapshot label, else the id itself (no row names it; CD-233).
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
	t, _ := time.Parse("2006-01", m) // load has read it
	return t.Format("January 2006")
}
