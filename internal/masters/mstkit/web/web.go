// Package mstweb is the screens every master shares (MST Task 22, CD-89): the
// list, the new and record pages, disable, enable, delete (after a confirm
// step) and the picker search; for a master with steps, the Master-mode create
// wizard (wizard.go) and the per-section edit (sections.go). A master's web/ subpackage
// fills a Screen with its service, its copy and a few small functions; this
// package does the rest. It renders only internal/ui components and adds no
// class of its own; the form is built from the DocType's fields by
// docscreen.FieldInput. Tailwind scans masters' .templ files (app.css
// @source), so keep daisyUI words out of their comments and text.
package mstweb

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// pageSize is the list page size (list.PageSizes[0]); the picker asks for
// the same and PickerResults shows the first 20.
const pageSize = 25

// rowsButton is the name of the rows editor's Add and Remove buttons.
const rowsButton = "_rows"

// noMatch is the empty list while a search is on: Copy.Empty's "yet" would be
// untrue.
const noMatch = "No results. Try a different search."

// idPattern is a record's URL segment. Ids start at 1, so /0 and /007 match
// no route.
const idPattern = "/{id:[1-9][0-9]*}"

// Service is a master's standard surface (WP2 plan, "Standard master
// surface"): R is its record type, In its input.
type Service[R, In any] interface {
	Create(ctx context.Context, p auth.Principal, in In) (mstkit.Saved, error)
	Update(ctx context.Context, p auth.Principal, id int64, version int, in In) (mstkit.Saved, error)
	SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error
	Delete(ctx context.Context, p auth.Principal, id int64, version int) error
	Get(ctx context.Context, tx *db.Tx, id int64) (R, error)
}

// Copy is a master's text, in sentence case (design-system §9).
type Copy struct {
	Plural string // list title and Back link: "Tax Codes"
	Noun   string // the plural inside a sentence: "tax codes", "UoMs"; required
	One    string // the singular inside a sentence: "tax code", "UoM"; required
	New    string // "New tax code"
	Search string // "Search tax codes"
	Empty  string // "No tax codes yet."
	// Success banners after each write: "Tax code saved."
	Saved, Disabled, Enabled, Deleted string
	// Rows and AddRow are the rows editor's legend and Add button: "Unit
	// conversions", "Add conversion"; required when the DocType has OnLine
	// fields.
	Rows, AddRow string
}

// Head is what the screens show of a stored record besides its fields.
type Head struct {
	Code, Name string // the title is "{Code} {Name}" (CD-68); Code "" for Address and Contact
	Disabled   bool
	Version    int
}

// Screen is one master's screens under Base.
type Screen[R, In any] struct {
	DocType string // registry name; its fields make the form and the list
	Base    string // "/m/taxcode"
	Copy    Copy
	Svc     Service[R, In]
	Env     mstkit.Env
	// ToInput turns the form's values into the service's input. values is
	// keyed by Field.Name, as typed; a row's fields are "lines[N].field", with
	// the rows numbered 1..n whatever was posted. A value that doesn't parse
	// adds a FieldError with NUM's message on its field, and never becomes a
	// zero or "none" (CD-37): the service is not called while there are any.
	// A row's error carries Field and Line (N), as the service's do.
	ToInput func(values map[string]string) (In, []doctype.FieldError)
	// Validate is the master's pure Validate. With parse errors on the form
	// it supplies the other fields' errors, because the service can't be
	// called; nil shows the parse errors alone.
	Validate func(In) (In, error)
	// Values is a stored record in the form's terms: Check fields "1" or
	// "0", Link fields the id in decimal, numbers as docscreen.FieldInput and
	// docscreen.Display take them, scalar dates as stored (yyyy-mm-dd; the
	// form shows them dd-mm-yyyy), a row's fields "lines[N].field".
	Values func(R) map[string]string
	Head   func(R) Head
	// Ref is the record AUTH checks, as the service builds it (Project and
	// Site put their own id in ProjectIDs or SiteIDs). CreatedBy stays empty:
	// mstkit.Env.CanRow reads it from the row.
	Ref func(id int64) *auth.RecordRef
	// Codes names the fields that render through components.CodeInput.
	Codes map[string]components.CodeKind
	// Numbers names the fields that render through components.NumberInput
	// with that kind, where the Field's own Type doesn't pick it: Item's
	// 6-decimal factor (CD-203).
	Numbers map[string]components.NumberKind
	// ZeroIsNone names the list columns where a stored zero means no value
	// and shows empty: Item's standard rate (CD-37), never Tax Code's 0%.
	ZeroIsNone map[string]bool
	// NoNew, NoDisable and NoDelete leave the action out: its route is not
	// mounted and its control is hidden (MST-019).
	NoNew, NoDisable, NoDelete bool
	// Extra is optional and renders on the record page for everyone who can
	// read the record; what it posts to checks its own rights.
	Extra func(R) templ.Component
	// WideLines names the row fields that need a RowsEditor's wide track:
	// Item's factor (CD-203).
	WideLines map[string]bool
	// CanCreate is optional and runs after AUTH's create right. It returns
	// the 403 for a user who may not create this master although they hold
	// that right (CD-172): New is hidden and the form is refused. It wraps
	// the service's own check, so the message isn't copied: Project and Site
	// pass their CheckUnrestricted.
	CanCreate func(ctx context.Context, tx *db.Tx, p auth.Principal) error
	// OptionLabels is a Select field's words for its stored values: field
	// name, then stored value, then label (CD-175). The form, list and
	// read-only page show the label, a value without one as stored; the form
	// posts the stored value and the export keeps it. A labelled field renders
	// as radios whatever its number of options.
	OptionLabels map[string]map[string]string
	// StartRows is how many blank rows the new page opens with; 0 opens with
	// none, and Add starts the first.
	StartRows int
	// Defaults is the new page's values, keyed by Field.Name: a Select's
	// preselected option. A refused form shows what was posted and a stored
	// record what is stored.
	Defaults map[string]string
	// Wizard is optional and makes the new page a Master-mode create wizard
	// at Base/new (CD-50): a CreateWizard. NoNew still leaves it out.
	Wizard Wizard
	// Sections is optional and makes the record page a summary with Change per
	// section, each its own form at Base/{id}/edit/{slug}; POST Base/{id} is
	// not mounted, because a section posts only its fields (CD-50). Steps
	// makes them from Field.Step.
	Sections []docscreen.FormStep
	// Selects names the Text fields that render through components.Select with
	// these options, wherever the field is: a wizard's row or a section.
	Selects map[string][]components.Option
	// PickRole is optional and answers a picker search that names a role
	// (?role=, CD-218); without a role the search is the kit's.
	PickRole func(ctx context.Context, p auth.Principal, role, search string) ([]components.PickRow, error)
	// Mount is optional and adds the master's own routes beside the kit's: its
	// child records' pages (pages.go).
	Mount func(Pages) func(chi.Router)
	// Lookups names the fields a person fills in by something they know, not
	// the stored value (Employee's user, by email). Only the wizard and
	// sections read them.
	Lookups map[string]Lookup
}

