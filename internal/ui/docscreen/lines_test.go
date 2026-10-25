package docscreen_test

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The line tests' items: three masters so moved lines are told apart.
func setLineItems() {
	docscreentest.SetMaster(docscreentest.Item, 3, doctype.MasterInfo{Code: "MS-ANGLE", Name: "MS Angle 50x50x5"})
	docscreentest.SetMaster(docscreentest.Item, 4, doctype.MasterInfo{Code: "MS-FLAT", Name: "MS Flat 40x6"})
	docscreentest.SetMaster(docscreentest.Item, 5, doctype.MasterInfo{Code: "TMT-12", Name: "TMT Bar 12 mm"})
}

// threeLines is draft 42 at version 6 with lines 11, 12 and 13 (items 3,
// 4, 5) in plain form.
func threeLines() doc.Record {
	return doc.Record{
		Version: 6,
		Values:  map[string]string{"supplier_id": "7", "order_date": "2026-10-24", "amount": "94164.00"},
		Lines: []map[string]string{
			{"id": "11", "item_id": "3", "qty": "10.000", "rate": "62.5000"},
			{"id": "12", "item_id": "4", "qty": "20.000", "rate": "62.5000"},
			{"id": "13", "item_id": "5", "qty": "30.000", "rate": "62.5000"},
		},
	}
}

// manyLines is draft 42 at version 6 with n lines; the lines numbered in
// angle are item 3 (MS Angle), the rest item 4 (MS Flat).
func manyLines(n int, angle ...int) doc.Record {
	rec := threeLines()
	rec.Lines = nil
	for no := 1; no <= n; no++ {
		item := "4"
		if slices.Contains(angle, no) {
			item = "3"
		}
		rec.Lines = append(rec.Lines, map[string]string{"id": strconv.Itoa(100 + no), "item_id": item, "qty": "1.000", "rate": "10.0000"})
	}
	return rec
}

func newLinesEnv(t *testing.T, rec doc.Record) *docEnv {
	t.Helper()
	e := newFormEnv(t, allRights...)
	setLineItems()
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
	return e
}

func plainQty(t *testing.T, typed string) string {
	t.Helper()
	q, err := types.ParseQty(typed)
	if err != nil {
		t.Fatal(err)
	}
	return q.Plain()
}

// oneSave is the single recorded save of draft 42.
func oneSave(t *testing.T, e *docEnv) doc.ActionInput {
	t.Helper()
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].ID != 42 {
		t.Fatalf("engine calls = %+v, want one save of 42", calls)
	}
	return calls[0].Input
}

