package docscreen_test

import (
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// m38 DocTypes whose state field list can't filter: one not Filterable, one
// Filterable but OnLine. Each fails one half of Screens' filter rule.
const (
	m38NotFilterable = "docscreentest.M38NotFilterable"
	m38OnLine        = "docscreentest.M38OnLine"
)

var m38RegisterOnce sync.Once

func m38Register() {
	m38RegisterOnce.Do(func() {
		states := []string{string(doctype.StateDraft), string(doctype.StatePending), string(doctype.StateApproved)}
		doctype.Register(doctype.DocType{
			Name: m38NotFilterable, Label: "M38 not filterable", PluralLabel: "M38 not filterables", Table: "m38_not_filterable", LinesTable: "m38_not_filterable_line",
			Submittable: true, Hooks: authtest.NopHooks{},
			Fields: []doctype.Field{{Name: "state", Label: "State", Type: doctype.Select, ReadOnly: true, Options: states}},
		})
		doctype.Register(doctype.DocType{
			Name: m38OnLine, Label: "M38 on line", PluralLabel: "M38 on lines", Table: "m38_on_line", LinesTable: "m38_on_line_line",
			Submittable: true, Hooks: authtest.NopHooks{},
			Fields: []doctype.Field{{Name: "state", Label: "State", Type: doctype.Select, Filterable: true, OnLine: true, Options: states}},
		})
	})
}

// M0-38: Screens gives a DocType a "list filtered" screen exactly when list
// accepts a filter on its state field; list.Build is the oracle. Order has
// one; Receipt and the two m38 DocTypes (state not Filterable, state OnLine)
// have none, so both sides and both parts of the rule are tested.
func TestScreens_FilteredOnlyWithStateFilter(t *testing.T) {
	m38Register()
	ids := docscreen.SampleIDs{Draft: 1, Pending: 2, Approved: 3, Rejected: 4, Cancelled: 5, Closed: 6, Lines500: 7, CloseAs: "priya@vardhan.test"}
	hasFiltered := func(name string) bool {
		for _, s := range docscreen.Screens(name, ids) {
			if s.Name == name+" list filtered" {
				return true
			}
		}
		return false
	}
	wantFilterable := map[string]bool{fixture.Order: true, fixture.Receipt: false, m38NotFilterable: false, m38OnLine: false}
	seen := map[string]bool{}
	for _, dt := range doctype.All() {
		if !dt.IsDocument() {
			continue
		}
		q := list.Query{Page: 1, PageSize: 25, Filters: []list.Filter{{Field: "state", Op: list.OpIn, Values: []string{string(doctype.StateApproved)}}}}
		_, _, err := list.Build(dt, q, auth.Predicate{})
		want := err == nil
		if got := hasFiltered(dt.Name); got != want {
			t.Errorf("%s: list filtered screen %v, want %v (list.Build error: %v)", dt.Name, got, want, err)
		}
		if w, ok := wantFilterable[dt.Name]; ok {
			seen[dt.Name] = true
			if w != want {
				t.Errorf("%s: list accepts a state filter %v, want %v", dt.Name, want, w)
			}
		}
	}
	for name := range wantFilterable {
		if !seen[name] {
			t.Errorf("%s is not a registered document DocType", name)
		}
	}
	// An unregistered name is taken to have every state, so it is filtered.
	if !hasFiltered("no.such") {
		t.Error("Screens(no.such) has no list filtered screen; an unregistered name keeps it")
	}
}