// Lookup is a field whose form shows a person's key for a stored value
// (CD-218). It runs in a read tx before the service, which stays authoritative
// inside the write. Blank is no value and calls neither function.
type Lookup struct {
	// Find is the stored value for what was typed; ok false refuses with Msg.
	Find func(ctx context.Context, tx *db.Tx, typed string) (stored string, ok bool, err error)
	// Show is what the page shows for a stored value.
	Show func(ctx context.Context, tx *db.Tx, stored string) (string, error)
	Msg  string // the refusal
	Hint string // the field's hint
	// Masked is what a user without write sees for a stored value (CD-222);
	// Show is for writers only.
	Masked string
}

// Routes mounts the screens under Base and the picker search at
// /pick/{DocType} (CD-195). Every handler checks rights through
// the service or AUTH itself; WP3 mounts them behind AUTH's middleware,
// which puts the principal on the request (CD-12).
func (s Screen[R, In]) Routes() func(chi.Router) {
	h := s.handlers()
	return func(r chi.Router) {
		r.Get(s.Base, h.handle(h.listPage))
		r.Get(s.Base+"/export.csv", h.handle(h.export))
		switch {
		case s.NoNew:
		case s.Wizard != nil:
			r.Get(s.Base+"/new", h.handle(h.wizardGet))
			r.Get(s.Base+"/new/{step}", h.handle(h.wizardGet))
			r.Post(s.Base+"/new/{step}", h.handle(h.wizardPost))
			r.Post(s.Base+"/new/"+startOverSlug, h.handle(h.wizardStartOver))
		default:
			r.Get(s.Base+"/new", h.handle(h.newPage))
			r.Post(s.Base+"/new", h.handle(h.create))
		}
		r.Get(s.Base+idPattern, h.handle(h.page))
		if s.Sections == nil {
			r.Post(s.Base+idPattern, h.handle(h.save))
		} else {
			r.Get(s.Base+idPattern+"/edit/{section}", h.handle(h.sectionPage))
			r.Post(s.Base+idPattern+"/edit/{section}", h.handle(h.sectionSave))
		}
		if !s.NoDisable {
			r.Post(s.Base+idPattern+"/disable", h.handle(h.setDisabled(true)))
			r.Post(s.Base+idPattern+"/enable", h.handle(h.setDisabled(false)))
		}
		if !s.NoDelete {
			r.Post(s.Base+idPattern+"/delete", h.handle(h.delete))
		}
		r.Get("/pick/"+s.DocType, h.handle(h.pick))
		if s.Mount != nil {
			s.Mount(h.pages())(r)
		}
	}
}

func (s Screen[R, In]) handlers() *handlers[R, In] {
	if s.Copy.Noun == "" || s.Copy.One == "" {
		panic("mstweb: " + s.DocType + ": Copy.Noun and Copy.One are required")
	}
	dt, _ := doctype.Lookup(s.DocType) // registered by the master package's init
	rowFields := slices.DeleteFunc(slices.Clone(dt.Fields), func(f doctype.Field) bool { return !f.OnLine })
	if len(rowFields) > 0 && (s.Copy.Rows == "" || s.Copy.AddRow == "") {
		panic("mstweb: " + s.DocType + ": Copy.Rows and Copy.AddRow are required with OnLine fields")
	}
	h := &handlers[R, In]{
		Screen:   s,
		dt:       sortable(dt),
		list:     &list.Service{DB: s.Env.DB, Auth: s.Env.Auth},
		exporter: csvio.NewExporter(s.Env.DB, s.Env.Auth),
		deps:     docscreen.Deps{DB: s.Env.DB, Engine: s.Env.Engine},
		// disabled is set by Disable and Enable, never on the form.
		fields:    slices.DeleteFunc(slices.Clone(dt.Fields), func(f doctype.Field) bool { return f.Name == "disabled" || f.OnLine }),
		rowFields: rowFields,
	}
	if s.Wizard != nil {
		h.wiz = s.Wizard.core()
	}
	for _, st := range slices.Concat(s.Sections, h.wiz.steps) {
		for _, name := range st.Fields {
			if _, ok := dt.Field(name); !ok {
				panic("mstweb: " + s.DocType + ": step " + st.Slug + " names no field " + name)
			}
		}
	}
	return h
}

// sortable is a copy of dt with code and name Sortable, the columns of every
// master's title. No master declares a Sortable field, and the list refuses
// to sort by any other.
func sortable(dt *doctype.DocType) *doctype.DocType {
	out := *dt
	out.Fields = slices.Clone(dt.Fields)
	for i, f := range out.Fields {
		if f.Name == "code" || f.Name == "name" {
			out.Fields[i].Sortable = true
		}
	}
	return &out
}

