package docscreen_test

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// stateLister answers a query by its state filter: totals[state] for a
// one-value state filter, all for any other query, and remembers every
// query it got.
type stateLister struct {
	mu     sync.Mutex
	totals map[string]int
	all    types.Result[docscreen.ListRow]
	got    []docscreen.ListQuery
}

func (l *stateLister) List(_ context.Context, _ auth.Principal, _ *doctype.DocType, q docscreen.ListQuery) (types.Result[docscreen.ListRow], error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, q)
	for _, f := range q.Filters {
		if f.Field == "state" && len(f.Values) == 1 {
			return types.Result[docscreen.ListRow]{Total: l.totals[f.Values[0]]}, nil
		}
	}
	return l.all, nil
}

// Count answers as List does, by the query's state filter.
func (l *stateLister) Count(ctx context.Context, p auth.Principal, dt *doctype.DocType, q docscreen.ListQuery) (int, error) {
	res, err := l.List(ctx, p, dt, q)
	return res.Total, err
}

func r5Env(t *testing.T, l *stateLister, modules map[string]docscreen.Module) http.Handler {
	t.Helper()
	registerLabelled()
	az := authfake.New()
	az.Grant("Buyer", labelledType, "read", auth.ScopeAll)
	registerColumns()
	az.Grant("Buyer", columnsType, "read", auth.ScopeAll)
	az.AddRole(priya.ID, "Buyer")
	d := docscreen.Deps{
		DB: &docscreentest.Reader{}, Engine: docfake.New(), Auth: az, Approval: &approver{},
		Lister: l, Extras: docscreentest.NewExtras(), Modules: modules,
		Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
	}
	r := chi.NewRouter()
	docscreen.Routes(d)(r)
	return r
}

func labelledRows() types.Result[docscreen.ListRow] {
	return types.Result[docscreen.ListRow]{Total: 7, Rows: []docscreen.ListRow{
		{Info: doctype.Info{DocType: labelledType, ID: 1, State: doctype.StateApproved, Display: "LO/1"},
			Values: map[string]string{"kind": "normal", "state": "approved", "amount": "2.00"}},
	}}
}

type quickChip struct{ text, href, current string }

func quickChips(doc *goquery.Document) []quickChip {
	var out []quickChip
	doc.Find(`ul[aria-label="Quick filters"] a`).Each(func(_ int, a *goquery.Selection) {
		out = append(out, quickChip{norm(a.Text()), a.AttrOr("href", ""), a.AttrOr("aria-current", "")})
	})
	return out
}

// R5 (UI v2 §5.12, A-23): the quick chips come from the DocType's state
// field, in its declared order, with plain labels (the DocType's state
// labels) and counts; a state with no records is left out. Each chip keeps
// the search, sort and the other filters and starts on page 1; the counts
// are asked with the same search and filters.
func TestList_QuickChipsFromStateField(t *testing.T) {
	l := &stateLister{totals: map[string]int{"draft": 2, "pending_approval": 0, "approved": 1205}, all: labelledRows()}
	h := r5Env(t, l, nil)
	rec := uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?q=steel&sort=amount.desc&page=3&f=kind:in:urgent", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := uitest.Doc(t, rec)
	enc := func(q docscreen.ListQuery) string { return "/doc/" + labelledType + "?" + q.Encode() }
	kind := docscreen.Filter{Field: "kind", Op: "in", Values: []string{"urgent"}}
	state := func(v string) docscreen.Filter {
		return docscreen.Filter{Field: "state", Op: "in", Values: []string{v}}
	}
	want := []quickChip{
		{"All", enc(docscreen.ListQuery{Search: "steel", Sort: "amount.desc", Filters: []docscreen.Filter{kind}}), "true"},
		{"Draft 2", enc(docscreen.ListQuery{Search: "steel", Sort: "amount.desc", Filters: []docscreen.Filter{kind, state("draft")}}), ""},
		{"Approved 1,205", enc(docscreen.ListQuery{Search: "steel", Sort: "amount.desc", Filters: []docscreen.Filter{kind, state("approved")}}), ""},
	}
	if got := quickChips(doc); !slices.Equal(got, want) {
		t.Errorf("quick chips =\n%q\nwant\n%q", got, want)
	}
	counts := 0
	for _, q := range l.got[1:] {
		counts++
		if q.Search != "steel" || len(q.Filters) != 2 || q.Filters[0].Field != "kind" || q.Size != 0 || q.Page != 0 || q.Sort != "" {
			t.Errorf("count query %+v: want the search, the kind filter and the state, first page, no sort", q)
		}
	}
	if counts != 3 {
		t.Errorf("%d count queries, want one per declared state (3)", counts)
	}
}

