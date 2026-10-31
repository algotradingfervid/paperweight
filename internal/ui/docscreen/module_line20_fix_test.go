package docscreen_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// TestM20Fix_AddTopNoLinePages: with NoLinePages the Items step has no add
// button, AddTop's repeat after the totals included (CD-256 (7), CD-275).
func TestM20Fix_AddTopNoLinePages(t *testing.T) {
	e, id := m20Env(t, 4, docscreen.Module{})
	m01bMount(e, docscreentest.Hooked, docscreen.Module{AddTop: true, NoLinePages: true, LineNoun: m20Noun})
	for _, hx := range []bool{false, true} {
		what := "AddTop and NoLinePages with 4 lines, htmx " + strconv.FormatBool(hx)
		page := m01bOK(t, e.get(t, m01bURL(id)+"/edit/items", m20Hdr(hx)...), what)
		if n := page.Find("[data-add-line]").Length(); n != 0 {
			t.Errorf("%s: %d add buttons, want none", what, n)
		}
		if n := m20Adds(page, id).Length(); n != 0 {
			t.Errorf("%s: %d links to the new-line page, want none", what, n)
		}
	}
}