type handlers[R, In any] struct {
	Screen[R, In]
	dt       *doctype.DocType
	list     *list.Service
	exporter *csvio.Exporter
	deps     docscreen.Deps
	fields   []doctype.Field // the form's: no disabled, no rows
	// rowFields is each row's: the OnLine fields.
	rowFields []doctype.Field
	wiz       wizardCore // zero without a Wizard
}

// handle runs fn as the signed-in user. A request without one carries the
// zero Principal, which AUTH refuses.
func (h *handlers[R, In]) handle(fn HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.PrincipalFrom(r.Context())
		if err := fn(w, r, p); err != nil {
			h.fail(w, r, err)
		}
	}
}

// fail renders err by its status (CD-44): AUTH's 403 and 404, the 400 of a
// form without its version, as a page with the producing package's message.
// htmx requests, db.ErrBusy (503) and anything else go to web.ErrorFrom.
func (h *handlers[R, In]) fail(w http.ResponseWriter, r *http.Request, err error) {
	if pr, ok := docscreen.Classify(err); ok && !ui.IsHTMX(r) {
		ui.Render(w, r, pr.Status, problemPage(pr.Message, components.Link{Label: h.Copy.Plural, Href: h.Base}), nil)
		return
	}
	web.ErrorFrom(w, r, err)
}

// listPage serves GET Base: enabled records by the DocType's default sort,
// or every record with include_disabled=true (each tagged Enabled or
// Disabled); q searches the DocType's SearchFields and sort is one of
// sortOptions.
func (h *handlers[R, In]) listPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(page, 1)
	all := q.Get("include_disabled") == "true"
	lq, err := h.query(q)
	if err != nil {
		return err
	}
	lq.Page, lq.PageSize = page, pageSize
	res, err := h.list.List(ctx, p, h.dt, lq)
	if err != nil {
		return err
	}
	cols := h.listColumns(all)
	rows := make([]components.Row, len(res.Rows))
	err = h.Env.DB.Read(ctx, func(tx *db.Tx) error {
		for i, lr := range res.Rows {
			vals, err := h.cells(ctx, tx, lr, cols)
			if err != nil {
				return err
			}
			off, _ := lr.Values["disabled"].(bool)
			rows[i] = components.Row{Href: h.recordPath(lr.ID), Values: vals, State: stateOf(off)}
		}
		return nil
	})
	if err != nil {
		return err
	}
	var newLink *components.Link
	if !h.NoNew && h.checkCreate(ctx, p) == nil {
		newLink = &components.Link{Label: h.Copy.New, Href: h.Base + "/new"}
	}
	v := listView{
		Title: h.Copy.Plural,
		Bar: components.FilterBarProps{
			Action: h.Base, Search: q.Get("q"), SearchLabel: h.Copy.Search, New: newLink,
			Sort: selectedSort(h.dt, q), Sorts: sortOptions(h.dt), Count: h.count(res.Total),
		},
		List:  components.DataListProps{Label: h.Copy.Plural, Columns: cols, Rows: rows},
		Pages: h.pageInfo(q, page, res.Total),
	}
	// Past the end there are records: Pagination says there are no more.
	if res.Total == 0 {
		text, err := h.emptyText(ctx, p, q.Get("q"))
		if err != nil {
			return err
		}
		v.List.Empty = components.EmptyState(text, newLink)
	}
	if h.can(ctx, p, auth.RightExport) {
		v.Bar.Export = &components.Link{Label: "Export", Href: h.exportURL(q)}
	}
	if all {
		v.Bar.Keep = []components.Param{{Name: "include_disabled", Value: "true"}}
		v.Bar.Chips = []components.Chip{{Label: "Disabled included", RemoveURL: h.listURL(q, false)}}
		v.Bar.ClearURL = h.Base
	} else if !h.NoDisable {
		v.ShowDisabled = &components.Link{Label: "Show disabled " + h.Copy.Noun, Href: h.listURL(q, true)}
	}
	ui.Render(w, r, http.StatusOK, listPage(v), listMain(v))
	return nil
}

// listColumns is the list's columns. The code is in the title. The state
// column, Enabled or Disabled, only shows when disabled records are listed
// (all). A field of the master's own at the state's priority (Project's and
// Employee's status, M13) is a plain column: DataList's state column would
// show the Row's State, not its value. Disabled then gets a column of its own.
func (h *handlers[R, In]) listColumns(all bool) []components.Column {
	isDisabled := func(c components.Column) bool { return c.Key == "disabled" }
	cols := slices.DeleteFunc(docscreen.ListColumns(h.dt), func(c components.Column) bool {
		return c.Key == "code" || isDisabled(c) && !all
	})
	for i := range cols {
		switch {
		case isDisabled(cols[i]):
			cols[i].Label = "Status" // its values are Enabled and Disabled
		case cols[i].Priority == components.PState:
			cols[i].Priority = components.PMeta
		}
	}
	if all && !slices.ContainsFunc(cols, isDisabled) {
		cols = append(cols, components.Column{Key: "disabled", Label: "State", Priority: components.PState})
	}
	return cols
}

// query is the list.Query of a list URL: its search, sort and disabled
// filter. The list and its export share it. An unknown sort is the list's own
// 422.
func (h *handlers[R, In]) query(q url.Values) (list.Query, error) {
	keys, err := list.ParseSort(h.dt, strings.Replace(q.Get("sort"), ".", " ", 1))
	if err != nil {
		return list.Query{}, err
	}
	lq := list.Query{Search: q.Get("q"), Sort: keys}
	if q.Get("include_disabled") != "true" {
		lq.Filters = list.DefaultFilters(h.dt)
	}
	return lq, nil
}

// cells is a list row's text for cols (CD-42), formatted by docscreen.Display
// as a document list's are; the name column is the title.
func (h *handlers[R, In]) cells(ctx context.Context, tx *db.Tx, lr list.Row, cols []components.Column) (map[string]string, error) {
	vals := make(map[string]string, len(cols))
	for _, c := range cols {
		i := slices.IndexFunc(h.dt.Fields, func(f doctype.Field) bool { return f.Name == c.Key })
		v := lr.Values[c.Key]
		if h.ZeroIsNone[c.Key] && v != nil && reflect.ValueOf(v).IsZero() {
			v = nil
		}
		s, err := h.display(ctx, tx, h.dt.Fields[i], plain(v))
		if err != nil {
			return nil, err
		}
		vals[c.Key] = s
	}
	vals["name"] = title(lr)
	return vals, nil
}