func TestLines_AddChangeRemoveMove(t *testing.T) {
	loaded := threeLines().Lines
	l1, l2, l3 := loaded[0], loaded[1], loaded[2]
	ok := doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}

	t.Run("add", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
		if res := e.get(t, doc42+"/lines/new"); res.Code != http.StatusOK || buttonsNamed(uitest.Doc(t, res).Selection, "Save line").Length() != 1 {
			t.Fatalf("GET lines/new: status %d, want 200 with a \"Save line\" button", res.Code)
		}
		rec := e.post(t, doc42+"/lines/new", url.Values{"version": {"6"}, "item_id": {"5"}, "qty": {"5"}, "rate": {"10"}})
		if got := location(t, rec); got != doc42+"/edit/items" {
			t.Errorf("303 to %q, want %s/edit/items", got, doc42)
		}
		in := oneSave(t, e)
		if in.Version != 6 || len(in.Lines) != 4 {
			t.Fatalf("save = %+v, want version 6 and 4 lines", in)
		}
		for i, want := range []map[string]string{l1, l2, l3} {
			if !maps.Equal(in.Lines[i], want) {
				t.Errorf("line %d = %q, want the loaded %q", i+1, in.Lines[i], want)
			}
		}
		added := in.Lines[3]
		if added["id"] != "" || added["item_id"] != "5" || added["qty"] != plainQty(t, "5") || added["rate"] != "10.00" {
			t.Errorf("added line = %q, want no id, item 5, qty %s, rate 10.00", added, plainQty(t, "5"))
		}
	})
	t.Run("change", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
		res := e.get(t, doc42+"/lines/2")
		if res.Code != http.StatusOK {
			t.Fatalf("GET lines/2: status %d", res.Code)
		}
		page := uitest.Doc(t, res)
		if v, _ := page.Find(`input[name="item_id"]`).Attr("value"); v != "4" {
			t.Errorf("line page item_id = %q, want 4", v)
		}
		if buttonsNamed(page.Selection, "Save line").Length() != 1 {
			t.Error("line page has no \"Save line\"")
		}
		rec := e.post(t, doc42+"/lines/2", url.Values{"version": {"6"}, "item_id": {"4"}, "qty": {"25"}, "rate": {"62.5"}})
		if got := location(t, rec); got != doc42+"/edit/items" {
			t.Errorf("303 to %q", got)
		}
		in := oneSave(t, e)
		if in.Version != 6 || len(in.Lines) != 3 {
			t.Fatalf("save = %+v, want version 6 and 3 lines", in)
		}
		if !maps.Equal(in.Lines[0], l1) || !maps.Equal(in.Lines[2], l3) {
			t.Errorf("other lines = %q, %q; want the loaded ones", in.Lines[0], in.Lines[2])
		}
		if c := in.Lines[1]; c["id"] != "12" || c["item_id"] != "4" || c["qty"] != plainQty(t, "25") || c["rate"] != "62.50" {
			t.Errorf("changed line = %q, want id 12, item 4, qty %s, rate 62.50", c, plainQty(t, "25"))
		}
	})
	t.Run("remove", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
		res := e.get(t, doc42+"/lines/2/remove")
		if res.Code != http.StatusOK {
			t.Fatalf("GET remove: status %d", res.Code)
		}
		if text := norm(uitest.Doc(t, res).Text()); !strings.Contains(text, "Remove line 2 (MS Flat 40x6)?") {
			t.Errorf("remove page lacks \"Remove line 2 (MS Flat 40x6)?\"")
		}
		if calls := e.docs.Calls(); len(calls) != 0 {
			t.Fatalf("GET remove called the engine: %+v", calls)
		}
		rec := e.post(t, doc42+"/lines/2/remove", url.Values{"version": {"6"}})
		if got := location(t, rec); got != doc42+"/edit/items" {
			t.Errorf("303 to %q", got)
		}
		in := oneSave(t, e)
		if in.Version != 6 || len(in.Lines) != 2 || !maps.Equal(in.Lines[0], l1) || !maps.Equal(in.Lines[1], l3) {
			t.Errorf("save = %+v, want version 6 and lines 11, 13", in)
		}
	})
	for _, c := range []struct {
		name, no, dir string
		want          []map[string]string // nil: a no-op
	}{
		{"line 2 up", "2", "up", []map[string]string{l2, l1, l3}},
		{"line 2 down", "2", "down", []map[string]string{l1, l3, l2}},
		{"first up", "1", "up", nil},
		{"last down", "3", "down", nil},
	} {
		t.Run("move "+c.name, func(t *testing.T) {
			e := newLinesEnv(t, threeLines())
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
			rec := e.post(t, doc42+"/lines/"+c.no+"/move?dir="+c.dir, url.Values{"version": {"6"}})
			if got := location(t, rec); got != doc42+"/edit/items" {
				t.Errorf("303 to %q, want %s/edit/items", got, doc42)
			}
			if c.want == nil {
				if calls := e.docs.Calls(); len(calls) != 0 {
					t.Errorf("a no-op move saved: %+v", calls)
				}
				return
			}
			in := oneSave(t, e)
			if in.Version != 6 || !slices.EqualFunc(in.Lines, c.want, maps.Equal) {
				t.Errorf("save = version %d lines %q, want version 6 lines %q", in.Version, in.Lines, c.want)
			}
		})
	}
}

