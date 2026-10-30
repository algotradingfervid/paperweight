package docscreen_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The header form shows only from 64rem, so the page has its own input in
// every state, holding the text that was searched.
func TestSearch_PageHasItsOwnInputInEveryState(t *testing.T) {
	for _, c := range []struct {
		name, target, want string
		rows               bool
	}{
		{name: "blank", target: "/search"},
		{name: "results", target: "/search?q=steel", want: "steel", rows: true},
		{name: "no match", target: "/search?q=zzz", want: "zzz"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			if c.rows {
				e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))
			}
			rec := e.get(t, c.target)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			doc := uitest.Doc(t, rec)
			form := doc.Find(`main form[action="/search"]`)
			if form.Length() != 1 {
				t.Fatalf("%d search forms in main, want 1", form.Length())
			}
			if got := form.AttrOr("method", ""); got != "get" {
				t.Errorf("method = %q, want get", got)
			}
			input := form.Find(`input[name="q"]`)
			if input.Length() != 1 {
				t.Fatalf("%d q inputs, want 1", input.Length())
			}
			if got := input.AttrOr("value", ""); got != c.want {
				t.Errorf("value = %q, want %q", got, c.want)
			}
			label := form.Find(`label[for="` + input.AttrOr("id", "-") + `"]`)
			if got := squeeze(label.Text()); got != "Search" {
				t.Errorf("label = %q, want Search", got)
			}
		})
	}
}

func TestSearch_SeeAllNamesThePluralLabel(t *testing.T) {
	e := newSearchEnv(t, srchOrder)
	e.lister.set(srchOrder, srchRows(srchOrder, 6)...)

	_, doc := e.search(t, "steel")
	g := wantGroup(t, doc, "Search Orders", 6)
	if want := "See all 6 Search Orders"; g.seeAllText != want {
		t.Errorf("See all text = %q, want %q", g.seeAllText, want)
	}
}