// display is a stored value's text: its Lookup's mask, its OptionLabels label,
// else docscreen's. Only sectionCards shows a writer the Lookup's own text, so
// a new caller can't reveal it (CD-222).
func (h *handlers[R, In]) display(ctx context.Context, tx *db.Tx, fd doctype.Field, value string) (string, error) {
	if l, ok := h.Lookups[fd.Name]; ok && value != "" {
		return l.Masked, nil
	}
	if label := h.OptionLabels[fd.Name][value]; label != "" {
		return label, nil
	}
	return docscreen.Display(ctx, tx, h.Env.Engine, fd, value)
}

// plain is a list.Row value (typed by its field, nil for NULL) in the form
// docscreen.Display takes. No master lists a date or timestamp (M13).
func plain(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case interface{ Plain() string }: // Money, Qty, Rate, Percent, Factor
		return x.Plain()
	}
	return fmt.Sprint(v)
}

// emptyText is the list with no record to show. A search that found nothing
// and a list whose records are all disabled are not "yet".
func (h *handlers[R, In]) emptyText(ctx context.Context, p auth.Principal, search string) (string, error) {
	if search != "" {
		return noMatch, nil
	}
	n, err := h.list.Count(ctx, p, h.dt, list.Query{})
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "All " + h.Copy.Noun + " are disabled.", nil
	}
	return h.Copy.Empty, nil
}

// count is the bar's result count; an empty list says so itself.
func (h *handlers[R, In]) count(total int) string {
	switch total {
	case 0:
		return ""
	case 1:
		return "1 " + h.Copy.One
	}
	return types.FormatCount(int64(total)) + " " + h.Copy.Noun
}

// selectedSort is the sort the bar shows: the URL's, else the default, if the
// DocType has one.
func selectedSort(dt *doctype.DocType, q url.Values) string {
	if s := q.Get("sort"); s != "" || len(dt.DefaultSort) == 0 {
		return s
	}
	return sortValue(dt.DefaultSort[0])
}

func sortValue(k doctype.SortKey) string {
	if k.Desc {
		return k.Field + ".desc"
	}
	return k.Field + ".asc"
}

// sortOptions is DefaultSort's keys, then each Sortable field both ways, in
// docscreen's words.
func sortOptions(dt *doctype.DocType) []components.Option {
	keys := slices.Clone(dt.DefaultSort)
	for _, f := range dt.Fields {
		if f.Sortable {
			keys = append(keys, doctype.SortKey{Field: f.Name}, doctype.SortKey{Field: f.Name, Desc: true})
		}
	}
	var opts []components.Option
	for _, k := range keys {
		i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == k.Field })
		label := dt.Fields[i].Label + " low to high"
		if k.Desc {
			label = dt.Fields[i].Label + " high to low"
		}
		o := components.Option{Value: sortValue(k), Label: label}
		if !slices.ContainsFunc(opts, func(x components.Option) bool { return x.Value == o.Value }) {
			opts = append(opts, o)
		}
	}
	return opts
}

// export serves GET Base/export.csv: the list the URL shows, every page, as
// a download. read guards the route, so a user without it gets the read
// message; csvio's Export then checks export (CD-145). The buffer turns a
// refused export into a page, not a broken download.
func (h *handlers[R, In]) export(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	ctx := r.Context()
	if err := h.Env.Auth.Can(ctx, p, h.DocType, auth.RightRead, nil); err != nil {
		return err
	}
	lq, err := h.query(r.URL.Query())
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if _, err := h.exporter.Export(ctx, &buf, p, h.dt, lq); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(h.Base)+`.csv"`)
	_, _ = w.Write(buf.Bytes()) // the client is gone; nothing can tell it
	return nil
}

// title is a list row's "{Code} {Name}" (CD-68), or the name alone for a
// master without a code.
func title(lr list.Row) string {
	code, _ := lr.Values["code"].(string)
	name, _ := lr.Values["name"].(string)
	return strings.TrimSpace(code + " " + name)
}

// pick is the picker search, GET /pick/{DocType}?q= (the URL
// docscreen.FieldInput's pickers use): enabled records the user may read,
// "{Code} {Name}" (CD-68). htmx names the listbox it fills in HX-Target.
func (h *handlers[R, In]) pick(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	q := r.URL.Query().Get("q")
	rows, err := h.pickRows(r, p, q)
	if err != nil {
		return err
	}
	frag := components.PickerResults(cmp.Or(r.Header.Get("HX-Target"), "pick-"+h.dt.Table), rows, q)
	ui.Render(w, r, http.StatusOK, frag, frag)
	return nil
}

// pickRows is the search's rows: PickRole's when the URL names a role.
func (h *handlers[R, In]) pickRows(r *http.Request, p auth.Principal, q string) ([]components.PickRow, error) {
	if role := r.URL.Query().Get("role"); role != "" && h.PickRole != nil {
		return h.PickRole(r.Context(), p, role, q)
	}
	res, err := h.list.Picker(r.Context(), p, h.dt, q, pageSize)
	if err != nil {
		return nil, err
	}
	rows := make([]components.PickRow, len(res.Rows))
	for i, lr := range res.Rows {
		code, _ := lr.Values["code"].(string)
		name, _ := lr.Values["name"].(string)
		rows[i] = components.PickRow{ID: lr.ID, Code: code, Name: name}
	}
	return rows, nil
}