// The current chip is the one-value state filter, also with no records left
// (it is never dropped while on); it is not repeated as a removable chip.
// Two values are not a quick chip: neither All nor a state is current, and
// the filter stays removable with plain words.
func TestList_QuickChipCurrent(t *testing.T) {
	l := &stateLister{totals: map[string]int{"draft": 2, "approved": 5}, all: labelledRows()}
	h := r5Env(t, l, nil)
	doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?f=state:=:pending_approval", nil))
	var cur []string
	for _, c := range quickChips(doc) {
		if c.current != "" {
			cur = append(cur, c.text)
		}
	}
	if !slices.Equal(cur, []string{"Sent for sign-off 0"}) {
		t.Errorf("current chips = %q, want the pending state with its label and count", cur)
	}
	if n := doc.Find(`ul[aria-label="Active filters"]`).Length(); n != 0 {
		t.Errorf("%d removable chip groups; the state is the quick chip", n)
	}

	doc = uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?f=state:in:draft&f=state:in:approved", nil))
	for _, c := range quickChips(doc) {
		if c.current != "" {
			t.Errorf("%q is current with two states on", c.text)
		}
	}
	if got := norm(doc.Find(`ul[aria-label="Active filters"] a.chip`).Text()); got != "State: Draft, ApprovedRemove filter" {
		t.Errorf("removable chip = %q", got)
	}
}

// A module that hides the state field gets no quick chips and no State
// group; the panel keeps the other Filterable Select (Kind).
func TestList_QuickChipsHiddenState(t *testing.T) {
	l := &stateLister{totals: map[string]int{"draft": 2}, all: labelledRows()}
	h := r5Env(t, l, map[string]docscreen.Module{labelledType: {Hide: func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "state" }}})
	doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType, nil))
	if n := len(quickChips(doc)); n != 0 {
		t.Errorf("%d quick chips with the state hidden", n)
	}
	if len(l.got) != 1 {
		t.Errorf("%d List calls; with the state hidden only the list is read", len(l.got))
	}
	var legends []string
	doc.Find("details.filterbar-panel legend").Each(func(_ int, s *goquery.Selection) { legends = append(legends, norm(s.Text())) })
	if !slices.Equal(legends, []string{"Kind"}) {
		t.Errorf("panel groups = %q, want Kind only", legends)
	}
}

// R5: the Filters panel has a group per Filterable header Select, ticks
// labelled as users see them and on for the active values; the bar keeps
// as hidden inputs only the filters the ticks don't carry, so a search
// neither doubles nor drops one. The button counts the active filters.
func TestList_FiltersPanel(t *testing.T) {
	l := &stateLister{totals: map[string]int{}, all: labelledRows()}
	h := r5Env(t, l, nil)
	doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?f=kind:=:urgent&f=state:!=:draft&size=50", nil))
	panel := doc.Find("form.filterbar details.filterbar-panel")
	var ticks []string
	panel.Find(`input[type="checkbox"]`).Each(func(_ int, s *goquery.Selection) {
		on := ""
		if _, ok := s.Attr("checked"); ok {
			on = " on"
		}
		ticks = append(ticks, s.AttrOr("name", "")+"="+s.AttrOr("value", "")+" "+norm(s.Closest("label").Text())+on)
	})
	want := []string{
		"f=kind:in:normal normal", "f=kind:in:urgent Very urgent on",
		"f=state:in:draft Draft", "f=state:in:pending_approval Sent for sign-off", "f=state:in:approved Approved",
	}
	if !slices.Equal(ticks, want) {
		t.Errorf("ticks =\n%q\nwant\n%q", ticks, want)
	}
	var kept []string
	doc.Find(`form.filterbar > input[type="hidden"]`).Each(func(_ int, s *goquery.Selection) {
		kept = append(kept, s.AttrOr("name", "")+"="+s.AttrOr("value", ""))
	})
	if want := []string{"f=state:!=:draft", "size=50"}; !slices.Equal(kept, want) {
		t.Errorf("hidden inputs = %q, want %q", kept, want)
	}
	if got := norm(panel.Find("summary").Text()); got != "Filters 2 on" {
		t.Errorf("summary = %q", got)
	}
}

// R5: the search box's placeholder names the SearchFields as users see them.
func TestList_SearchPlaceholder(t *testing.T) {
	e := newEnv(t, "read")
	e.lister.Set(docscreentest.Sample, twoRows(2))
	doc := uitest.Doc(t, e.get(t, "/doc/"+docscreentest.Sample))
	if got := doc.Find("#filter-q").AttrOr("placeholder", ""); got != "Remarks" {
		t.Errorf("placeholder = %q, want Remarks", got)
	}
	l := &stateLister{all: labelledRows()}
	doc = uitest.Doc(t, uitest.Do(t, r5Env(t, l, nil), http.MethodGet, "/doc/"+labelledType, nil))
	if _, ok := doc.Find("#filter-q").Attr("placeholder"); ok {
		t.Error("a placeholder with no SearchFields")
	}
}

