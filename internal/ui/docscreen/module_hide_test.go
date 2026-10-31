package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-1b (CD-256 (1), (2), (10), (11)): Module.Hide everywhere a field renders
// or posts, the hooks' Ctx, and Module.LineCard. Every test runs on
// docscreentest.Hooked (or m01bRO, for a ReadOnly field) on a fixture
// database, the real engine and the real AUTH service (CD-125).

// m01bHide hides the named fields, whatever the Ctx.
func m01bHide(names ...string) func(docscreen.Ctx, doctype.Field) bool {
	return func(_ docscreen.Ctx, f doctype.Field) bool { return slices.Contains(names, f.Name) }
}

// m01bMount gives dt the module m and rebuilds the router.
func m01bMount(e *hookedEnv, dt string, m docscreen.Module) {
	e.d.Modules = map[string]docscreen.Module{dt: m}
	e.remount()
}

// m01bHooked is the Hooked harness with one draft saved by the open user on
// METRO-SITE (ref HK-REF, remarks "hooked remarks"; line 1 PRJ-METRO,
// CEMENT-OPC, 10, "first"; line 2 PRJ-TECHPARK, WIRE-18G, 5, "second"), and
// m as Hooked's module.
func m01bHooked(t *testing.T, m docscreen.Module) (*hookedEnv, int64) {
	t.Helper()
	e := newHookedEnv(t)
	res, err := e.draft(t, e.open, "METRO-SITE")
	if err != nil {
		t.Fatalf("saving the Hooked draft: %v", err)
	}
	m01bMount(e, docscreentest.Hooked, m)
	return e, res.ID
}

func m01bURL(id int64) string { return "/doc/" + docscreentest.Hooked + "/" + itoa(id) }

func m01bRecord(t *testing.T, e *hookedEnv, id int64) doc.Record {
	t.Helper()
	return e.fx.Record(t, docscreentest.Hooked, id)
}

// m01bVersion is the stored version of Hooked id, as a form posts it.
func m01bVersion(t *testing.T, e *hookedEnv, id int64) string {
	t.Helper()
	return strconv.Itoa(m01bRecord(t, e, id).Version)
}

// m01bOK fails unless rec is a 200 and returns its page.
func m01bOK(t *testing.T, rec *httptest.ResponseRecorder, what string) *goquery.Document {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200; body %.400s", what, rec.Code, rec.Body)
	}
	return uitest.Doc(t, rec)
}

// m01bNamed counts the elements posting name, or with the input id of name.
func m01bNamed(page *goquery.Document, name string) int {
	return page.Find(`[name="` + name + `"], [id="` + components.FieldID(name) + `"]`).Length()
}

// m01bSubmit submits Hooked id on the engine; Hooked isn't Approvable, so it
// is approved.
func m01bSubmit(t *testing.T, e *hookedEnv, id int64) {
	t.Helper()
	rec := m01bRecord(t, e, id)
	res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSubmit, doc.ActionInput{Version: rec.Version})
	if err != nil {
		t.Fatalf("submitting Hooked %d: %v", id, err)
	}
	if res.State != doctype.StateApproved {
		t.Fatalf("submitted state = %s, want approved", res.State)
	}
}

// m01bPairs are the label → value rows of the section.card titled title.
func m01bPairs(page *goquery.Document, title string) (labels, values []string) {
	sec := checkSection(page.Selection, title)
	sec.Find("dl.summary .summary-row").Each(func(_ int, row *goquery.Selection) {
		labels = append(labels, norm(row.Find("dt").Text()))
		values = append(values, norm(row.Find("dd").Text()))
	})
	return labels, values
}

// m01bLine1 is line 1's stored values in input form, but for note.
func (e *hookedEnv) m01bLine1(version string) url.Values {
	m := e.fx.M
	return url.Values{
		"version": {version}, "project_id": {itoa(m.Project["PRJ-METRO"])}, "item_id": {itoa(m.Item["CEMENT-OPC"])}, "qty": {"7"},
	}
}

