package purchaseweb

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

const pendingBase = "/reports/purchase/pending"

// pendingKey is one id filter of the report: its query key, label, the
// master its Picker searches and the role that narrows it.
type pendingKey struct{ key, label, docType, role string }

var pendingKeys = []pendingKey{
	{"vendor", "Vendor", party.DocType, "vendor"},
	{"project", "Project", project.DocType, ""},
	{"site", "Site", site.DocType, ""},
	{"item", "Item", item.DocType, ""},
}

// pendingColumns are the report's DataList columns (UX §5.19); the CSV has
// all 15 of spec §13.
var pendingColumns = []components.Column{
	{Key: "po", Label: "PO number", Priority: components.PTitle, Code: true, NoWrap: true},
	{Key: "value", Label: "Pending value", Priority: components.PFigure, Num: true},
	{Key: "item", Label: "Item", Priority: components.PMeta},
	{Key: "qty", Label: "Pending and received", Priority: components.PMeta},
	{Key: "vendor", Label: "Vendor", Priority: components.PMeta},
	{Key: "due", Label: "Due", Priority: components.PMeta},
	{Key: "place", Label: "Project and site", Priority: components.PLow, Code: true, NoWrap: true},
	{Key: "order", Label: "Order date and net rate", Priority: components.PLow},
}

// dueText is `Due {date}`, with `(expected)` when the date is Expected on
// and `· Overdue` when past (spec §27.1); "" with neither date.
func dueText(r purchase.PendingRow) string {
	d, s := r.RequiredBy, ""
	if r.ExpectedOn != (types.Date{}) {
		d, s = r.ExpectedOn, " (expected)"
	}
	if d == (types.Date{}) {
		return ""
	}
	s = "Due " + types.FormatDate(d) + s
	if r.Overdue {
		s += metaSep + "Overdue"
	}
	return s
}

// pendingRow is r as a DataList row, linking to its PO.
func pendingRow(r purchase.PendingRow) components.Row {
	var ordered string
	if r.OrderDate != (types.Date{}) {
		ordered = "Ordered " + types.FormatDate(r.OrderDate)
	}
	return components.Row{Href: poHref(r.POID), Values: map[string]string{
		"po": r.PONumber, "value": types.FormatINR(r.PendingValue), "item": summaryLine(r.ItemName, r.ItemCode),
		"qty":    "Pending " + types.FormatQtyUoM(r.Pending, r.UoM) + " of " + types.FormatQty(r.Ordered) + metaSep + "Received " + types.FormatQty(r.Received),
		"vendor": r.Vendor, "due": dueText(r), "place": summaryLine(r.Project, r.Site),
		"order": summaryLine(ordered, "Net rate "+types.FormatRate(r.NetRate)),
	}}
}

// pendingLabels are the chip labels of the active id filters, from the
// rows' snapshots; a filter with no row reads its id (CD-233).
func pendingLabels(rows []purchase.PendingRow) map[string]string {
	labels := map[string]string{}
	for _, r := range rows {
		labels["vendor"], labels["project"], labels["site"], labels["item"] = r.Vendor, r.Project, r.Site, r.ItemCode
	}
	return labels
}

