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

// M0-34: every list URL Screens gives a DocType is one list accepts,
// checked for every registered document DocType (M0-38: also those with no
// filterable state field, which get no filtered screen); list.Build is the
// real query check. fixture.Order and fixture.Claim keep their filtered one.
func TestScreens_ListURLsAreAccepted(t *testing.T) {
	ids := docscreen.SampleIDs{Draft: 1, Pending: 2, Approved: 3, Rejected: 4, Cancelled: 5, Closed: 6, Lines500: 7, CloseAs: "priya@vardhan.test"}
	checked, filtered := map[string]bool{}, map[string]bool{}
	for _, dt := range doctype.All() {
		if !dt.IsDocument() {
			continue
		}
		checked[dt.Name] = true
		for _, s := range docscreen.Screens(dt.Name, ids) {
			if s.Name == dt.Name+" list filtered" {
				filtered[dt.Name] = true
			}
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
		if !checked[name] || !filtered[name] {
			t.Errorf("%s: checked %v, list filtered screen %v; want both", name, checked[name], filtered[name])
		}
	}
}