// HOOK-010: a hidden header field (ref) and a hidden line field (note) keep
// their stored values through a generic step save and a generic line save; a
// crafted post of either is dropped. Neither renders on its step, its line
// page, the desktop grid, the check page or the document page's Details.
func TestModule_HideKeepsValue(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("ref", "note")})
	base := m01bURL(id)

	terms := m01bOK(t, e.get(t, base+"/edit/terms"), "GET terms")
	if m01bNamed(terms, "ref") != 0 || strings.Contains(norm(terms.Find("#wizard").Text()), "HK-REF") {
		t.Errorf("terms step renders the hidden ref")
	}
	if m01bNamed(terms, "remarks") == 0 {
		t.Fatalf("terms step has no remarks input: the step itself must still render")
	}

	// A generic step save without ref.
	rec := e.post(t, base+"/edit/terms", url.Values{"version": {m01bVersion(t, e, id)}, "remarks": {"new remarks"}})
	if got := location(t, rec); got != base+"/edit/items" {
		t.Errorf("terms save went to %q, want the items step", got)
	}
	if got := m01bRecord(t, e, id).Values; got["ref"] != "HK-REF" || got["remarks"] != "new remarks" {
		t.Errorf("after a terms save without ref: ref %q, remarks %q; want HK-REF kept and remarks saved", got["ref"], got["remarks"])
	}
	// A crafted post of ref is dropped.
	rec = e.post(t, base+"/edit/terms", url.Values{"version": {m01bVersion(t, e, id)}, "remarks": {"again"}, "ref": {"CRAFTED"}})
	location(t, rec)
	if got := m01bRecord(t, e, id).Values; got["ref"] != "HK-REF" || got["remarks"] != "again" {
		t.Errorf("after a crafted terms post: ref %q, remarks %q; want HK-REF kept and remarks again", got["ref"], got["remarks"])
	}

	line := m01bOK(t, e.get(t, base+"/lines/1"), "GET line 1")
	if m01bNamed(line, "note") != 0 {
		t.Errorf("line page renders the hidden note")
	}
	if m01bNamed(line, "qty") == 0 {
		t.Fatalf("line page has no qty input: the page itself must still render")
	}

	// A generic line save without note.
	if got := location(t, e.post(t, base+"/lines/1", e.m01bLine1(m01bVersion(t, e, id)))); got != base+"/edit/items" {
		t.Errorf("line save went to %q, want the items step", got)
	}
	if l := m01bRecord(t, e, id).Lines[0]; l["note"] != "first" || l["qty"] != "7.000" {
		t.Errorf("after a line save without note: note %q, qty %q; want first kept and 7.000 saved", l["note"], l["qty"])
	}
	// A crafted post of note is dropped.
	form := e.m01bLine1(m01bVersion(t, e, id))
	form.Set("qty", "8")
	form.Set("note", "crafted")
	location(t, e.post(t, base+"/lines/1", form))
	if l := m01bRecord(t, e, id).Lines[0]; l["note"] != "first" || l["qty"] != "8.000" {
		t.Errorf("after a crafted line post: note %q, qty %q; want first kept and 8.000 saved", l["note"], l["qty"])
	}

	items := m01bOK(t, e.get(t, base+"/edit/items"), "GET items")
	if n := items.Find(`[id="grid-1-note"], [id="grid-2-note"]`).Length(); n != 0 {
		t.Errorf("the desktop grid has %d note cells, want none", n)
	}
	if items.Find(`[id="grid-1-qty"]`).Length() != 1 {
		t.Fatalf("the desktop grid has no grid-1-qty: the grid itself must still render")
	}

	check := m01bOK(t, e.get(t, base+"/edit/check"), "GET check")
	if labels, values := m01bPairs(check, "Terms"); slices.Contains(labels, "Reference") || slices.Contains(values, "HK-REF") {
		t.Errorf("check page Terms section = %q %q, want no Reference", labels, values)
	}

	m01bSubmit(t, e, id)
	page := m01bOK(t, e.get(t, base), "GET the document page")
	labels, values := m01bPairs(page, "Details")
	if slices.Contains(labels, "Reference") || slices.Contains(values, "HK-REF") {
		t.Errorf("document page Details = %q %q, want no Reference", labels, values)
	}
	if !slices.Contains(labels, "Remarks") {
		t.Errorf("document page Details = %q, want Remarks still shown", labels)
	}
}