// activePending is q's non-blank filters, overdue among them.
func activePending(q url.Values) url.Values {
	out := url.Values{}
	for _, f := range pendingKeys {
		if v := q.Get(f.key); v != "" {
			out.Set(f.key, v)
		}
	}
	if q.Get("overdue") == "1" {
		out.Set("overdue", "1")
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

// pendingView is the report page.
type pendingView struct {
	bar   components.FilterBarProps
	list  components.DataListProps
	pages components.PageInfo
}

// pendingReport answers GET /reports/purchase/pending (UX §5.19): one page
// of the rows p may read that match the query, the filter bar (Export only
// with export, CD-145) and the pager. A bad id is AUTH's bad form, no read
// the service's 403.
func (h *handlers) pendingReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	f, err := pendingFilterOf(r.URL.Query())
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	page := pageNumber(r)
	rep, err := h.d.Service.PendingReport(ctx, p, f, page)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	q := activePending(r.URL.Query())
	labels := pendingLabels(rep.Rows)
	v := pendingView{
		bar: components.FilterBarProps{
			Action: pendingBase, FilterURL: withQuery(pendingBase+"/filter", q), ClearURL: pendingBase,
		},
		list: components.DataListProps{Label: pendingTitle, Columns: pendingColumns, Empty: components.EmptyState(msgPendingNone, nil)},
	}
	if rep.Total > 0 {
		v.bar.Count = counted(rep.Total, "pending line", "pending lines")
	}
	for _, pf := range pendingKeys {
		if id := q.Get(pf.key); id != "" {
			label, ok := labels[pf.key]
			if !ok {
				label = id
			}
			v.bar.Chips = append(v.bar.Chips, components.Chip{Label: pf.label + ": " + label, RemoveURL: withQuery(pendingBase, without(q, pf.key))})
		}
	}
	if f.OverdueOnly {
		v.bar.Chips = append(v.bar.Chips, components.Chip{Label: overdueOnly, RemoveURL: withQuery(pendingBase, without(q, "overdue"))})
	} else {
		over := without(q, "overdue")
		over.Set("overdue", "1")
		v.bar.Links = []components.Link{{Label: overdueOnly, Href: withQuery(pendingBase, over)}}
	}
	if len(q) > 0 {
		v.list.Empty = components.NoMatch(msgPendingMiss, "", pendingBase)
	}
	if ok, err := h.can(ctx, p, purchase.DocType, auth.RightExport); err != nil {
		web.ErrorFrom(w, r, err)
		return
	} else if ok {
		v.bar.Export = &components.Link{Label: "Export", Href: withQuery(pendingBase+"/export.csv", q)}
	}
	for _, row := range rep.Rows {
		v.list.Rows = append(v.list.Rows, pendingRow(row))
	}
	list := pendingBase + "?"
	if len(q) > 0 {
		list = withQuery(pendingBase, q) + "&"
	}
	v.pages = pageInfo(list, "pending lines", page, purchase.PendingPageSize, rep.Total)
	ui.Render(w, r, http.StatusOK, pendingPage(v), nil)
}

// pendingFilterView is the report's filter page: a GET form back to it.
type pendingFilterView struct {
	back    string
	overdue bool
	fields  []templ.Component
}

// pendingFilter answers GET /reports/purchase/pending/filter (UX §5.19):
// a Picker for each id filter whose master p may read (so its search
// answers), the query's choice chosen and overdue kept.
func (h *handlers) pendingFilter(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	query := r.URL.Query()
	f, err := pendingFilterOf(query)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	rep, err := h.d.Service.PendingReport(ctx, p, f, 1)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	q := activePending(query)
	labels := pendingLabels(rep.Rows)
	v := pendingFilterView{back: withQuery(pendingBase, q), overdue: f.OverdueOnly}
	for _, pf := range pendingKeys {
		ok, err := h.can(ctx, p, pf.docType, auth.RightRead)
		if err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
		if !ok {
			continue
		}
		search := "/pick/" + pf.docType
		if pf.role != "" {
			search += "?role=" + pf.role
		}
		pp := components.PickerProps{
			Field:     components.FieldProps{ID: "f-" + pf.key, Label: pf.label, Clearable: true},
			SearchURL: search, HiddenName: pf.key, ListboxID: "f-" + pf.key + "-list",
		}
		if id, err := strconv.ParseInt(q.Get(pf.key), 10, 64); err == nil {
			label, ok := labels[pf.key]
			if !ok {
				label = q.Get(pf.key)
			}
			pp.Chosen = &components.PickRow{ID: id, Name: label}
		}
		v.fields = append(v.fields, components.Picker(pp))
	}
	ui.Render(w, r, http.StatusOK, pendingFilterScreen(v), nil)
}

// pendingFilterOpts is the filter page's frame, with Back to the report.
func pendingFilterOpts(back string) layout.PageOpts {
	return layout.PageOpts{Title: "Filter pending items", Width: layout.Form, Back: &components.Link{Label: pendingTitle, Href: back}}
}