var registerColumnsOnce sync.Once

const columnsType = "docscreentest.R5Columns"

func registerColumns() {
	docscreentest.RegisterSample()
	registerColumnsOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: columnsType, Label: "Columns", PluralLabel: "Columns", Table: "docscreentest_r5columns",
			Submittable: true, TestOnly: true, SearchFields: []string{"note", "number", "supplier_id"},
			Fields: []doctype.Field{
				{Name: "number", Label: "PO number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: docscreentest.Supplier, ListPriority: 4},
				{Name: "order_id", Label: "Order", Type: doctype.Link, Link: docscreentest.Sample, ListPriority: 4},
				{Name: "at", Label: "At", Type: doctype.Timestamp, ListPriority: 4},
				{Name: "note", Label: "Note", Type: doctype.Text, ListPriority: 5},
				{Name: "due", Label: "Due", Type: doctype.Date, ListPriority: 5},
				{Name: "maker_id", Label: "Maker", Type: doctype.Link, Link: docscreentest.Supplier, ListPriority: 6},
				{Name: "lines", Label: "Lines", Type: doctype.Int},
				{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Filterable: true, ListPriority: 3, Options: []string{
					string(doctype.StateClosed), string(doctype.StateApproved), string(doctype.StateRejected),
					string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateCancelled),
				}},
			},
		})
	})
}

// R5 (GRN M3): document numbers and dates are NoWrap, so they never break in
// aligned rows; a Link to a master (a name) is Wide; a master list's title
// (a name), a plain text column and low-priority columns (an empty track
// below 64rem) are neither.
func TestListColumns_NoWrapAndWide(t *testing.T) {
	registerColumns()
	dt, _ := doctype.Lookup(columnsType)
	var got []string
	for _, c := range docscreen.ListColumns(dt) {
		got = append(got, c.Key+map[bool]string{true: " nowrap", false: ""}[c.NoWrap]+map[bool]string{true: " wide", false: ""}[c.Wide])
	}
	want := []string{"number nowrap", "state", "supplier_id wide", "order_id nowrap", "at nowrap", "note", "due", "maker_id"}
	// state is the declared Select at priority 3: a plain column.
	if !slices.Equal(got, want) {
		t.Errorf("columns = %q, want %q", got, want)
	}

	master := &doctype.DocType{Name: "docscreentest.R5Master", Fields: []doctype.Field{{Name: "name", Label: "Name", Type: doctype.Text, ListPriority: 1}}}
	if c := docscreen.ListColumns(master); len(c) != 1 || c[0].NoWrap {
		t.Errorf("a non-Submittable title = %+v, want a plain column: names wrap", c)
	}
}

// R5: quick chips offer only the states work moves through (draft, pending,
// approved), in the state field's declared order and worded as the list's
// badge; closed, rejected and cancelled, with records, stay in the panel.
// The placeholder keeps an abbreviation's capitals (PO number).
func TestList_QuickChipsWorkingStatesOnly(t *testing.T) {
	l := &stateLister{totals: map[string]int{"closed": 4, "approved": 1, "rejected": 2, "draft": 3, "pending_approval": 5, "cancelled": 6}, all: labelledRows()}
	h := r5Env(t, l, nil)
	doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+columnsType, nil))
	var got []string
	for _, c := range quickChips(doc) {
		got = append(got, c.text)
	}
	if want := []string{"All", "Approved 1", "Draft 3", "Pending approval 5"}; !slices.Equal(got, want) {
		t.Errorf("quick chips = %q, want %q", got, want)
	}
	if n := doc.Find(`details.filterbar-panel input[value="state:in:closed"]`).Length(); n != 1 {
		t.Errorf("%d closed ticks in the panel, want 1", n)
	}
	if got := doc.Find("#filter-q").AttrOr("placeholder", ""); got != "Note, PO number or supplier" {
		t.Errorf("placeholder = %q", got)
	}
	if got := norm(doc.Find(`details.filterbar-panel input[value="state:in:pending_approval"]`).Closest("label").Text()); got != "Pending approval" {
		t.Errorf("pending tick = %q, want the badge's word", got)
	}

	// A field the module hides leaves the placeholder.
	h = r5Env(t, l, map[string]docscreen.Module{columnsType: {Hide: func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "note" }}})
	doc = uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+columnsType, nil))
	if got := doc.Find("#filter-q").AttrOr("placeholder", ""); got != "PO number or supplier" {
		t.Errorf("placeholder with note hidden = %q", got)
	}
}

// failLister lists, but every Count fails.
type failLister struct{ stateLister }

func (l *failLister) Count(context.Context, auth.Principal, *doctype.DocType, docscreen.ListQuery) (int, error) {
	return 0, errors.New("disk on fire")
}