// CD-256 (1), postedStep: a step save, and the first step's save on /new,
// skip a hidden field: omitted it keeps its stored value, posted it is dropped.
func TestModule_HidePostedStep(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("site_id")})
	base := m01bURL(id)
	metro, tech := itoa(e.site["METRO-SITE"]), itoa(e.site["TECH-SITE"])
	supplier := itoa(e.fx.M.Party["SHREE"])

	location(t, e.post(t, base+"/edit/details", url.Values{"version": {m01bVersion(t, e, id)}, "supplier_id": {supplier}}))
	if got := m01bRecord(t, e, id).Values["site_id"]; got != metro {
		t.Errorf("site_id after a details save without it = %q, want %s kept", got, metro)
	}
	location(t, e.post(t, base+"/edit/details", url.Values{"version": {m01bVersion(t, e, id)}, "supplier_id": {supplier}, "site_id": {tech}}))
	if got := m01bRecord(t, e, id).Values["site_id"]; got != metro {
		t.Errorf("site_id after a crafted details post = %q, want %s kept", got, metro)
	}

	to := location(t, e.post(t, "/doc/"+docscreentest.Hooked+"/new", url.Values{"supplier_id": {supplier}, "site_id": {tech}}))
	newID, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(to, "/doc/"+docscreentest.Hooked+"/"), "/edit/terms"), 10, 64)
	if err != nil {
		t.Fatalf("POST /new went to %q, want the new draft's terms step", to)
	}
	if got := m01bRecord(t, e, newID).Values; got["site_id"] != "" || got["supplier_id"] != supplier {
		t.Errorf("a draft created with a crafted site_id stored site %q, supplier %q; want no site and supplier %s", got["site_id"], got["supplier_id"], supplier)
	}
}

// CD-256 (1), postedLine: a line save, of an existing line and a new one,
// skips a hidden line field.
func TestModule_HidePostedLine(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("note")})
	base := m01bURL(id)

	location(t, e.post(t, base+"/lines/1", e.m01bLine1(m01bVersion(t, e, id))))
	if l := m01bRecord(t, e, id).Lines[0]; l["note"] != "first" || l["qty"] != "7.000" {
		t.Errorf("line 1 after a save without note: note %q, qty %q; want first kept, 7.000 saved", l["note"], l["qty"])
	}
	form := e.m01bLine1(m01bVersion(t, e, id))
	form.Set("note", "crafted")
	location(t, e.post(t, base+"/lines/1", form))
	if l := m01bRecord(t, e, id).Lines[0]; l["note"] != "first" {
		t.Errorf("line 1 note after a crafted post = %q, want first kept", l["note"])
	}
	// The grid's htmx row save takes the same path.
	form = e.m01bLine1(m01bVersion(t, e, id))
	form.Set("note", "crafted grid")
	m01bOK(t, e.post(t, base+"/lines/1", form, "HX-Request", "true"), "grid row save")
	if l := m01bRecord(t, e, id).Lines[0]; l["note"] != "first" {
		t.Errorf("line 1 note after a crafted grid post = %q, want first kept", l["note"])
	}

	form = e.m01bLine1(m01bVersion(t, e, id))
	form.Set("note", "crafted new")
	location(t, e.post(t, base+"/lines/new", form))
	lines := m01bRecord(t, e, id).Lines
	if len(lines) != 3 || lines[2]["note"] != "" || lines[2]["qty"] != "7.000" {
		t.Errorf("stored lines = %q, want a third with qty 7.000 and no note", lines)
	}
}