func (h *handlers[R, In]) newPage(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	if err := h.checkCreate(r.Context(), p); err != nil {
		return err
	}
	f := form{Values: map[string]string{}}
	maps.Copy(f.Values, h.Defaults)
	for no := 1; no <= h.StartRows; no++ {
		for _, fd := range h.rowFields {
			f.Values[rowKey(no, fd.Name)] = ""
		}
	}
	return h.render(w, r, p, http.StatusOK, 0, f)
}

func (h *handlers[R, In]) create(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	f := h.posted(r)
	if action := r.PostFormValue(rowsButton); action != "" {
		return h.redrawRows(w, r, p, 0, f, action)
	}
	renumberRows(h.rowFields, f.Values, rowNos(h.rowFields, f.Values))
	in, errs := h.ToInput(f.Values)
	if len(errs) > 0 {
		return h.unparsed(w, r, p, 0, f, in, errs)
	}
	saved, err := h.Svc.Create(r.Context(), p, in)
	if err != nil {
		return h.refused(w, r, p, 0, f, err)
	}
	return Done(w, r, h.Copy.Saved, h.recordPath(saved.ID), saved.Warnings...)
}

func (h *handlers[R, In]) page(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	return h.render(w, r, p, http.StatusOK, RecordID(r), form{})
}

// save updates the record at the version the page posted (CD-94).
func (h *handlers[R, In]) save(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	id := RecordID(r)
	f := h.posted(r)
	version, err := PostedVersion(r)
	if err != nil {
		return err
	}
	f.Version = version
	if action := r.PostFormValue(rowsButton); action != "" {
		return h.redrawRows(w, r, p, id, f, action)
	}
	renumberRows(h.rowFields, f.Values, rowNos(h.rowFields, f.Values))
	in, errs := h.ToInput(f.Values)
	if len(errs) > 0 {
		return h.unparsed(w, r, p, id, f, in, errs)
	}
	saved, err := h.Svc.Update(r.Context(), p, id, version, in)
	if err != nil {
		return h.refused(w, r, p, id, f, err)
	}
	return Done(w, r, h.Copy.Saved, h.recordPath(id), saved.Warnings...)
}

// unparsed answers a form ToInput couldn't read. The service is not called:
// in holds a zero for what failed, and the service might save it (CD-37). So
// the right the service would check goes first, which keeps a 403 ahead of
// the 422, and Validate adds the other fields' errors; checks that need the
// database wait for the next post. A row that failed isn't in in, so Validate
// is only heard on the scalar fields then: its total of the rows, or a row
// number shifted by the missing row, would be false.
func (h *handlers[R, In]) unparsed(w http.ResponseWriter, r *http.Request, p auth.Principal, id int64, f form, in In, errs []doctype.FieldError) error {
	if err := h.checkSubmit(r.Context(), p, id); err != nil {
		return err
	}
	rowFailed := slices.ContainsFunc(errs, func(e doctype.FieldError) bool { return e.Line > 0 })
	var he doctype.HTTPError
	if h.Validate != nil {
		if _, err := h.Validate(in); errors.As(err, &he) {
			for _, e := range he.FieldErrors() {
				if rowFailed && (e.Field == "" || e.Line > 0) {
					continue
				}
				// A failed field is empty in in: its parse error says why.
				if !slices.ContainsFunc(errs, func(x doctype.FieldError) bool { return x.Field == e.Field && x.Line == e.Line }) {
					errs = append(errs, e)
				}
			}
		}
	}
	return h.refused(w, r, p, id, f, &mstkit.ValidationError{Fields: errs})
}

// checkSubmit is the right the service would check for this form: id 0 is the
// new page's.
func (h *handlers[R, In]) checkSubmit(ctx context.Context, p auth.Principal, id int64) error {
	if id == 0 {
		return h.checkCreate(ctx, p)
	}
	return h.canRow(ctx, p, auth.RightWrite, h.Ref(id))
}

// canRow is the check on a stored row: its created_by decides own scope, as in
// the list (CD-223).
func (h *handlers[R, In]) canRow(ctx context.Context, p auth.Principal, right auth.Right, rec *auth.RecordRef) error {
	return h.Env.CanRow(ctx, p, h.kind(), right, rec)
}

func (h *handlers[R, In]) kind() mstkit.Kind {
	return mstkit.Kind{DocType: h.DocType, Table: h.dt.Table}
}

// checkCreate is whether p may create at all: AUTH's right, then CanCreate.
func (h *handlers[R, In]) checkCreate(ctx context.Context, p auth.Principal) error {
	if err := h.Env.Auth.Can(ctx, p, h.DocType, auth.RightCreate, nil); err != nil {
		return err
	}
	if h.CanCreate == nil {
		return nil
	}
	return h.Env.DB.Read(ctx, func(tx *db.Tx) error { return h.CanCreate(ctx, tx, p) })
}

// redrawRows answers an Add or Remove button: the same page with the rows
// changed and everything else as typed. Nothing is saved (CD-201), but the
// right a save needs comes first, and a row that isn't there is a bad form.
func (h *handlers[R, In]) redrawRows(w http.ResponseWriter, r *http.Request, p auth.Principal, id int64, f form, action string) error {
	if err := h.checkSubmit(r.Context(), p, id); err != nil {
		return err
	}
	if err := editRows(h.rowFields, nil, f.Values, action); err != nil {
		return err
	}
	return h.render(w, r, p, http.StatusOK, id, f)
}

// editRows applies action, "add" or "remove:<no>", to the rows of fields in
// values, and numbers them from 1 again. A row's fields stay together; Add's
// are their defaults.
func editRows(fields []doctype.Field, defaults, values map[string]string, action string) error {
	nos := rowNos(fields, values)
	if action != "add" {
		num, ok := strings.CutPrefix(action, "remove:")
		no, err := strconv.Atoi(num)
		if !ok || err != nil || !slices.Contains(nos, no) {
			return auth.ErrBadForm
		}
		nos = slices.DeleteFunc(nos, func(n int) bool { return n == no })
	}
	renumberRows(fields, values, nos)
	if action == "add" {
		for _, fd := range fields {
			values[rowKey(len(nos)+1, fd.Name)] = defaults[fd.Name]
		}
	}
	return nil
}