// moveForms are the POST forms moving line no, with their container.
func moveForms(sel *goquery.Selection, no int) *goquery.Selection {
	return sel.Find(`form[method="post"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		a, _ := s.Attr("action")
		return strings.Contains(a, "/lines/"+strconv.Itoa(no)+"/move")
	})
}

func TestLines_MoveCarriesPostedVersion(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	res := e.get(t, doc42+"/edit/items")
	if res.Code != http.StatusOK {
		t.Fatalf("GET items: status %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	for _, where := range []struct {
		name string
		sel  *goquery.Selection
	}{
		{"cards", page.Find("ol.lines")},
		{"grid", page.Find("#line-editor")},
	} {
		forms := moveForms(where.sel, 2)
		if forms.Length() != 2 {
			t.Errorf("%s: %d move forms for line 2, want up and down", where.name, forms.Length())
		}
		forms.Each(func(_ int, f *goquery.Selection) {
			a, _ := f.Attr("action")
			v := f.Find(`input[type="hidden"][name="version"]`)
			if got, _ := v.Attr("value"); v.Length() != 1 || got != "6" {
				t.Errorf("%s: move form %q carries version %q (%d inputs), want the rendered 6", where.name, a, got, v.Length())
			}
		})
	}

	ok := doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
	location(t, e.post(t, doc42+"/lines/2/move?dir=up", url.Values{"version": {"5"}}))
	if in := oneSave(t, e); in.Version != 5 {
		t.Errorf("move saved with version %d, want the posted 5 (the loaded record is at 6, CD-94)", in.Version)
	}
}

// A line POST without a usable version is CD-146's 400; DOC is not called.
func TestLines_MissingOrBadVersionIs400(t *testing.T) {
	posts := []struct {
		path string
		form url.Values
	}{
		{doc42 + "/lines/new", url.Values{"item_id": {"5"}, "qty": {"5"}, "rate": {"10"}}},
		{doc42 + "/lines/2", url.Values{"item_id": {"4"}, "qty": {"25"}, "rate": {"62.5"}}},
		{doc42 + "/lines/2/remove", url.Values{}},
		{doc42 + "/lines/2/move?dir=up", url.Values{}},
		{doc42 + "/edit/details", url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}}},
	}
	for _, p := range posts {
		for _, v := range []struct {
			name    string
			version []string
		}{{"missing", nil}, {"abc", []string{"abc"}}} {
			t.Run(p.path+"/"+v.name, func(t *testing.T) {
				e := newLinesEnv(t, threeLines())
				e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
				form := maps.Clone(p.form)
				form.Set("csrf_token", csrfToken)
				if v.version != nil {
					form["version"] = v.version
				}
				rec := e.do(t, http.MethodPost, p.path, form)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", rec.Code)
				}
				if calls := e.docs.Calls(); len(calls) != 0 {
					t.Errorf("engine called: %+v", calls)
				}
				if got := norm(uitest.Doc(t, rec).Text()); !strings.Contains(got, badFormText) {
					t.Errorf("body = %.200q, want %q", got, badFormText)
				}
			})
		}
	}
}

// oobTarget is the id an out-of-band element swaps into.
func oobTarget(s *goquery.Selection) string {
	v, _ := s.Attr("hx-swap-oob")
	if i := strings.Index(v, ":"); i >= 0 {
		return strings.TrimPrefix(strings.TrimSpace(v[i+1:]), "#")
	}
	id, _ := s.Attr("id")
	return id
}

// gridPost posts form#grid-row-<no> as htmx would: its own named controls,
// changed by set, plus #doc-version's version (hx-include).
func gridPost(t *testing.T, e *docEnv, page *goquery.Document, no int, version string, set map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	id := "grid-row-" + strconv.Itoa(no)
	row := page.Find("form#" + id)
	if row.Length() != 1 {
		t.Fatalf("found %d form#%s", row.Length(), id)
	}
	post, _ := row.Attr("hx-post")
	form := url.Values{}
	row.Find("input[name], select[name], textarea[name]").Each(func(_ int, s *goquery.Selection) {
		name, _ := s.Attr("name")
		val, _ := s.Attr("value")
		if goquery.NodeName(s) == "textarea" {
			val = s.Text()
		}
		form.Set(name, val)
	})
	for elID, val := range set {
		name, ok := row.Find("#" + elID).Attr("name")
		if !ok {
			t.Fatalf("form#%s has no named #%s", id, elID)
		}
		form.Set(name, val)
	}
	form.Set("version", version)
	return e.post(t, post, form, "HX-Request", "true", "HX-Target", id, "HX-Trigger", id)
}

func TestLines_GridSaveUsesPostedVersion(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	res := e.get(t, doc42+"/edit/items")
	if res.Code != http.StatusOK {
		t.Fatalf("GET items: status %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)

	// One #doc-version, in the step form, which posts no other version
	// (CD-95).
	dv := page.Find("#doc-version")
	if dv.Length() != 1 || goquery.NodeName(dv) != "input" {
		t.Fatalf("found %d #doc-version, want one input", dv.Length())
	}
	if typ, _ := dv.Attr("type"); typ != "hidden" {
		t.Errorf("#doc-version type = %q", typ)
	}
	if name, _ := dv.Attr("name"); name != "version" {
		t.Errorf("#doc-version name = %q", name)
	}
	if v, _ := dv.Attr("value"); v != "6" {
		t.Errorf("#doc-version value = %q, want 6", v)
	}
	if a, _ := formOf(page, dv).Attr("action"); a != doc42+"/edit/items" {
		t.Errorf("#doc-version sits in a form posting to %q, want the step form %s/edit/items", a, doc42)
	}
	if n := stepForm(page, doc42+"/edit/items").Find(`[name="version"]`).Length(); n != 1 {
		t.Errorf("the step form posts %d version inputs, want only #doc-version", n)
	}
	pageIDs := map[string]bool{}
	page.Find("[id]").Each(func(_ int, s *goquery.Selection) {
		if s.Closest("form.linegrid-form").Length() == 0 {
			id, _ := s.Attr("id")
			pageIDs[id] = true
		}
	})

	t.Run("saved", func(t *testing.T) {
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
		before := len(e.docs.Calls())
		rec := gridPost(t, e, page, 2, "5", map[string]string{"grid-2-qty": "25"})
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
		}
		calls := e.docs.Calls()[before:]
		if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].Input.Version != 5 {
			t.Fatalf("engine calls = %+v, want one save at the posted version 5 (loaded is 6)", calls)
		}
		if l := calls[0].Input.Lines; len(l) != 3 || l[1]["id"] != "12" || l[1]["qty"] != plainQty(t, "25") {
			t.Errorf("saved lines = %q, want line 2 (id 12) with qty %s", l, plainQty(t, "25"))
		}
		if h := rec.Header().Get("Location") + rec.Header().Get("HX-Redirect") + rec.Header().Get("HX-Push-Url"); h != "" {
			t.Errorf("grid save navigates (%q); it swaps its row only", h)
		}

		resp := uitest.Doc(t, rec)
		var main []string
		resp.Find("body").Children().Each(func(_ int, s *goquery.Selection) {
			if _, oob := s.Attr("hx-swap-oob"); oob || goquery.NodeName(s) == "template" {
				return
			}
			id, _ := s.Attr("id")
			main = append(main, goquery.NodeName(s)+"#"+id)
		})
		if !slices.Equal(main, []string{"form#grid-row-2"}) {
			t.Errorf("swapped elements = %q, want only form#grid-row-2", main)
		}
		row := resp.Find("form#grid-row-2")
		if tgt, _ := row.Attr("hx-target"); tgt != "this" {
			t.Errorf("row hx-target = %q, want this", tgt)
		}
		v := resp.Find(`input#doc-version[hx-swap-oob]`)
		if v.Length() != 1 {
			t.Fatalf("found %d out-of-band #doc-version, want 1", v.Length())
		}
		if got, _ := v.Attr("value"); got != "7" {
			t.Errorf("out-of-band #doc-version = %q, want the result's 7", got)
		}
		if name, _ := v.Attr("name"); name != "version" {
			t.Errorf("out-of-band #doc-version name = %q", name)
		}
		totals := 0
		resp.Find("[hx-swap-oob]").Each(func(_ int, s *goquery.Selection) {
			tgt := oobTarget(s)
			if tgt == "doc-version" {
				return
			}
			if !pageIDs[tgt] {
				t.Errorf("out-of-band element targets #%s, which the items page lacks", tgt)
				return
			}
			if got, want := norm(s.Text()), norm(page.Find("#"+tgt).Text()); got != want {
				t.Errorf("out-of-band #%s = %q, want the totals as the page shows them %q", tgt, got, want)
			}
			totals++
		})
		if totals == 0 {
			t.Error("no out-of-band totals")
		}
	})

	t.Run("conflict", func(t *testing.T) {
		const msg = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusConflict, msg: msg})
		before := len(e.docs.Calls())
		rec := gridPost(t, e, page, 2, "5", map[string]string{"grid-2-qty": "25"})
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body %.300s", rec.Code, rec.Body)
		}
		if calls := e.docs.Calls()[before:]; len(calls) != 1 || calls[0].Input.Version != 5 {
			t.Errorf("engine calls = %+v, want one save at version 5", calls)
		}
		resp := uitest.Doc(t, rec)
		found := false
		resp.Find("[hx-swap-oob]").Each(func(_ int, s *goquery.Selection) {
			if strings.Contains(norm(s.Text()), msg) {
				found = true
				if tgt := oobTarget(s); !pageIDs[tgt] {
					t.Errorf("the conflict banner targets #%s, which the items page lacks", tgt)
				}
			}
		})
		if !found {
			t.Errorf("no out-of-band element carries %q", msg)
		}
		row := resp.Find("form#grid-row-2")
		if row.Length() != 1 {
			t.Fatal("the 409 does not return form#grid-row-2")
		}
		if v, _ := row.Find("#grid-2-qty").Attr("value"); v != "25" {
			t.Errorf("row qty = %q, want the typed 25 kept", v)
		}
	})
}