// CD-256 (1), linePage: the line form leaves out a hidden field on GET of an
// existing and a new line, and on a 422 redraw.
func TestModule_HideLinePage(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("note")})
	base := m01bURL(id)
	for _, path := range []string{"/lines/1", "/lines/new"} {
		page := m01bOK(t, e.get(t, base+path), "GET "+path)
		if n := m01bNamed(page, "note"); n != 0 {
			t.Errorf("GET %s renders %d note inputs, want none", path, n)
		}
		if m01bNamed(page, "qty") == 0 {
			t.Errorf("GET %s has no qty input", path)
		}
	}
	form := e.m01bLine1(m01bVersion(t, e, id))
	form.Set("qty", "abc")
	rec := e.post(t, base+"/lines/1", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a line post with qty abc = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if n := m01bNamed(page, "note"); n != 0 || m01bNamed(page, "qty") == 0 {
		t.Errorf("the 422 line page renders %d note inputs, want none beside qty", n)
	}
}

// CD-256 (1), gridRowOf: the desktop grid has no cell for a hidden line
// field, on the items step and in a row save's answer.
func TestModule_HideGridRow(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("note")})
	base := m01bURL(id)
	items := m01bOK(t, e.get(t, base+"/edit/items"), "GET items")
	if n := items.Find(`[id="grid-1-note"], [id="grid-2-note"]`).Length(); n != 0 {
		t.Errorf("items step grid has %d note cells, want none", n)
	}
	if items.Find(`[id="grid-1-qty"], [id="grid-2-qty"]`).Length() != 2 {
		t.Fatalf("items step grid has no qty cells: the grid must still render")
	}
	row := m01bOK(t, e.post(t, base+"/lines/1", e.m01bLine1(m01bVersion(t, e, id)), "HX-Request", "true"), "grid row save")
	if n := row.Find(`[id="grid-1-note"], [id="grid-2-note"]`).Length(); n != 0 {
		t.Errorf("the grid row save's answer has %d note cells, want none", n)
	}
	if row.Find(`[id="grid-1-qty"]`).Length() == 0 {
		t.Errorf("the grid row save's answer has no grid-1-qty")
	}
}

// CD-256 (1), stepViewOf's read-only Fixed list and totalsOf: m01bRO's
// ReadOnly tag shows on its step and in the items step's totals, until it is
// hidden.
func TestModule_HideReadOnly(t *testing.T) {
	e, id := m01bROEnv(t)
	base := "/doc/" + m01bRO + "/" + itoa(id)
	text := func(path string) string {
		t.Helper()
		return norm(m01bOK(t, e.get(t, base+path), "GET "+path).Find("main").Text())
	}
	// Without a module the tag shows: the setup decides the outcome.
	if !strings.Contains(text("/edit/details"), m01bTag) || !strings.Contains(text("/edit/items"), m01bTag) {
		t.Fatalf("with no module the details step and items totals must show %s", m01bTag)
	}
	m01bMount(e, m01bRO, docscreen.Module{Hide: m01bHide("tag")})
	t.Run("Fixed", func(t *testing.T) {
		if got := text("/edit/details"); strings.Contains(got, m01bTag) || strings.Contains(got, "Batch tag") {
			t.Errorf("details step shows the hidden ReadOnly tag: %q", got)
		}
	})
	t.Run("totalsOf", func(t *testing.T) {
		if got := text("/edit/items"); strings.Contains(got, m01bTag) || strings.Contains(got, "Batch tag") {
			t.Errorf("items step totals show the hidden ReadOnly tag: %q", got)
		}
		form := url.Values{"version": {strconv.Itoa(e.fx.Record(t, m01bRO, id).Version)}, "item_id": {itoa(e.fx.M.Item["CEMENT-OPC"])}, "qty": {"3"}}
		rec := e.post(t, base+"/lines/1", form, "HX-Request", "true")
		if got := norm(m01bOK(t, rec, "grid row save").Text()); strings.Contains(got, m01bTag) {
			t.Errorf("the grid row save's totals show the hidden ReadOnly tag: %q", got)
		}
	})
	t.Run("check section", func(t *testing.T) {
		if labels, values := m01bPairs(m01bOK(t, e.get(t, base+"/edit/check"), "GET check"), "Details"); slices.Contains(values, m01bTag) {
			t.Errorf("check page Details = %q %q, want no tag", labels, values)
		}
	})
}

