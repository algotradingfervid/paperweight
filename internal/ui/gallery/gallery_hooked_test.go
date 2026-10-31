package gallery

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-5 (hooks spec §6): the gallery's one hooked sample. Every document
// DocType the gallery registers is swept: today that is Hooked (Sample has
// no Hooks, so it isn't a document DocType).
func TestGallery_EveryDocTypeSwept(t *testing.T) {
	Handler() // registers the gallery's DocTypes
	all := Screens("")
	if dt, ok := doctype.Lookup(docscreentest.Hooked); !ok || !dt.IsDocument() {
		t.Fatalf("Hooked registered %v; want a document DocType the check below covers", ok)
	}
	if missing := docscreen.Unswept(all); len(missing) != 0 {
		t.Errorf("document DocTypes the gallery doesn't sweep: %q", missing)
	}
	for _, w := range docscreen.Screens(docscreentest.Hooked, hookedIDs) {
		i := slices.IndexFunc(all, func(s screens.Screen) bool { return s.Name == w.Name })
		if i < 0 {
			t.Errorf("Screens has no %q", w.Name)
		} else if all[i].URL != w.URL || all[i].As != "" || (all[i].Setup == nil) != (w.Setup == nil) {
			t.Errorf("%q = %+v, want URL %q, no As, Setup kept", w.Name, all[i], w.URL)
		}
	}
	for _, n := range []string{"step terms", "details 422", "details 409", "read-only draft", "close", "cancel", "items 500"} {
		if !slices.ContainsFunc(all, func(s screens.Screen) bool { return s.Name == docscreentest.Hooked+" "+n }) {
			t.Errorf("Screens has no %q", docscreentest.Hooked+" "+n)
		}
	}
}

func hookedURL(id int64, path string) string {
	return "/doc/" + docscreentest.Hooked + "/" + strconv.FormatInt(id, 10) + path
}

// Each visible hook shows on its page, so the sweep checks its region.
func TestGallery_HookedShowsEveryHook(t *testing.T) {
	h := Handler()
	draft, approved := hookedIDs.Draft, hookedIDs.Approved
	for _, c := range []struct {
		url   string
		texts []string
	}{
		{hookedURL(draft, "/edit/details"), []string{hookedBodyTitle, hookedLeadLabel, hookedBarPrimary, hookedDetailsLabel}},
		{"/doc/" + docscreentest.Hooked + "/new", []string{hookedBodyTitle, hookedBarPrimary}},
		{hookedURL(draft, "/edit/terms"), []string{hookedTop, "Reference"}},
		{hookedURL(draft, "/edit/items"), []string{hookedItemsLabel, hookedLineNote, "ST-001"}},
		{hookedURL(draft, "/lines/1"), []string{hookedLineTitle, "Quantity", "Note"}},
		{hookedURL(draft, "/lines/new"), []string{hookedLineTitle}},
		{hookedURL(draft, "/edit/check"), []string{hookedCheckNote, hookedCheckWarn, hookedCheckTotal, hookedCheckCard}},
		{hookedURL(approved, ""), []string{hookedPageWarn, hookedPageNote, hookedPageMore, hookedPagePair, hookedPageCard, uitest.LongError}},
		{hookedURL(hookedIDs.Closed, ""), []string{hookedPagePrimary}},
		{hookedURL(approved, "/cancel"), []string{hookedReasonNote, hookedReasonHint}},
		{hookedURL(approved, "/close"), []string{hookedReasonNote, hookedReasonHint}},
		{hookedURL(hookedReadOnly, ""), []string{hookedPageNote, hookedLineNote}},
	} {
		d := get(t, h, c.url)
		if d == nil {
			continue
		}
		text := d.Text()
		for _, want := range c.texts {
			if !strings.Contains(text, want) {
				t.Errorf("GET %s: no %q", c.url, want)
			}
		}
	}
	// The read-only draft is the document page, not its wizard: its steps
	// send the reader there (CD-263 G1).
	if d := get(t, h, hookedURL(hookedReadOnly, "")); d != nil && d.Find("#wizard-form").Length() != 0 {
		t.Error("the read-only draft shows the wizard form")
	}
	if r := uitest.Do(t, h, http.MethodGet, hookedURL(hookedReadOnly, "/edit/details"), nil); r.Code != http.StatusSeeOther {
		t.Errorf("GET the read-only draft's first step = %d, want 303 to its page", r.Code)
	}
	if r := uitest.Do(t, h, http.MethodGet, hookedURL(draft, ""), nil); r.Code != http.StatusSeeOther || !strings.HasSuffix(r.Header().Get("Location"), "/edit/check") {
		t.Errorf("GET Priya's own draft = %d to %q, want 303 to its check page: write is own-scope, so the read-only page proves nothing otherwise",
			r.Code, r.Header().Get("Location"))
	}
}