// renumberRows keeps rows nos of values, in that order, numbered from 1, so
// a row's number on the page is its Line in the service's errors.
func renumberRows(fields []doctype.Field, values map[string]string, nos []int) {
	rows := make(map[string]string, len(values))
	for i, no := range nos {
		for _, fd := range fields {
			rows[rowKey(i+1, fd.Name)] = values[rowKey(no, fd.Name)]
		}
	}
	maps.DeleteFunc(values, func(key, _ string) bool { return isRowKey(fields, key) })
	maps.Copy(values, rows)
}

func (h *handlers[R, In]) setDisabled(off bool) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		id := RecordID(r)
		version, err := PostedVersion(r)
		if err != nil {
			return err
		}
		if err := h.Svc.SetDisabled(r.Context(), p, id, version, off); err != nil {
			return h.refused(w, r, p, id, form{}, err)
		}
		msg := h.Copy.Enabled
		if off {
			msg = h.Copy.Disabled
		}
		return Done(w, r, msg, h.recordPath(id))
	}
}

// delete deletes an unused record; a used one is refused with mstkit's
// "Cannot delete …" (422, no field), shown on the record page.
func (h *handlers[R, In]) delete(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
	id := RecordID(r)
	version, err := PostedVersion(r)
	if err != nil {
		return err
	}
	if err := h.Svc.Delete(r.Context(), p, id, version); err != nil {
		return h.refused(w, r, p, id, form{}, err)
	}
	return Done(w, r, h.Copy.Deleted, h.Base)
}

// Done is a successful write: its warnings and a success banner on the next
// page (CD-80) and 303 to it.
func Done(w http.ResponseWriter, r *http.Request, msg, next string, warnings ...string) error {
	for _, warning := range warnings {
		web.Flash(r.Context(), "warning", warning)
	}
	web.Flash(r.Context(), "success", msg)
	http.Redirect(w, r, next, http.StatusSeeOther) //nolint:gosec // G710: next is Base or Base + "/" + a numeric id
	return nil
}

// refused shows the page again when the page can show err: field errors next
// to their fields (422), or a stale version (409, with Reload) or a refusal
// with no field (422) in a banner. f keeps what the user typed; an empty f
// shows the stored record. Anything else (403, 404, 503) is returned for
// fail.
func (h *handlers[R, In]) refused(w http.ResponseWriter, r *http.Request, p auth.Principal, id int64, f form, err error) error {
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusConflict && he.Status() != http.StatusUnprocessableEntity {
		return err
	}
	f.Errs, f.Problem = splitErrs(he.FieldErrors())
	if len(f.Errs) == 0 && f.Problem == "" {
		f.Problem = he.Error()
	}
	f.Reload = he.Status() == http.StatusConflict
	return h.render(w, r, p, he.Status(), id, f)
}

// splitErrs keeps the errors that name a field and joins the messages of
// those that don't (Field ""), which the page shows in its banner.
func splitErrs(errs []doctype.FieldError) ([]doctype.FieldError, string) {
	var fields []doctype.FieldError
	var others []string
	for _, e := range errs {
		if e.Field == "" {
			others = append(others, e.Message)
		} else {
			fields = append(fields, e)
		}
	}
	return fields, strings.Join(others, " ")
}

// render renders the new page (id 0) or record id's page with status. On a
// record page an empty f shows the stored values and version. A user
// without write sees the values instead of the form.
func (h *handlers[R, In]) render(w http.ResponseWriter, r *http.Request, p auth.Principal, status int, id int64, f form) error {
	ctx := r.Context()
	v := recordView{Title: h.Copy.New, Action: h.Base + "/new", Back: components.Link{Label: h.Copy.Plural, Href: h.Base}, CanWrite: true, Width: layout.Form}
	if len(h.rowFields) > 0 {
		v.Width = layout.Content // the editor's rows need more than a form's width (CD-201)
	}
	if id != 0 {
		ref := h.Ref(id)
		var canDelete bool
		var rec R
		err := h.Env.DB.Read(ctx, func(tx *db.Tx) error {
			// CanRowTx fills ref, so the other two rights reuse its created_by.
			err := h.Env.CanRowTx(ctx, tx, p, h.kind(), auth.RightRead, ref)
			if err != nil {
				return err
			}
			v.CanWrite = h.Env.Auth.CanTx(ctx, tx, p, h.DocType, auth.RightWrite, ref) == nil
			canDelete = h.Env.Auth.CanTx(ctx, tx, p, h.DocType, auth.RightDelete, ref) == nil
			if rec, err = h.Svc.Get(ctx, tx, id); err != nil {
				return err
			}
			if h.Sections != nil {
				v.Cards, err = h.sectionCards(ctx, tx, id, h.stored(rec), v.CanWrite)
				return err
			}
			if v.CanWrite {
				return nil
			}
			vals := h.stored(rec)
			if v.Pairs, err = h.pairs(ctx, tx, vals); err != nil {
				return err
			}
			v.RowPairs, err = h.rowPairs(ctx, tx, vals)
			return err
		})
		if err != nil {
			return err
		}
		head := h.Head(rec)
		if f.Values == nil {
			f.Version = head.Version
			if f.Values, err = h.formValues(rec); err != nil {
				return err
			}
		}
		if h.Sections != nil { // the summary has no inputs for a field error to sit on
			f.Problem = strings.TrimSpace(f.Problem + " " + strings.Join(messages(f.Errs), " "))
			f.Errs = nil
		}
		v.ID, v.Title, v.Action, v.Disabled = id, strings.TrimSpace(head.Code+" "+head.Name), h.recordPath(id), head.Disabled
		v.More = h.more(id, head.Disabled, f.Version, v.CanWrite, canDelete)
		v.Confirm = !h.NoDelete && r.URL.Query().Has("delete") && canDelete
		if h.Extra != nil {
			v.Extra = h.Extra(rec)
		}
	}
	v.Form = f
	for _, fd := range h.fields {
		v.Inputs = append(v.Inputs, h.input(fd, fd.Name, f))
	}
	if len(h.rowFields) > 0 {
		v.Rows = h.rows(f)
	}
	ui.Render(w, r, status, recordPage(v), nil)
	return nil
}