// CD-256 (1), checkStep: a hidden Required field left empty doesn't mark its
// step Needs attention, nor list its problem.
func TestModule_HideNeedsAttention(t *testing.T) {
	e, id := m01bNoSupplier(t)
	status := func() (string, string) {
		t.Helper()
		sec := checkSection(m01bOK(t, e.get(t, m01bURL(id)+"/edit/check"), "GET check").Selection, "Details")
		return norm(sec.Find(".check-status").Text()), norm(sec.Text())
	}
	if st, text := status(); st != "Needs attention" || !strings.Contains(text, "Supplier is required.") {
		t.Fatalf("with no module the Details section = %q %q, want Needs attention with Supplier is required.", st, text)
	}
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Hide: m01bHide("supplier_id")})
	if st, text := status(); st != "Complete" || strings.Contains(text, "Supplier is required.") {
		t.Errorf("Details section with supplier_id hidden = %q %q, want Complete without its problem", st, text)
	}
}

// CD-256 (1), stepErrors: DOC still refuses the submit (Hide is presentation
// only), and the failing step's error summary shows the hidden field's error
// as plain text, with no link to an input that isn't there.
func TestModule_HideStepErrors(t *testing.T) {
	e, id := m01bNoSupplier(t)
	m01bMount(e, docscreentest.Hooked, docscreen.Module{Hide: m01bHide("supplier_id")})
	rec := e.post(t, m01bURL(id)+"/submit", url.Values{"version": {m01bVersion(t, e, id)}})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("submit with the hidden Required supplier empty = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	page := uitest.Doc(t, rec)
	if n := m01bNamed(page, "supplier_id"); n != 0 {
		t.Errorf("the failing step renders %d supplier inputs, want none", n)
	}
	if got := norm(page.Find(".error-summary").Text()); !strings.Contains(got, "Supplier is required.") {
		t.Errorf("error summary = %q, want Supplier is required.", got)
	}
	if links := summaryLinks(page); slices.Contains(links, "#"+components.FieldID("supplier_id")) {
		t.Errorf("error summary links %q, want no link to the hidden supplier", links)
	}
	wantNoDeadLinks(t, rec)
}

// CD-256 (1), pageViewOf: the document page's Details drop a hidden field,
// and Header and HeaderNames stay parallel, so every other label keeps its
// own value.
func TestModule_HidePageHeader(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{Hide: m01bHide("ref")})
	m01bSubmit(t, e, id)
	v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	labels, values := m01bPairs(m01bOK(t, e.get(t, m01bURL(id)), "GET the document page"), "Details")
	wantLabels := []string{"Supplier", "Site", "Remarks"}
	wantValues := []string{v.Display["supplier_id"], v.Display["site_id"], v.Display["remarks"]}
	if !slices.Equal(labels, wantLabels) || !slices.Equal(values, wantValues) {
		t.Errorf("Details = %q %q, want %q %q", labels, values, wantLabels, wantValues)
	}
}

