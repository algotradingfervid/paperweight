package docscreen

import (
	"cmp"
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// defaultPageSize is the list page size when the URL has none (NUM-117).
const defaultPageSize = 25

// listView is everything the list screen renders.
type listView struct {
	Opts  layout.PageOpts
	Bar   components.FilterBarProps
	List  components.DataListProps
	Pages components.PageInfo
}

// list serves GET /doc/{doctype}.
func (d Deps) list(w http.ResponseWriter, r *http.Request) {
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	d.renderList(w, r, dt, "", nil)
}

// renderList renders dt's list for the request's query; success and
// warnings, when set, are an htmx action's banners (delete, CD-80).
func (d Deps) renderList(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, success string, warnings []string) {
	ctx := r.Context()
	p := d.principal(r)
	if err := d.Auth.Can(ctx, p, dt.Name, "read", nil); err != nil {
		problem(w, r, err)
		return
	}
	hide := d.ListHidden(r, dt)
	q := ListedQuery(dt, ParseListQuery(r.URL.Query()), hide)
	res, err := d.Lister.List(ctx, p, dt, q)
	if err != nil {
		problem(w, r, err)
		return
	}
	cols := listedColumns(dt, hide)
	rows, err := d.listRows(r.WithContext(auth.WithPrincipal(ctx, p)), dt, cols, res.Rows, docURL)
	if err != nil {
		problem(w, r, err)
		return
	}

	plural := strings.ToLower(dt.PluralLabel)
	var newLink *components.Link
	if d.can(r, p, dt.Name, "create") {
		newLink = &components.Link{Label: "New " + strings.ToLower(dt.Label), Href: "/doc/" + dt.Name + "/new"}
	}
	v := listView{
		Opts: layout.PageOpts{Title: dt.PluralLabel},
		Bar: components.FilterBarProps{
			Action:      "/doc/" + dt.Name,
			Search:      q.Search,
			SearchLabel: "Search " + plural,
			Sort:        selectedSort(dt, q),
			Sorts:       sortOptions(dt, hide),
			Chips:       chips(dt, q),
			ClearURL:    listURL(dt.Name, ListQuery{Search: q.Search, Sort: q.Sort, Size: q.Size}),
			Count:       countLine(dt, res.Total),
			New:         newLink,
			Keep:        keep(q),
		},
		List: components.DataListProps{
			Label:   dt.PluralLabel,
			Columns: cols,
			Rows:    rows,
		},
		Pages: pageInfo(dt.Name, q, res.Total, plural),
	}
	if res.Total == 0 {
		v.List.Empty = emptyState(dt, q, plural)
	}
	if d.can(r, p, dt.Name, "export") {
		v.Bar.Export = &components.Link{Label: "Export", Href: "/doc/" + dt.Name + "/export.csv"}
	}
	if success != "" || len(warnings) > 0 {
		v.Opts.BannerSlot = actionBanners(success, warnings)
	}
	ui.Render(w, r, http.StatusOK, listPage(v), listMain(v))
}

// emptyState is the list's empty state (CD-366): "yet" and the plain line
// with no search or filter, else what didn't match and Clear filters, which
// keeps the sort and size. q is the listed query, so a filter on a hidden
// field is already gone; filter values are never repeated.
func emptyState(dt *doctype.DocType, q ListQuery, plural string) templ.Component {
	clearURL := listURL(dt.Name, ListQuery{Sort: q.Sort, Size: q.Size})
	switch {
	case q.Search != "":
		return components.NoMatch("No "+plural+" match \""+q.Search+"\"", "Try fewer words, or clear the filters.", clearURL)
	case len(q.Filters) > 0:
		return components.NoMatch("No "+plural+" match these filters", "Try fewer filters, or clear them.", clearURL)
	}
	first := min(1, len(plural)) // a DocType may leave PluralLabel blank
	return components.EmptyList("No "+plural+" yet", strings.ToUpper(plural[:first])+plural[first:]+" you add show up here.")
}

// ListHidden is the Hide question of the list, search and CSV export:
// whether name is a field of dt the module hides, asked with Listing set and
// no View (CD-327 (1), (3), CD-334).
func (d Deps) ListHidden(r *http.Request, dt *doctype.DocType) func(name string) bool {
	c := d.ctxOf(r, dt, nil)
	c.Listing = true
	return func(name string) bool {
		f, ok := fieldOf(dt, name)
		return ok && hidden(c, f)
	}
}

// ListedQuery is q without the filters and the sort on a field hide reports
// hidden; with no sort left and dt's first DefaultSort key hidden, it sorts
// by the first sort option left on a Sortable field (CD-334 (2), (3)).
func ListedQuery(dt *doctype.DocType, q ListQuery, hide func(string) bool) ListQuery {
	q.Filters = slices.DeleteFunc(slices.Clone(q.Filters), func(f Filter) bool { return hide(f.Field) })
	if field, _, _ := strings.Cut(q.Sort, "."); q.Sort != "" && hide(field) {
		q.Sort = ""
	}
	if q.Sort == "" && len(dt.DefaultSort) != 0 && hide(dt.DefaultSort[0].Field) {
		for _, o := range sortOptions(dt, hide) {
			field, _, _ := strings.Cut(o.Value, ".")
			if f, ok := fieldOf(dt, field); ok && f.Sortable {
				q.Sort = o.Value
				break
			}
		}
	}
	return q
}

// listedColumns is ListColumns(dt) without the columns hide reports hidden
// (CD-327 (3)).
func listedColumns(dt *doctype.DocType, hide func(string) bool) []components.Column {
	return slices.DeleteFunc(ListColumns(dt), func(col components.Column) bool { return hide(col.Key) })
}

// PartyListed reports whether p's list of name shows its party: false when
// name's module hides the party field, asked as the list page asks, with
// Listing set and no View (CD-327 (3), CD-334). The app gives it to the
// approval readers as approval.Deps.PartyVisible. An unknown name keeps the
// party.
func (d Deps) PartyListed(ctx context.Context, p auth.Principal, name string) bool {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return true
	}
	r := (&http.Request{Method: http.MethodGet, URL: &url.URL{Path: d.InboxURL}}).WithContext(auth.WithPrincipal(ctx, p))
	return !partyHidden(Ctx{Req: r, Principal: p, DT: dt, Deps: d, Listing: true})
}