// A quick chip count that fails fails the page: a chip never shows a count
// it couldn't read.
func TestList_QuickChipCountFails(t *testing.T) {
	registerLabelled()
	l := &failLister{stateLister: stateLister{all: labelledRows()}}
	az := authfake.New()
	az.Grant("Buyer", labelledType, "read", auth.ScopeAll)
	az.AddRole(priya.ID, "Buyer")
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{
		DB: &docscreentest.Reader{}, Engine: docfake.New(), Auth: az, Approval: &approver{},
		Lister: l, Extras: docscreentest.NewExtras(),
		Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
	})(r)
	rec := uitest.Do(t, r, http.MethodGet, "/doc/"+labelledType, nil)
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "datalist-rows") {
		t.Errorf("status = %d; want 500 and no list", rec.Code)
	}
}

// CD-394 (2): a one-value state filter with no quick chip (closed) stays a
// removable chip, worded as the badge, and All isn't current.
func TestList_StateWithoutQuickChipIsRemovable(t *testing.T) {
	l := &stateLister{totals: map[string]int{"draft": 3, "closed": 2}, all: labelledRows()}
	doc := uitest.Doc(t, uitest.Do(t, r5Env(t, l, nil), http.MethodGet, "/doc/"+columnsType+"?f=state:in:closed", nil))
	if got := norm(doc.Find(`ul[aria-label="Active filters"] a.chip`).Text()); got != "State: ClosedRemove filter" {
		t.Errorf("removable chips = %q, want State: Closed", got)
	}
	for _, c := range quickChips(doc) {
		if c.current != "" {
			t.Errorf("%q is current with closed on", c.text)
		}
	}
}

// A quick chip is current for the in filter its link sets, and only = or in
// make one: a not-draft filter stays a removable chip with nothing current.
func TestList_QuickChipOnlyForInOrEquals(t *testing.T) {
	l := &stateLister{totals: map[string]int{"draft": 2, "approved": 5}, all: labelledRows()}
	h := r5Env(t, l, nil)
	doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?f=state:in:draft", nil))
	var cur []string
	for _, c := range quickChips(doc) {
		if c.current != "" {
			cur = append(cur, c.text)
		}
	}
	if !slices.Equal(cur, []string{"Draft 2"}) {
		t.Errorf("current chips = %q, want Draft 2", cur)
	}
	if n := doc.Find(`ul[aria-label="Active filters"]`).Length(); n != 0 {
		t.Errorf("%d removable chip groups; the state is the quick chip", n)
	}

	doc = uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+labelledType+"?f=state:!=:draft", nil))
	for _, c := range quickChips(doc) {
		if c.current != "" {
			t.Errorf("%q is current with not draft on", c.text)
		}
	}
	if got := norm(doc.Find(`ul[aria-label="Active filters"] a.chip`).Text()); got != "State: not DraftRemove filter" {
		t.Errorf("removable chip = %q, want State: not Draft", got)
	}
}

// Removable chips put the comparison operators in words, by field type:
// dates before / up to / after / from, numbers under / up to / over / from;
// other fields say the operator itself.
func TestList_ChipOperatorWords(t *testing.T) {
	l := &stateLister{all: labelledRows()}
	h := r5Env(t, l, nil)
	for _, c := range []struct{ dt, filter, want string }{
		{columnsType, "at:%3C:2026-10-01", "At: before 2026-10-01"},
		{columnsType, "at:%3C%3D:2026-10-01", "At: up to 2026-10-01"},
		{columnsType, "at:%3E:2026-10-01", "At: after 2026-10-01"},
		{columnsType, "at:%3E%3D:2026-10-01", "At: from 2026-10-01"},
		{columnsType, "due:%3C:2026-10-01", "Due: before 2026-10-01"},
		{columnsType, "at:on%20or%20after:2026-10-01", "At: on or after 2026-10-01"},
		{columnsType, "at:not%20in:2026-10-01", "At: not 2026-10-01"},
		{columnsType, "note:%3C:m", "Note: < m"},
		{columnsType, "lines:%3E:3", "Lines: over 3"},
		{labelledType, "amount:%3C:500", "Amount: under 500"},
		{labelledType, "amount:%3C%3D:500", "Amount: up to 500"},
		{labelledType, "amount:%3E:500", "Amount: over 500"},
		{labelledType, "amount:%3E%3D:500", "Amount: from 500"},
	} {
		doc := uitest.Doc(t, uitest.Do(t, h, http.MethodGet, "/doc/"+c.dt+"?f="+c.filter, nil))
		if got := norm(doc.Find(`ul[aria-label="Active filters"] a.chip`).Text()); got != c.want+"Remove filter" {
			t.Errorf("%s: chip = %q, want %q", c.filter, got, c.want)
		}
	}
}