// CD-256 (2): Hide sees the loaded DocView on a POST to an existing draft, as
// on its GET, so a rule that reads a stored value decides the same both ways;
// on /new View is nil, on its GET and POST.
func TestModule_HideSeesView(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]bool{} // "METHOD path" → View != nil on each call
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		mu.Lock()
		key := c.Req.Method + " " + c.Req.URL.Path
		seen[key] = append(seen[key], c.View != nil)
		mu.Unlock()
		if c.View == nil {
			return false
		}
		// The stored remarks decide; the posted ones never do.
		return (f.Name == "ref" && c.View.Inputs["remarks"] == "hooked remarks") || f.Name == "supplier_id"
	}
	e, id := m01bHooked(t, docscreen.Module{Hide: hide})
	base := m01bURL(id)

	if page := m01bOK(t, e.get(t, base+"/edit/terms"), "GET terms"); m01bNamed(page, "ref") != 0 {
		t.Errorf("GET terms renders ref, which Hide hides on the stored remarks")
	}
	location(t, e.post(t, base+"/edit/terms", url.Values{"version": {m01bVersion(t, e, id)}, "remarks": {"changed"}, "ref": {"CRAFTED"}}))
	if got := m01bRecord(t, e, id).Values; got["ref"] != "HK-REF" || got["remarks"] != "changed" {
		t.Errorf("after the terms POST: ref %q, remarks %q; want HK-REF kept (Hide saw the stored remarks) and remarks changed", got["ref"], got["remarks"])
	}

	newURL := "/doc/" + docscreentest.Hooked + "/new"
	if page := m01bOK(t, e.get(t, newURL), "GET /new"); m01bNamed(page, "supplier_id") == 0 {
		t.Errorf("GET /new hides supplier_id: Hide must get View nil there")
	}
	supplier := itoa(e.fx.M.Party["SHREE"])
	to := location(t, e.post(t, newURL, url.Values{"supplier_id": {supplier}}))
	newID, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(to, "/doc/"+docscreentest.Hooked+"/"), "/edit/terms"), 10, 64)
	if err != nil {
		t.Fatalf("POST /new went to %q, want the new draft's terms step", to)
	}
	if got := m01bRecord(t, e, newID).Values["supplier_id"]; got != supplier {
		t.Errorf("POST /new stored supplier %q, want %s: Hide must get View nil there", got, supplier)
	}

	mu.Lock()
	defer mu.Unlock()
	for key, want := range map[string]bool{
		"GET " + base + "/edit/terms": true, "POST " + base + "/edit/terms": true,
		"GET " + newURL: false, "POST " + newURL: false,
	} {
		calls := seen[key]
		if len(calls) == 0 {
			t.Errorf("%s: Hide was never called", key)
		}
		for _, got := range calls {
			if got != want {
				t.Errorf("%s: Hide saw View set %v, want %v on every call", key, got, want)
				break
			}
		}
	}
}

// CD-256 (10): Ctx.Deps is Deps.WithStored of the stored inputs, so a hook
// rendering FieldInput for a stored link gets its label without a read check
// (CD-217, CD-228): the header's on a step, the line's on its line page. The
// harness has no LinkRead, so plain Deps shows only the raw id.
func TestModule_HideCtxDepsStored(t *testing.T) {
	var mu sync.Mutex
	rendered := map[string]string{} // "path field" → FieldInput's HTML with c.Deps
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if c.View == nil || c.Req.Method != http.MethodGet {
			return false
		}
		value := ""
		switch {
		case f.Name == "site_id" && strings.HasSuffix(c.Req.URL.Path, "/edit/details"):
			value = c.View.Inputs["site_id"]
		case f.Name == "project_id" && strings.HasSuffix(c.Req.URL.Path, "/lines/1") && len(c.View.Lines) > 0:
			value = c.View.Lines[0].Inputs["project_id"]
		default:
			return false
		}
		var b strings.Builder
		err := docscreen.FieldInput(f, "m01b-probe", value, "", c.Deps).Render(c.Req.Context(), &b)
		if err != nil {
			b.WriteString("render error: " + err.Error())
		}
		mu.Lock()
		rendered[c.Req.URL.Path+" "+f.Name] = b.String()
		mu.Unlock()
		return false
	}
	e, id := m01bHooked(t, docscreen.Module{Hide: hide})
	base := m01bURL(id)
	m01bOK(t, e.get(t, base+"/edit/details"), "GET details")
	m01bOK(t, e.get(t, base+"/lines/1"), "GET line 1")

	dt, _ := doctype.Lookup(docscreentest.Hooked)
	project, _ := dt.Field("project_id")
	var plain strings.Builder
	if err := docscreen.FieldInput(project, "m01b-probe", itoa(e.fx.M.Project["PRJ-METRO"]), "", docscreen.Deps{DB: e.fx.DB}).Render(t.Context(), &plain); err != nil {
		t.Fatalf("FieldInput: %v", err)
	}
	if strings.Contains(plain.String(), "PRJ-METRO") {
		t.Fatalf("FieldInput with plain Deps shows PRJ-METRO: the check below would prove nothing")
	}

	mu.Lock()
	defer mu.Unlock()
	for key, want := range map[string]string{base + "/edit/details site_id": "METRO-SITE", base + "/lines/1 project_id": "PRJ-METRO"} {
		got, ok := rendered[key]
		if !ok {
			t.Errorf("%s: Hide was never called", key)
			continue
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: FieldInput with c.Deps = %.300s, want the stored link's label %s", key, got, want)
		}
	}
}

