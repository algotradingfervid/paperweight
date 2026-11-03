package mstweb_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// hiddenInputs is the bar form's own hidden inputs as name=value.
func hiddenInputs(r webtest.Response) []string {
	return r.Doc.Find(`form.filterbar > input[type="hidden"]`).Map(func(_ int, s *goquery.Selection) string {
		return s.AttrOr("name", "") + "=" + s.AttrOr("value", "")
	})
}

// R5 (UI v2 §5.12, shell M5): a master list's search has a placeholder from
// its SearchFields; the Filters panel holds the disabled tick (on when
// disabled records are listed, so a search keeps them without a hidden
// input) and, for phones, Export; the code sits apart from the name.
func TestScreens_ListFiltersPanel(t *testing.T) {
	h := harness(t, stubService{}, onStatusDocType)
	kavya := h.Env.Kavya
	id := seedItem(t, h, "M12")
	for _, c := range []struct {
		url, summary string
		checked      bool
	}{{base, "Filters", false}, {base + "?include_disabled=true", "Filters 1 on", true}} {
		r := h.Get(t, kavya, c.url)
		webtest.WantStatus(t, r, http.StatusOK)
		if got := r.Doc.Find("#filter-q").AttrOr("placeholder", ""); got != "Code or name" {
			t.Errorf("%s placeholder = %q, want Code or name", c.url, got)
		}
		box := r.Doc.Find(`form.filterbar details.filterbar-panel input[type="checkbox"][name="include_disabled"][value="true"]`)
		if box.Length() != 1 {
			t.Fatalf("%s: %d disabled ticks in the Filters panel, want 1", c.url, box.Length())
		}
		if got := webtest.Squash(r.Doc.Find("details.filterbar-panel summary").Text()); got != c.summary {
			t.Errorf("%s summary = %q, want %q", c.url, got, c.summary)
		}
		if _, on := box.Attr("checked"); on != c.checked {
			t.Errorf("%s: tick on = %v, want %v", c.url, on, c.checked)
		}
		if got := hiddenInputs(r); len(got) != 0 {
			t.Errorf("%s hidden inputs = %q; the tick carries include_disabled", c.url, got)
		}
		if r.Doc.Find(`details.filterbar-panel a[href^="`+base+`/export.csv"]`).Length() != 1 {
			t.Errorf("%s: no Export in the Filters panel", c.url)
		}
		if got := webtest.Squash(r.Doc.Find(`a[href="` + stubPath(id) + `"] .datalist-code`).Text()); got != "M12" {
			t.Errorf("%s: code span = %q, want M12 apart from the name", c.url, got)
		}
	}
}

// A master with no Disable has no disabled tick, so no Filters panel; a
// listed include_disabled stays a hidden input, and only then.
func TestScreens_ListNoDisableKeepsParam(t *testing.T) {
	h := harness(t, stubService{}, onStatusDocType, func(s *screen) { s.NoDisable = true })
	if got := hiddenInputs(h.Get(t, h.Env.Kavya, base)); len(got) != 0 {
		t.Errorf("hidden inputs on the plain list = %q", got)
	}
	r := h.Get(t, h.Env.Kavya, base+"?include_disabled=true")
	webtest.WantStatus(t, r, http.StatusOK)
	if n := r.Doc.Find("details.filterbar-panel").Length(); n != 0 {
		t.Errorf("%d Filters panels with nothing to tick", n)
	}
	if got := hiddenInputs(r); !slices.Equal(got, []string{"include_disabled=true"}) {
		t.Errorf("hidden inputs = %q", got)
	}
	if !strings.Contains(webtest.Squash(r.Doc.Find("summary, .filterbar").Text()), "Disabled included") {
		t.Error("no Disabled included chip")
	}
}

// R5: ListExtra columns follow the DocType's, each row's text from
// ListValues as the viewer; a ListValues error fails the page.
func TestScreens_ListExtraColumns(t *testing.T) {
	var fail error
	var seen auth.Principal
	extra := func(s *screen) {
		s.ListExtra = []components.Column{{Key: "city", Label: "City", Priority: components.PMeta}}
		s.ListValues = func(_ context.Context, _ *db.Tx, p auth.Principal, row list.Row) (map[string]string, error) {
			seen = p
			code, _ := row.Values["code"].(string)
			return map[string]string{"city": "City of " + code}, fail
		}
	}
	h := harness(t, stubService{}, onStatusDocType, extra)
	id := seedItem(t, h, "M12")
	r := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, r, http.StatusOK)
	if got, want := listHead(r), []string{"Name", "Kind", "City"}; !slices.Equal(got, want) {
		t.Errorf("columns = %q, want %q", got, want)
	}
	row, _ := r.RowLink(stubPath(id))
	if !strings.Contains(row, "City of M12") {
		t.Errorf("row = %q, want the ListValues text", row)
	}
	if seen.ID != h.Env.Kavya.ID {
		t.Errorf("ListValues saw user %v, want the viewer %v", seen.ID, h.Env.Kavya.ID)
	}
	fail = errors.New("disk on fire")
	if r := h.Get(t, h.Env.Kavya, base); r.Status != http.StatusInternalServerError {
		t.Errorf("a failing ListValues: status %d, want 500", r.Status)
	}
}

// Registration refuses a ListExtra that takes the list past DataList's
// limit (8 columns).
func TestScreens_ListExtraTooManyColumnsPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), statusDocType) {
			t.Errorf("recover() = %v, want a panic naming the DocType", r)
		}
	}()
	harness(t, stubService{}, onStatusDocType, func(s *screen) {
		for i := range 6 {
			s.ListExtra = append(s.ListExtra, components.Column{Key: "x" + strconv.Itoa(i), Label: "X", Priority: components.PMeta})
		}
	})
}