// The module's Saves refuse what they can't parse, with a 422 and the typed
// value kept, and nothing reaches DOC.
func TestGallery_HookedSavesRefuse(t *testing.T) {
	h := Handler()
	step := uitest.Do(t, h, http.MethodPost, hookedURL(hookedIDs.Draft, "/edit/details"),
		url.Values{"version": {"1"}, "supplier_id": {"1"}, "site_id": {"1"}, hookedLeadField: {"seven"}, "csrf": {"gallery"}})
	if step.Code != http.StatusUnprocessableEntity || !strings.Contains(step.Body.String(), hookedLeadError) || !strings.Contains(step.Body.String(), `value="seven"`) {
		t.Errorf("details POST with a bad delivery time = %d, want 422 with %q and the typed value; body %.400s", step.Code, hookedLeadError, step.Body)
	}
	ok := uitest.Do(t, h, http.MethodPost, hookedURL(hookedIDs.Draft, "/edit/details"),
		url.Values{"version": {"1"}, "supplier_id": {"1"}, "site_id": {"1"}, hookedLeadField: {"7"}, "csrf": {"gallery"}})
	if ok.Code != http.StatusConflict {
		t.Errorf("details POST with a good delivery time = %d, want the gallery's 409 from DOC", ok.Code)
	}
	line := uitest.Do(t, h, http.MethodPost, hookedURL(hookedIDs.Draft, "/lines/1"),
		url.Values{"version": {"1"}, "item_id": {"1"}, "qty": {"x"}, "note": {"n"}, "csrf": {"gallery"}})
	if line.Code != http.StatusUnprocessableEntity || !strings.Contains(line.Body.String(), `value="x"`) {
		t.Errorf("line POST with a bad quantity = %d, want 422 with the typed value; body %.400s", line.Code, line.Body)
	}
}

// Each DocType's list shows only its own rows: Sample's list has no Hooked
// document, and Hooked's no Sample one. Rows link by the list's DocType and
// the row's id, so the ids tell them apart.
func TestGallery_ListsKeepTheirDocType(t *testing.T) {
	h := Handler()
	for _, c := range []struct {
		list      string
		mine, not int64
	}{
		{docscreentest.Sample, galleryIDs.Draft, hookedIDs.Draft},
		{docscreentest.Hooked, hookedIDs.Draft, galleryIDs.Draft},
	} {
		d := get(t, h, "/doc/"+c.list)
		if d == nil {
			continue
		}
		if d.Find(`a[href="/doc/`+c.list+"/"+strconv.FormatInt(c.mine, 10)+`"]`).Length() == 0 {
			t.Errorf("%s list has no row for its own document %d", c.list, c.mine)
		}
		if n := d.Find(`a[href="/doc/` + c.list + "/" + strconv.FormatInt(c.not, 10) + `"]`).Length(); n != 0 {
			t.Errorf("%s list has a row for the other DocType's document %d", c.list, c.not)
		}
	}
}