// CD-256 (1), (11): Module.LineCard replaces each line's card on the check
// page and the document page; on a draft CD-242's clearing of Received still
// applies after the override, and an approved document shows it.
func TestModule_LineCardOverride(t *testing.T) {
	const received = "Received 4 of 10"
	card := func(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
		c := l.Card
		c.Item, c.Code, c.Received = "M01B card "+l.Inputs["note"], "M01B-CODE", received
		return c
	}
	e, id := m01bHooked(t, docscreen.Module{LineCard: card})
	base := m01bURL(id)
	names := func(sel *goquery.Selection) []string {
		return sel.Find(".lines-card .lines-name").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
	}
	want := []string{"M01B card first M01B-CODE", "M01B card second M01B-CODE"}

	items := checkSection(m01bOK(t, e.get(t, base+"/edit/check"), "GET check").Selection, "Items")
	if got := names(items); !slices.Equal(got, want) {
		t.Errorf("check page cards = %q, want %q", got, want)
	}
	if strings.Contains(norm(items.Text()), received) {
		t.Errorf("draft check page shows %q: CD-242's clearing must run after the override", received)
	}

	m01bSubmit(t, e, id)
	lines := checkSection(m01bOK(t, e.get(t, base), "GET the document page").Selection, "Lines")
	if got := names(lines); !slices.Equal(got, want) {
		t.Errorf("document page cards = %q, want %q", got, want)
	}
	if got := strings.Count(norm(lines.Text()), received); got != 2 {
		t.Errorf("approved document page shows %q %d times, want once per line", received, got)
	}
}

// Hooks spec §4.3: a field hidden for its raw value, like GRN's site_id,
// keeps its display value for the hooks (c.View.Display), its stored value
// through a save, and its raw id never shows: no input or Details row.
func TestModule_HideLinkDisplay(t *testing.T) {
	var mu sync.Mutex
	var shown []string
	hide := func(c docscreen.Ctx, f doctype.Field) bool {
		if f.Name != "site_id" {
			return false
		}
		if c.View != nil {
			mu.Lock()
			shown = append(shown, c.View.Display["site_id"])
			mu.Unlock()
		}
		return true
	}
	e, id := m01bHooked(t, docscreen.Module{Hide: hide})
	base := m01bURL(id)
	raw := itoa(e.site["METRO-SITE"])

	details := m01bOK(t, e.get(t, base+"/edit/details"), "GET details")
	if n := m01bNamed(details, "site_id"); n != 0 {
		t.Errorf("details step has %d site_id inputs, want none", n)
	}
	location(t, e.post(t, base+"/edit/details", url.Values{"version": {m01bVersion(t, e, id)}, "supplier_id": {itoa(e.fx.M.Party["SHREE"])}}))
	if got := m01bRecord(t, e, id).Values["site_id"]; got != raw {
		t.Errorf("site_id after a details save = %q, want %s kept", got, raw)
	}
	if labels, values := m01bPairs(m01bOK(t, e.get(t, base+"/edit/check"), "GET check"), "Details"); slices.Contains(labels, "Site") || slices.Contains(values, raw) {
		t.Errorf("check page Details = %q %q, want no Site row", labels, values)
	}
	m01bSubmit(t, e, id)
	if labels, values := m01bPairs(m01bOK(t, e.get(t, base), "GET the document page"), "Details"); slices.Contains(labels, "Site") || slices.Contains(values, raw) {
		t.Errorf("document page Details = %q %q, want no Site row and no raw id", labels, values)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(shown) == 0 {
		t.Fatal("Hide never saw a View")
	}
	for _, s := range shown {
		if s == raw || !strings.Contains(s, "METRO-SITE") {
			t.Errorf("c.View.Display[site_id] = %q, want the site's code METRO-SITE, not its id %s", s, raw)
			break
		}
	}
}

// m01bNoSupplier is the Hooked harness with a draft whose Required supplier
// is empty (a draft save skips Required, CD-178), and no module.
func m01bNoSupplier(t *testing.T) (*hookedEnv, int64) {
	t.Helper()
	e := newHookedEnv(t)
	m := e.fx.M
	res, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": itoa(e.site["METRO-SITE"]), "ref": "HK-REF"},
		Lines:  []map[string]string{{"project_id": itoa(m.Project["PRJ-METRO"]), "item_id": itoa(m.Item["CEMENT-OPC"]), "qty": "10.000"}},
	})
	if err != nil {
		t.Fatalf("saving a Hooked draft with no supplier: %v", err)
	}
	return e, res.ID
}