// Every line goes to the grid, built from the line page's field components
// (CD-139); the 20-card limit is the phone's.
func TestLines_GridHasEveryLine(t *testing.T) {
	e := newLinesEnv(t, manyLines(25))
	res := e.get(t, doc42+"/edit/items")
	if res.Code != http.StatusOK {
		t.Fatalf("status %d, body %.300s", res.Code, res.Body)
	}
	page := uitest.Doc(t, res)
	if n := page.Find("#line-editor").Length(); n != 1 {
		t.Fatalf("found %d #line-editor, want 1 (LineGrid only inside LineList)", n)
	}
	if page.Find("#line-editor").Closest(".linelist").Length() != 1 {
		t.Error("the grid is not inside the LineList")
	}
	if n := page.Find("ol.lines > li.lines-card").Length(); n != 20 {
		t.Errorf("%d cards, want the first 20", n)
	}
	rows := page.Find("#line-editor form.linegrid-form")
	if rows.Length() != 25 {
		t.Fatalf("%d grid rows, want all 25", rows.Length())
	}
	rows.Each(func(i int, s *goquery.Selection) {
		if id, _ := s.Attr("id"); id != "grid-row-"+strconv.Itoa(i+1) {
			t.Errorf("grid row %d id = %q", i+1, id)
		}
	})
	row2 := page.Find("form#grid-row-2")
	if v, _ := row2.Find(`input[type="hidden"][name="item_id"]`).Attr("value"); v != "4" {
		t.Errorf("row 2 item id = %q, want 4", v)
	}
	if !strings.Contains(norm(row2.Text()), "MS Flat 40x6") {
		t.Error("row 2 lacks the item summary")
	}

	line := uitest.Doc(t, e.get(t, doc42+"/lines/2"))
	for _, f := range []string{"qty", "rate"} {
		g := row2.Find("#grid-2-" + f)
		if g.Length() != 1 {
			t.Errorf("row 2 has no #grid-2-%s", f)
			continue
		}
		l := line.Find(`[name="` + f + `"]`).Not(`[type="hidden"]`)
		for _, attr := range []string{"name", "value", "inputmode", "hx-post"} {
			gv, _ := g.Attr(attr)
			lv, _ := l.Attr(attr)
			if gv != lv {
				t.Errorf("#grid-2-%s %s = %q, the line page's = %q; want the same field component", f, attr, gv, lv)
			}
		}
	}
}