// listRows is rs as DataList rows over cols; href is each row's link. The
// party is left out when the module hides its field (CD-319 (7)); Hide is
// asked with no View and Listing set (CD-327 (1)). PartyKey names the party
// field's column, which cards skip (CD-371).
func (d Deps) listRows(r *http.Request, dt *doctype.DocType, cols []components.Column, rs []ListRow, href func(string, int64) string) ([]components.Row, error) {
	ctx := r.Context()
	c := d.ctxOf(r, dt, nil)
	c.Listing = true
	noParty := partyHidden(c)
	partyField, _ := doc.PartyField(dt)
	rows := make([]components.Row, len(rs))
	err := d.DB.Read(ctx, func(tx *db.Tx) error {
		for i, lr := range rs {
			vals := map[string]string{}
			for _, c := range cols {
				if c.Key == stateKey { // never the raw state (CD-325)
					vals[c.Key] = cmp.Or(doctype.StateLabel(*dt, lr.Info.State), string(lr.Info.State))
					continue
				}
				f, ok := fieldOf(dt, c.Key)
				if !ok {
					continue
				}
				s, err := d.display(ctx, tx, f, lr.Values[c.Key])
				if err != nil {
					return err
				}
				vals[c.Key] = s
			}
			rows[i] = components.Row{
				Href:       href(dt.Name, lr.Info.ID),
				Title:      lr.Info.Display,
				Values:     vals,
				State:      lr.Info.State,
				StateLabel: dt.StateLabels[lr.Info.State],
			}
			if !noParty {
				rows[i].Party = lr.Info.Party
				rows[i].PartyKey = partyField.Name
			}
		}
		return nil
	})
	return rows, err
}

func (d Deps) can(r *http.Request, p auth.Principal, name string, right auth.Right) bool {
	return d.Auth.Can(r.Context(), p, name, right, nil) == nil
}

// problem renders err as a problem page when Classify knows it, else
// leaves it to web.ErrorFrom (db.ErrBusy is 503, CD-76, CD-81).
func problem(w http.ResponseWriter, r *http.Request, err error) {
	pr, ok := Classify(err)
	if !ok {
		web.ErrorFrom(w, r, err)
		return
	}
	if r.Method == http.MethodGet && pr.Status == http.StatusNotFound {
		// A missing record reads the same as a hidden one; DOC's "no longer
		// exists" stays for a save of a deleted draft (CD-245, AUTH-108).
		pr.Message = doctype.ErrNotFound.Error()
	}
	notice(w, r, pr)
	ui.Render(w, r, pr.Status, problemPage(pr), problemMain(pr))
}

// notice sends an htmx write's 4xx refusal whose status htmx doesn't swap
// (anything but 409 and 422, layout's htmxConfig) also as an alert notice
// with its own message, so the user sees why (CD-348). A 5xx gets none: the
// network banner answers it (CD-346). It sets a header, so it goes before
// the response starts.
func notice(w http.ResponseWriter, r *http.Request, pr Problem) {
	if ui.IsHTMX(r) && r.Method == http.MethodPost && pr.Status < http.StatusInternalServerError && pr.Status != http.StatusConflict && pr.Status != http.StatusUnprocessableEntity {
		web.SetNotice(w.Header(), "error", pr.Message)
	}
}