// m01bRO is a test DocType with a ReadOnly header field, which Hooked lacks:
// details holds supplier_id and the ReadOnly tag, then items and check.
const (
	m01bRO  = "docscreentest.M01bRO"
	m01bTag = "RO-TAG-7"
)

var m01bROOnce sync.Once

func m01bRegisterRO() {
	m01bROOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: m01bRO, Label: "Read-only Order", PluralLabel: "Read-only Orders", Table: "m01b_ro", LinesTable: "m01b_ro_line",
			Submittable: true, Hooks: scopeHooks{},
			Fields: []doctype.Field{
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: fixture.Party},
				{Name: "tag", Label: "Batch tag", Type: doctype.Text, ReadOnly: true},
				{Name: "item_id", Label: "Item", Type: doctype.Link, Link: fixture.Item, OnLine: true},
				{Name: "qty", Label: "Quantity", Type: doctype.Qty, Positive: true, OnLine: true},
			},
		})
	})
}

// m01bROEnv is the Hooked harness, with the Hooker role also given every
// right on m01bRO on the real AUTH service, and one m01bRO draft by the open
// user with one line and tag RO-TAG-7, written as a calculation would.
func m01bROEnv(t *testing.T) (*hookedEnv, int64) {
	t.Helper()
	m01bRegisterRO()
	e := newHookedEnv(t)
	e.fx.Exec(t, `CREATE TABLE m01b_ro (id INTEGER PRIMARY KEY AUTOINCREMENT, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER, amendment_no INTEGER NOT NULL DEFAULT 0,
		version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by INTEGER NOT NULL,
		supplier_id INTEGER, tag TEXT NOT NULL DEFAULT '')`)
	e.fx.Exec(t, `CREATE TABLE m01b_ro_line (id INTEGER PRIMARY KEY AUTOINCREMENT, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL,
		item_id INTEGER, qty INTEGER)`)
	e.fx.Naming.Prefix[m01bRO] = "RO"
	ctx := t.Context()
	a, ok := e.d.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Deps.Auth is %T, want the real *auth.Auth", e.d.Auth)
	}
	all := map[auth.Right]auth.Scope{}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "delete"} {
		all[r] = auth.ScopeAll
	}
	if err := a.SetRights(ctx, e.fx.Admin, "Hooker", m01bRO, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	p, _, err := a.LoadPrincipal(ctx, e.open.ID)
	if err != nil {
		t.Fatalf("LoadPrincipal: %v", err)
	}
	e.open, e.who = p, p
	res, err := e.engine.Do(ctx, p, m01bRO, 0, doc.ActionSave, doc.ActionInput{
		Values: map[string]string{"supplier_id": itoa(e.fx.M.Party["SHREE"])},
		Lines:  []map[string]string{{"item_id": itoa(e.fx.M.Item["CEMENT-OPC"]), "qty": "10.000"}},
	})
	if err != nil {
		t.Fatalf("saving an m01bRO draft: %v", err)
	}
	e.fx.Exec(t, `UPDATE m01b_ro SET tag = ? WHERE id = ?`, m01bTag, res.ID)
	return e, res.ID
}