func TestLines_ShowAllAndFind(t *testing.T) {
	e := newLinesEnv(t, manyLines(64, 5, 30, 60))
	cards := func(target string) []string {
		t.Helper()
		res := e.get(t, target)
		if res.Code != http.StatusOK {
			t.Fatalf("GET %s: status %d", target, res.Code)
		}
		return uitest.Doc(t, res).Find("ol.lines > li.lines-card .lines-no").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
	}

	page := uitest.Doc(t, e.get(t, doc42+"/edit/items"))
	if n := page.Find("ol.lines > li.lines-card").Length(); n != 20 {
		t.Errorf("%d cards, want 20", n)
	}
	all := buttonsNamed(page.Selection, "Show all 64 lines")
	if all.Length() != 1 {
		t.Fatal("no \"Show all 64 lines\"")
	}
	href, _ := all.Attr("href")
	if u, err := url.Parse(href); err != nil || u.Path != doc42+"/edit/items" || u.Query().Get("all") != "1" {
		t.Errorf("Show all href = %q, want %s/edit/items?all=1", href, doc42)
	}
	if n := len(cards(doc42 + "/edit/items?all=1")); n != 64 {
		t.Errorf("?all=1: %d cards, want 64", n)
	}
	for _, q := range []string{"angle", "ANGLE", "ms-angle"} {
		if got := cards(doc42 + "/edit/items?find=" + url.QueryEscape(q)); !slices.Equal(got, []string{"5", "30", "60"}) {
			t.Errorf("?find=%s: cards %q, want lines 5, 30, 60", q, got)
		}
	}
}