func problemOpts(pr Problem) layout.PageOpts {
	return layout.PageOpts{Title: pr.Message, Error: true}
}

func docURL(name string, id int64) string {
	return "/doc/" + name + "/" + strconv.FormatInt(id, 10)
}

func listURL(name string, q ListQuery) string {
	if s := q.Encode(); s != "" {
		return "/doc/" + name + "?" + s
	}
	return "/doc/" + name
}

func fieldOf(dt *doctype.DocType, name string) (doctype.Field, bool) {
	i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == name })
	if i < 0 {
		return doctype.Field{}, false
	}
	return dt.Fields[i], true
}

func fieldLabel(dt *doctype.DocType, name string) string {
	if f, ok := fieldOf(dt, name); ok {
		return f.Label
	}
	if name == stateKey {
		return "State"
	}
	return name
}

func sortValue(k doctype.SortKey) string {
	if k.Desc {
		return k.Field + ".desc"
	}
	return k.Field + ".asc"
}

func sortLabel(dt *doctype.DocType, k doctype.SortKey) string {
	if k.Desc {
		return fieldLabel(dt, k.Field) + " high to low"
	}
	return fieldLabel(dt, k.Field) + " low to high"
}

// sortOptions lists DefaultSort's keys, then each Sortable field both ways,
// leaving out the fields hide reports hidden (CD-334 (2)).
func sortOptions(dt *doctype.DocType, hide func(string) bool) []components.Option {
	keys := slices.DeleteFunc(slices.Clone(dt.DefaultSort), func(k doctype.SortKey) bool { return hide(k.Field) })
	for _, f := range dt.Fields {
		if f.Sortable && !hide(f.Name) {
			keys = append(keys, doctype.SortKey{Field: f.Name, Desc: true}, doctype.SortKey{Field: f.Name})
		}
	}
	var opts []components.Option
	for _, k := range keys {
		o := components.Option{Value: sortValue(k), Label: sortLabel(dt, k)}
		if !slices.ContainsFunc(opts, func(x components.Option) bool { return x.Value == o.Value }) {
			opts = append(opts, o)
		}
	}
	return opts
}

func selectedSort(dt *doctype.DocType, q ListQuery) string {
	if q.Sort == "" && len(dt.DefaultSort) > 0 {
		return sortValue(dt.DefaultSort[0])
	}
	return q.Sort
}

// chips shows each filter; its remove URL is the list without it, on page 1.
func chips(dt *doctype.DocType, q ListQuery) []components.Chip {
	out := make([]components.Chip, len(q.Filters))
	for i, f := range q.Filters {
		label := fieldLabel(dt, f.Field) + ": "
		if f.Op != "=" {
			label += f.Op + " "
		}
		rest := q
		rest.Page = 0
		rest.Filters = slices.Concat(q.Filters[:i], q.Filters[i+1:])
		values := make([]string, len(f.Values))
		for n, v := range f.Values {
			values[n] = filterValueLabel(dt, f.Field, v)
		}
		out[i] = components.Chip{Label: label + strings.Join(values, ", "), RemoveURL: listURL(dt.Name, rest)}
	}
	return out
}

// filterValueLabel is a filter value as users see it: a state's
// doctype.StateLabel, a Select option's label, else the value (CD-325).
func filterValueLabel(dt *doctype.DocType, field, v string) string {
	if field == stateKey {
		return cmp.Or(doctype.StateLabel(*dt, doctype.State(v)), v)
	}
	if f, ok := fieldOf(dt, field); ok && f.Type == doctype.Select {
		return optionLabel(f, v)
	}
	return v
}

// keep is what the filter bar carries through a search or sort: the
// filters and size, never page, so the result starts on page 1.
func keep(q ListQuery) []components.Param {
	var out []components.Param
	for _, f := range q.Filters {
		for _, val := range f.Values {
			out = append(out, components.Param{Name: "f", Value: f.param(val)})
		}
	}
	if q.Size > 0 {
		out = append(out, components.Param{Name: "size", Value: strconv.Itoa(q.Size)})
	}
	return out
}

func countLine(dt *doctype.DocType, total int) string {
	switch total {
	case 0:
		return ""
	case 1:
		return "1 " + strings.ToLower(dt.Label)
	}
	return types.FormatCount(int64(total)) + " " + strings.ToLower(dt.PluralLabel)
}

func pageInfo(name string, q ListQuery, total int, noun string) components.PageInfo {
	size := q.Size
	if size <= 0 {
		size = defaultPageSize
	}
	page := max(q.Page, 1)
	pi := components.PageInfo{
		Page:  page,
		From:  (page-1)*size + 1,
		To:    min(page*size, total),
		Total: total,
		Noun:  noun,
	}
	prev, next := q, q
	prev.Page, next.Page = page-1, page+1
	pi.PrevURL, pi.NextURL = listURL(name, prev), listURL(name, next)
	return pi
}
