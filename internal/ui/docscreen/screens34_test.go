package docscreen_test

import (
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// M0-34: every list URL Screens gives a DocType is one list accepts. The
// filtered screen filters on the state convention column, so it is checked
// for each registered document DocType that declares it (fixture.Order and
// fixture.Claim at least); list.Build is the real query check.
func TestScreens_ListURLsAreAccepted(t *testing.T) {
	ids := docscreen.SampleIDs{Draft: 1, Pending: 2, Approved: 3, Rejected: 4, Cancelled: 5, Closed: 6, Lines500: 7, CloseAs: "priya@vardhan.test"}
	checked := map[string]bool{}
	for _, dt := range doctype.All() {
		if !dt.IsDocument() || !hasStateFilter(dt) {
			continue
		}
		checked[dt.Name] = true
		for _, s := range docscreen.Screens(dt.Name, ids) {
			path, raw, ok := strings.Cut(s.URL, "?")
			if !ok || path != "/doc/"+dt.Name {
				continue
			}
			v, err := url.ParseQuery(raw)
			if err != nil {
				t.Fatalf("%s: %v", s.Name, err)
			}
			q := docscreen.ParseListQuery(v)
			sort, err := list.ParseSort(dt, strings.Replace(q.Sort, ".", " ", 1))
			if err != nil {
				t.Errorf("%s (%s): list refuses the sort: %v", s.Name, s.URL, err)
				continue
			}
			lq := list.Query{Search: q.Search, Sort: sort, Page: max(q.Page, 1), PageSize: q.Size}
			if lq.PageSize == 0 {
				lq.PageSize = 25
			}
			for _, f := range q.Filters {
				lq.Filters = append(lq.Filters, list.Filter{Field: f.Field, Op: list.Op(f.Op), Values: f.Values})
			}
			if _, _, err := list.Build(dt, lq, auth.Predicate{}); err != nil {
				t.Errorf("%s (%s): list refuses it: %v", s.Name, s.URL, err)
			}
		}
	}
	for _, name := range []string{fixture.Order, fixture.Claim} {
		if !checked[name] {
			t.Errorf("%s was not checked; want it registered with a filterable state field", name)
		}
	}
}

func hasStateFilter(dt *doctype.DocType) bool {
	for _, f := range dt.Fields {
		if f.Name == "state" && f.Filterable && !f.OnLine {
			return true
		}
	}
	return false
}
