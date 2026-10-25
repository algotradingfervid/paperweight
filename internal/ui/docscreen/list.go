package docscreen

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
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
	ctx := r.Context()
	dt, ok := doctype.Lookup(chi.URLParam(r, "doctype"))
	if !ok {
		problem(w, r, doctype.ErrNotFound)
		return
	}
	p := d.principal(r)
	if err := d.Auth.Can(ctx, p, dt.Name, "read", nil); err != nil {
		problem(w, r, err)
		return
	}
	q := ParseListQuery(r.URL.Query())
	res, err := d.Lister.List(ctx, p, dt, q)
	if err != nil {
		problem(w, r, err)
		return
	}
	cols := ListColumns(dt)
	rows := make([]components.Row, len(res.Rows))
	err = d.DB.Read(ctx, func(tx *db.Tx) error {
		for i, lr := range res.Rows {
			vals := map[string]string{}
			for _, c := range cols {
				f, ok := fieldOf(dt, c.Key)
				if !ok {
					continue
				}
				s, err := Display(ctx, tx, d.Engine, f, lr.Values[c.Key])
				if err != nil {
					return err
				}
				vals[c.Key] = s
			}
			rows[i] = components.Row{
				Href:   docURL(dt.Name, lr.Info.ID),
				Title:  lr.Info.Display,
				Party:  lr.Info.Party,
				Values: vals,
				State:  lr.Info.State,
			}
		}
		return nil
	})
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
			Sorts:       sortOptions(dt),
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
		v.List.Empty = components.EmptyState("No "+plural+" yet.", newLink)
	}
	if d.can(r, p, dt.Name, "export") {
		v.Bar.Export = &components.Link{Label: "Export", Href: "/doc/" + dt.Name + "/export.csv"}
	}
	ui.Render(w, r, http.StatusOK, listPage(v), listMain(v))
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
	ui.Render(w, r, pr.Status, problemPage(pr), problemMain(pr))
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

// sortOptions lists DefaultSort's keys, then each Sortable field both ways.
func sortOptions(dt *doctype.DocType) []components.Option {
	keys := slices.Clone(dt.DefaultSort)
	for _, f := range dt.Fields {
		if f.Sortable {
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
		out[i] = components.Chip{Label: label + strings.Join(f.Values, ", "), RemoveURL: listURL(dt.Name, rest)}
	}
	return out
}

// keep is what the filter bar carries through a search or sort: the
// filters and size, never page, so the result starts on page 1.
func keep(q ListQuery) []components.Param {
	var out []components.Param
	for _, f := range q.Filters {
		for _, val := range f.Values {
			out = append(out, components.Param{Name: "f", Value: f.Field + ":" + f.Op + ":" + val})
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