// input is fd's control at path, its name or a row's "lines[N].field":
// EmailInput for a Lookups field, CodeInput for a Codes field, NumberInput for a Numbers field, Radios for an
// OptionLabels field, Select for a Selects field, else docscreen's.
func (h *handlers[R, In]) input(fd doctype.Field, path string, f form) templ.Component {
	lookup, looked := h.Lookups[fd.Name]
	code, coded := h.Codes[fd.Name]
	number, numbered := h.Numbers[fd.Name]
	labels, labelled := h.OptionLabels[fd.Name]
	opts, selected := h.Selects[fd.Name]
	fd.Name = path
	id, value, msg := components.FieldID(path), f.Values[path], f.errFor(path)
	if !looked && !coded && !numbered && !labelled && !selected {
		return docscreen.FieldInput(fd, id, value, msg, h.deps)
	}
	p := components.FieldProps{ID: id, Name: fd.Name, Label: fd.Label, Hint: fd.Hint, Error: msg, Value: value, Optional: !fd.Required}
	if looked {
		p.Hint = lookup.Hint
		return components.EmailInput(p, "off")
	}
	if selected {
		return components.Select(p, opts)
	}
	if labelled {
		opts := make([]components.Option, len(fd.Options))
		for i, o := range fd.Options {
			opts[i] = components.Option{Value: o, Label: cmp.Or(labels[o], o)}
		}
		return components.Radios(p, opts)
	}
	if fd.MaxLen > 0 {
		p.Attrs = templ.Attributes{"maxlength": strconv.Itoa(fd.MaxLen)}
	}
	if numbered {
		return components.NumberInput(p, number, false)
	}
	return components.CodeInput(p, code)
}

// rows is the editor for f's rows, a cell for each OnLine field.
func (h *handlers[R, In]) rows(f form) *components.RowsProps {
	return h.rowsEditor(h.Copy.Rows, h.Copy.AddRow, h.rowFields, f)
}

// rowsEditor is the editor for f's rows of fields.
func (h *handlers[R, In]) rowsEditor(legend, addLabel string, fields []doctype.Field, f form) *components.RowsProps {
	p := &components.RowsProps{ID: components.FieldID("lines"), Legend: legend, AddLabel: addLabel}
	for _, no := range rowNos(fields, f.Values) {
		row := components.RowsRow{No: no}
		for _, fd := range fields {
			row.Cells = append(row.Cells, components.GridCell{Label: fd.Label, Input: h.input(fd, rowKey(no, fd.Name), f), Wide: h.WideLines[fd.Name]})
		}
		p.Rows = append(p.Rows, row)
	}
	return p
}

// rowNos is the numbers of the rows of fields in values, in order.
func rowNos(fields []doctype.Field, values map[string]string) []int {
	var nos []int
	for key := range values {
		if isRowKey(fields, key) {
			no, _, _ := parseRowKey(key)
			nos = append(nos, no)
		}
	}
	slices.Sort(nos)
	return slices.Compact(nos)
}

func isRowKey(fields []doctype.Field, key string) bool {
	_, field, ok := parseRowKey(key)
	return ok && slices.ContainsFunc(fields, func(f doctype.Field) bool { return f.Name == field })
}

func rowKey(no int, field string) string {
	return doctype.FieldError{Field: field, Line: no}.Path()
}

// parseRowKey splits "lines[N].field"; rows count from 1, written as
// strconv.Itoa does, so one row has one key.
func parseRowKey(key string) (n int, field string, ok bool) {
	rest, ok := strings.CutPrefix(key, "lines[")
	if !ok {
		return 0, "", false
	}
	num, field, ok := strings.Cut(rest, "].")
	if !ok {
		return 0, "", false
	}
	n, err := strconv.Atoi(num)
	return n, field, err == nil && n > 0 && strconv.Itoa(n) == num
}

// stored is rec's values for the form and the read-only view. A zero Link id
// means no link, which shows as "", never "0".
func (h *handlers[R, In]) stored(rec R) map[string]string {
	vals := h.Values(rec)
	for _, fd := range h.fields {
		if fd.Type == doctype.Link && vals[fd.Name] == "0" {
			vals[fd.Name] = ""
		}
	}
	return vals
}

// formValues is rec's stored values as the form shows them: dates dd-mm-yyyy,
// where Values and the read-only view have them as stored. What a user typed
// never comes through here, so a refused save keeps it.
func (h *handlers[R, In]) formValues(rec R) (map[string]string, error) {
	vals := h.stored(rec)
	for _, fd := range h.fields {
		if fd.Type != doctype.Date || vals[fd.Name] == "" {
			continue
		}
		d, err := types.DateFromISO(vals[fd.Name])
		if err != nil {
			return nil, err
		}
		vals[fd.Name] = types.FormatDate(d)
	}
	return vals, nil
}

// pairs is the read-only view of the stored values.
func (h *handlers[R, In]) pairs(ctx context.Context, tx *db.Tx, vals map[string]string) ([]components.Pair, error) {
	out := make([]components.Pair, 0, len(h.fields))
	for _, fd := range h.fields {
		s, err := h.display(ctx, tx, fd, vals[fd.Name])
		if err != nil {
			return nil, err
		}
		out = append(out, components.Pair{Label: fd.Label, Value: s})
	}
	return out, nil
}

// rowPairs is the read-only view of the stored rows, one summary each.
func (h *handlers[R, In]) rowPairs(ctx context.Context, tx *db.Tx, vals map[string]string) ([][]components.Pair, error) {
	var out [][]components.Pair
	for _, no := range rowNos(h.rowFields, vals) {
		row := []components.Pair{{Label: "Row", Value: strconv.Itoa(no)}}
		for _, fd := range h.rowFields {
			s, err := h.display(ctx, tx, fd, vals[rowKey(no, fd.Name)])
			if err != nil {
				return nil, err
			}
			row = append(row, components.Pair{Label: fd.Label, Value: s})
		}
		out = append(out, row)
	}
	return out, nil
}

