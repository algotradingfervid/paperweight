package docscreen_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// CD-327 (3): the list and search pages leave out the header and the cells
// of a column whose field Module.Hide hides when asked with Listing true and
// no View. The control run without the hook shows both.
func TestHide54_ListColumns(t *testing.T) {
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if c.Listing && c.View != nil {
			t.Errorf("%s: Hide with Listing true has a View", c.Req.URL)
		}
		return c.Listing && f.Name == "order_date"
	}
	for _, c := range []struct {
		name  string
		m     docscreen.Module
		shown bool
	}{
		{"no hook", docscreen.Module{}, true},
		{"order date hidden on listing", docscreen.Module{Hide: hide}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, _ := m46Order(t, c.m)
			e.d.Lister.(*docscreentest.Lister).Set(fixture.Order, types.Result[docscreen.ListRow]{Total: 1, Rows: []docscreen.ListRow{{
				Info:   doctype.Info{DocType: fixture.Order, ID: 1, State: doctype.StateDraft, Display: "Draft 1"},
				Values: map[string]string{"supplier_id": itoa(e.fx.M.Party["SHREE"]), "order_date": "2026-10-25"},
			}}})
			for _, u := range []string{"/doc/" + fixture.Order, "/search?q=m54"} {
				page := m01bOK(t, e.get(t, u), "GET "+u)
				text := norm(page.Find("#main").Text())
				if !strings.Contains(text, "Supplier") {
					t.Fatalf("GET %s: no Supplier column in %q; the page shows no Orders", u, text)
				}
				for _, s := range []string{"Order date", "25-10-2026"} {
					if strings.Contains(text, s) != c.shown {
						t.Errorf("GET %s: page shows %q = %v, want %v", u, s, !c.shown, c.shown)
					}
				}
			}
		})
	}
}

// CD-327 (3): PartyListed keeps the party of a DocType that isn't
// registered, rather than asking a Hide about it.
func TestHide54_PartyListedUnknown(t *testing.T) {
	d := docscreen.Deps{Modules: map[string]docscreen.Module{"m54.Unknown": {Hide: func(docscreen.Ctx, doctype.Field) bool { return true }}}}
	if !d.PartyListed(t.Context(), auth.Principal{}, "m54.Unknown") {
		t.Error("PartyListed(m54.Unknown) = false, want true for a DocType that isn't registered")
	}
}

// m54Sorted is a DocType whose first DefaultSort key, Secret, is the field
// the test module hides on the list (CD-334).
const m54Sorted = "m54.Sorted"

var m54Once sync.Once

func registerM54Sorted() {
	m54Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m54Sorted, Label: "Sorted", PluralLabel: "Sorteds", Table: "m54_sorted", TestOnly: true,
			// note is a DefaultSort key the kernel can't sort by on request
			// (not Sortable), so the fallback skips it.
			DefaultSort: []doctype.SortKey{{Field: "secret", Desc: true}, {Field: "note"}},
			Fields: []doctype.Field{
				{Name: "label", Label: "Label", Type: doctype.Text, Sortable: true, ListPriority: 1},
				{Name: "secret", Label: "Secret", Type: doctype.Text, Sortable: true, Filterable: true, ListPriority: 4},
				{Name: "note", Label: "Note", Type: doctype.Text},
			},
		})
	})
}

// CD-334 (2), (3): on the list, a field Hide hides with Listing true has no
// sort option; a URL sort or filter on it is dropped before the Lister runs
// and draws no chip; a DefaultSort on it falls back to the next sort option
// on a Sortable field, past the non-Sortable second DefaultSort key. The control run without the hook keeps all of them.
func TestHide54_ListSortFilter(t *testing.T) {
	registerM54Sorted()
	hide := func(c docscreen.Ctx, f doctype.Field) bool { return c.Listing && f.Name == "secret" }
	for _, c := range []struct {
		name                string
		m                   docscreen.Module
		urlSort, defaultSrt string // the Lister's Sort for a URL sort on Secret and for none
		filters             int    // the Lister's filters for a URL filter on Secret
	}{
		{"no hook", docscreen.Module{}, "secret.asc", "", 1},
		{"secret hidden on listing", docscreen.Module{Hide: hide}, "label.desc", "label.desc", 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, "read")
			e.az.Grant("Buyer", m54Sorted, "read", auth.ScopeAll)
			e.d.Modules = map[string]docscreen.Module{m54Sorted: c.m}
			e.remount()
			lister := e.d.Lister.(*docscreentest.Lister)
			lister.Set(m54Sorted, types.Result[docscreen.ListRow]{Total: 1, Rows: []docscreen.ListRow{{
				Info:   doctype.Info{DocType: m54Sorted, ID: 1, Display: "Row 1"},
				Values: map[string]string{"label": "first", "secret": "zq-secret"},
			}}})

			page := m01bOK(t, e.get(t, "/doc/"+m54Sorted+"?sort=secret.asc&f=secret:=:zq-secret"), "GET with a sort and filter on Secret")
			if q := lister.Last(); q.Sort != c.urlSort || len(q.Filters) != c.filters {
				t.Errorf("Lister query = sort %q, %d filters; want sort %q, %d filters", q.Sort, len(q.Filters), c.urlSort, c.filters)
			}
			var opts []string
			page.Find("#filter-sort option").Each(func(_ int, s *goquery.Selection) { opts = append(opts, norm(s.Text())) })
			if len(opts) == 0 {
				t.Fatal("the list draws no sort option")
			}
			wantOpt := c.filters == 1
			if got := slices.ContainsFunc(opts, func(o string) bool { return strings.HasPrefix(o, "Secret") }); got != wantOpt {
				t.Errorf("sort options %q: a Secret option = %v, want %v", opts, got, wantOpt)
			}
			if got := strings.Contains(norm(page.Find("#main").Text()), "Secret: zq-secret"); got != wantOpt {
				t.Errorf("the Secret chip drawn = %v, want %v", got, wantOpt)
			}

			m01bOK(t, e.get(t, "/doc/"+m54Sorted), "GET with no sort")
			if q := lister.Last(); q.Sort != c.defaultSrt {
				t.Errorf("Lister Sort with no URL sort = %q, want %q", q.Sort, c.defaultSrt)
			}
		})
	}
}
