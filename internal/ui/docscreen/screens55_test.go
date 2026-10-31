package docscreen_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// M0-55 (CD-329): m55NoLines' Module sets NoLinePages; m55Lines beside it has
// a Module without it (CardsOnly), so a skip that fires for any Module fails.
const (
	m55NoLines = "m55.NoLines"
	m55Lines   = "m55.Lines"
)

var m55Once sync.Once

func m55Register() {
	m55Once.Do(func() {
		for _, n := range []string{m55NoLines, m55Lines} {
			t := strings.ToLower(strings.ReplaceAll(n, ".", "_"))
			doctype.Register(doctype.DocType{
				Name: n, Label: n, PluralLabel: n + "s", Table: t, LinesTable: t + "_line",
				Submittable: true, Hooks: authtest.NopHooks{},
				Fields: []doctype.Field{
					{Name: "remarks", Label: "Remarks", Type: doctype.Text, MaxLen: 500},
					{Name: "note", Label: "Note", Type: doctype.Text, OnLine: true},
				},
			})
		}
	})
}

var m55Deps = docscreen.Deps{Modules: map[string]docscreen.Module{
	m55NoLines: {NoLinePages: true},
	m55Lines:   {CardsOnly: true},
}}

var m55IDs = docscreen.SampleIDs{Draft: 1, Pending: 2, Approved: 3, Rejected: 4, Cancelled: 5, Closed: 6, Lines500: 7}

var m55LineScreens = []string{"line new", "line", "line remove"}

func m55Names(list []screens.Screen, dt string) []string {
	var out []string
	for _, s := range list {
		if n, ok := strings.CutPrefix(s.Name, dt+" "); ok && slices.Contains(m55LineScreens, n) {
			out = append(out, n)
		}
	}
	return out
}

func TestScreens_NoLinePagesLeavesOutLineScreens(t *testing.T) {
	m55Register()
	if got := m55Names(docscreen.Screens(m55NoLines, m55IDs, m55Deps), m55NoLines); len(got) != 0 {
		t.Errorf("Screens(%s, NoLinePages) has line screens %q; want none (CD-329)", m55NoLines, got)
	}
	for _, c := range []struct {
		name string
		list []screens.Screen
	}{
		{"CardsOnly module", docscreen.Screens(m55Lines, m55IDs, m55Deps)},
		{"no Deps", docscreen.Screens(m55Lines, m55IDs)},
	} {
		if got := m55Names(c.list, m55Lines); !slices.Equal(got, m55LineScreens) {
			t.Errorf("Screens(%s, %s) line screens = %q; want %q", m55Lines, c.name, got, m55LineScreens)
		}
	}
	if got := m55Names(docscreen.Screens(m55NoLines, m55IDs), m55NoLines); !slices.Equal(got, m55LineScreens) {
		t.Errorf("Screens(%s) without Deps line screens = %q; want %q", m55NoLines, got, m55LineScreens)
	}
}

func TestUnswept_NoLinePagesNeedsNoLineScreens(t *testing.T) {
	m55Register()
	list := slices.Concat(docscreen.Screens(m55NoLines, m55IDs, m55Deps), docscreen.Screens(m55Lines, m55IDs, m55Deps))
	got := docscreen.Unswept(list, m55Deps)
	if slices.Contains(got, m55NoLines) || slices.Contains(got, m55Lines) {
		t.Errorf("Unswept(both swept, Deps) = %q; want neither %s nor %s", got, m55NoLines, m55Lines)
	}
	if got := docscreen.Unswept(list); !slices.Contains(got, m55NoLines) {
		t.Errorf("Unswept(both swept) without Deps = %q; want %s named, its line screens missing", got, m55NoLines)
	}
	noLines := slices.DeleteFunc(slices.Clone(list), func(s screens.Screen) bool {
		return strings.Contains(s.URL, "/lines/")
	})
	if got := docscreen.Unswept(noLines, m55Deps); !slices.Contains(got, m55Lines) {
		t.Errorf("Unswept(no line screens, Deps) = %q; want %s named, it keeps its line pages", got, m55Lines)
	}
}