// more is the record's "More actions": Disable or Enable for write, and
// Delete for delete, which opens the confirm step (?delete) first
// (design-system §7: danger actions need a confirm step). The POSTs carry
// the rendered version (CD-94, CD-183).
func (h *handlers[R, In]) more(id int64, disabled bool, version int, canWrite, canDelete bool) []components.MoreAction {
	var out []components.MoreAction
	if !h.NoDisable && canWrite {
		toggle := components.MoreAction{Label: "Disable", Href: h.recordPath(id) + "/disable", Post: true}
		if disabled {
			toggle.Label, toggle.Href = "Enable", h.recordPath(id)+"/enable"
		}
		toggle.Hidden = []components.Param{{Name: "version", Value: strconv.Itoa(version)}}
		out = append(out, toggle)
	}
	if !h.NoDelete && canDelete {
		out = append(out, components.MoreAction{Label: "Delete", Href: h.recordPath(id) + "?delete", Danger: true})
	}
	return out
}

func (h *handlers[R, In]) can(ctx context.Context, p auth.Principal, right auth.Right) bool {
	return h.Env.Auth.Can(ctx, p, h.DocType, right, nil) == nil
}

// posted is the form's values as typed, its rows' included.
func (h *handlers[R, In]) posted(r *http.Request) form {
	f := form{Values: map[string]string{}}
	for _, fd := range h.fields {
		f.Values[fd.Name] = r.PostFormValue(fd.Name)
	}
	for key := range r.PostForm {
		if isRowKey(h.rowFields, key) {
			f.Values[key] = r.PostFormValue(key)
		}
	}
	return f
}

func (h *handlers[R, In]) recordPath(id int64) string {
	return h.Base + "/" + strconv.FormatInt(id, 10)
}

// listURL is the list with q's search and sort and, when all, disabled
// records too.
func (h *handlers[R, In]) listURL(q url.Values, all bool) string {
	return withQuery(h.Base, q, all)
}

// exportURL is the download of the list q shows.
func (h *handlers[R, In]) exportURL(q url.Values) string {
	return withQuery(h.Base+"/export.csv", q, q.Get("include_disabled") == "true")
}

func withQuery(base string, q url.Values, all bool) string {
	u := url.Values{}
	for _, k := range []string{"q", "sort"} {
		if v := q.Get(k); v != "" {
			u.Set(k, v)
		}
	}
	if all {
		u.Set("include_disabled", "true")
	}
	if len(u) == 0 {
		return base
	}
	return base + "?" + u.Encode()
}

// pageInfo is the list's paging; the links keep the search and filter.
func (h *handlers[R, In]) pageInfo(q url.Values, page, total int) components.PageInfo {
	at := func(n int) string {
		u := url.Values{}
		for k, v := range q {
			u[k] = v
		}
		u.Set("page", strconv.Itoa(n))
		return h.Base + "?" + u.Encode()
	}
	return components.PageInfo{
		Page: page, From: (page-1)*pageSize + 1, To: min(page*pageSize, total), Total: total, Noun: h.Copy.Noun,
		PrevURL: at(page - 1), NextURL: at(page + 1),
	}
}

// PostedVersion is the version the page was rendered at; a missing or
// non-numeric one is AUTH's 400 form error (CD-146, CD-183).
func PostedVersion(r *http.Request) (int, error) {
	v, err := strconv.Atoi(r.PostFormValue("version"))
	if err != nil {
		return 0, auth.ErrBadForm
	}
	return v, nil
}

// RecordID is the {id} in the URL. The routes only match digits; one too
// large for int64 is math.MaxInt64 here, which no record has (404 from Get).
func RecordID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

// disabledState tags a disabled record in the list's status column and on
// its page; masters have no document state ("", CD-21).
const disabledState doctype.State = "Disabled"

func stateOf(off bool) doctype.State {
	if off {
		return disabledState
	}
	return "Enabled"
}

// form is the form's values (as typed or as stored), the version the page
// was rendered at and what the last save was refused with: field errors,
// and Problem for the banner.
type form struct {
	Values  map[string]string
	Version int
	Errs    []doctype.FieldError
	Problem string
	Reload  bool
}

// errFor is the message on the field at path, "" when none.
func (f form) errFor(path string) string {
	for _, e := range f.Errs {
		if e.Path() == path {
			return e.Message
		}
	}
	return ""
}

// summary links each field error to its input's id; a line error's id is
// FieldID("lines[N].field"), as docscreen names line inputs.
func (f form) summary() []components.FieldErr {
	out := make([]components.FieldErr, 0, len(f.Errs))
	for _, e := range f.Errs {
		path := e.Field
		if e.Line > 0 {
			path = "lines[" + strconv.Itoa(e.Line) + "]." + e.Field
		}
		out = append(out, components.FieldErr{ID: components.FieldID(path), Message: e.Message})
	}
	return out
}

// listView is what the list renders.
type listView struct {
	Title        string
	Bar          components.FilterBarProps
	List         components.DataListProps
	Pages        components.PageInfo
	ShowDisabled *components.Link // lists disabled records too; nil when they are listed
}

// recordView is what the new and record pages render. ID is 0 on the new
// page.
type recordView struct {
	ID                          int64
	Title, Action               string
	Back                        components.Link
	Disabled, CanWrite, Confirm bool
	Form                        form
	Inputs                      []templ.Component
	Width                       layout.Width
	Rows                        *components.RowsProps // nil for a master without rows
	Cards                       []sectionCard         // the summary, with Sections
	Pairs                       []components.Pair
	RowPairs                    [][]components.Pair
	More                        []components.MoreAction
	Extra                       templ.Component
}
