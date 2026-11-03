package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// r7Errors is each field error the page's error summary lists, by its link.
func r7Errors(page *goquery.Document) []string {
	return page.Find(".error-summary a").Map(func(_ int, s *goquery.Selection) string {
		return strings.TrimPrefix(s.AttrOr("href", ""), "#") + " " + norm(s.Text())
	})
}

// CD-387 (3): a line with nothing identifying it (every starred Link on the
// line blank) is refused at Save line with each blank starred field's error,
// and nothing is written; a line with its item saves, whatever else is blank.
func TestR7_BlankLineRefused(t *testing.T) {
	ok := doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}
	for _, c := range []struct {
		name string
		form url.Values
		want []string
	}{
		{"quantity alone", url.Values{"qty": {"5"}, "rate": {"10"}}, []string{"f-item_id Item is required."}},
		{"nothing", url.Values{}, []string{"f-item_id Item is required.", "f-qty Quantity is required."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLinesEnv(t, threeLines())
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
			c.form.Set("version", "6")
			for _, path := range []string{"/lines/new", "/lines/2"} {
				rec := e.post(t, doc42+path, c.form, "HX-Request", "true", "HX-Target", "main")
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("%s: status = %d, want 422; body %.300s", path, rec.Code, rec.Body)
				}
				if got := r7Errors(uitest.Doc(t, rec)); !slices.Equal(got, c.want) {
					t.Errorf("%s: errors = %q, want %q", path, got, c.want)
				}
			}
			if calls := e.docs.Calls(); len(calls) != 0 {
				t.Errorf("engine calls = %+v, want none", calls)
			}
		})
	}
	t.Run("item alone saves", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
		rec := e.post(t, doc42+"/lines/new", url.Values{"version": {"6"}, "item_id": {"5"}})
		if got := location(t, rec); got != doc42+"/edit/items" {
			t.Errorf("303 to %q, want the items step; body %.300s", got, rec.Body)
		}
		if in := oneSave(t, e); len(in.Lines) != 4 || in.Lines[3]["item_id"] != "5" {
			t.Errorf("saved lines = %q, want a 4th with item 5", in.Lines)
		}
	})
	t.Run("grid row with its item cleared", func(t *testing.T) {
		e := newLinesEnv(t, threeLines())
		e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
		// The grid has no control to clear the item; a crafted post has.
		form := url.Values{"version": {"6"}, "item_id": {""}, "qty": {"25"}, "rate": {"62.5"}}
		rec := e.post(t, doc42+"/lines/2", form, "HX-Request", "true", "HX-Target", "grid-row-2", "HX-Trigger", "grid-row-2")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
		}
		row := uitest.Doc(t, rec).Find("form#grid-row-2")
		if row.Length() != 1 || !strings.Contains(norm(row.Text()), "Item is required.") {
			t.Errorf("grid row: %d, text %q; want the row with Item is required.", row.Length(), norm(row.Text()))
		}
		if calls := e.docs.Calls(); len(calls) != 0 {
			t.Errorf("engine calls = %+v, want none", calls)
		}
	})
	t.Run("a module's Line.Save", func(t *testing.T) {
		e, id := m01bHooked(t, docscreen.Module{})
		qtyOnly := func(c docscreen.Ctx, _ int) (docscreen.Save, error) {
			plain, _ := docscreen.ToPlain(m03Field("qty"), c.Req.PostFormValue("qty"))
			return docscreen.Save{Values: map[string]string{"qty": plain, "item_id": c.Req.PostFormValue("item_id")}}, nil
		}
		body := func(docscreen.Ctx, int, docscreen.StepState) (templ.Component, error) { return templ.Raw(""), nil }
		m01bMount(e, docscreentest.Hooked, docscreen.Module{Line: &docscreen.Line{Body: body, Save: qtyOnly}})
		base := m01bURL(id)
		before := m01bRecord(t, e, id)
		version := strconv.Itoa(before.Version)
		for _, path := range []string{"/lines/new", "/lines/1"} {
			rec := e.post(t, base+path, url.Values{"version": {version}, "qty": {"4"}, "item_id": {""}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("%s: status = %d, want 422; body %.300s", path, rec.Code, rec.Body)
			}
			if got := r7Errors(uitest.Doc(t, rec)); !slices.Equal(got, []string{"f-item_id Item is required."}) {
				t.Errorf("%s: errors = %q", path, got)
			}
		}
		m03Unchanged(t, e, id, before, "a blank Line.Save")
	})
}

// A line whose only starred Link the module hides has nothing to identify it
// by: no line is refused for being blank (CD-387 (3)).
func TestR7_BlankLineNeedsAStarredLink(t *testing.T) {
	ok := doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}
	for name, m := range map[string]docscreen.Module{
		"item hidden": {Hide: m01bHide("item_id")},
	} {
		t.Run(name, func(t *testing.T) {
			e := newLinesEnv(t, threeLines())
			e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: m}
			e.remount()
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, ok, nil)
			rec := e.post(t, doc42+"/lines/new", url.Values{"version": {"6"}})
			if got := location(t, rec); got != doc42+"/edit/items" {
				t.Errorf("303 to %q, want the items step; status %d, body %.300s", got, rec.Code, rec.Body)
			}
		})
	}
}

// R7: a module's CheckExtra.Problems mark the section of the step owning
// each one Needs attention and list its message there; a problem no step
// owns is an internal error, never dropped.
func TestR7_CheckModuleProblems(t *testing.T) {
	mount := func(e *docEnv, problems []doctype.FieldError) {
		e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
			return docscreen.CheckExtra{Problems: problems}, nil
		}}}
		e.remount()
	}
	for _, c := range []struct {
		name     string
		problems []doctype.FieldError
		details  string
		items    string
		listed   map[string][]string
	}{
		{"none", nil, "Complete", "Complete", nil},
		{"a header field", []doctype.FieldError{{Field: "remarks", Message: "Remarks need the site name."}}, "Needs attention", "Complete",
			map[string][]string{"Details": {"Remarks need the site name."}}},
		{"a line", []doctype.FieldError{{Field: "qty", Line: 1, Message: "Line 1: Tax code not found for this item."}}, "Complete", "Needs attention",
			map[string][]string{"Items": {"Line 1: Tax code not found for this item."}}},
		{"the lines as a whole", []doctype.FieldError{{Field: "lines", Message: "Add the freight line."}}, "Complete", "Needs attention",
			map[string][]string{"Items": {"Add the freight line."}}},
		{"a line, no field", []doctype.FieldError{{Line: 1, Message: "Line 1: Add a photo of the bill."}}, "Complete", "Needs attention",
			map[string][]string{"Items": {"Line 1: Add a photo of the bill."}}},
		{"one message twice", []doctype.FieldError{{Field: "remarks", Message: "Remarks need the site name."}, {Field: "priority", Message: "Remarks need the site name."}}, "Needs attention", "Complete",
			map[string][]string{"Details": {"Remarks need the site name."}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			e.seed(doctype.StateDraft)
			mount(e, c.problems)
			page := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
			if got := bannerTexts(page.Find("#wizard"), "warning"); len(got) != 0 {
				t.Errorf("top warnings = %q, want none: each problem has its section", got)
			}
			for title, want := range map[string]string{"Details": c.details, "Items": c.items} {
				sec := checkSection(page.Selection, title)
				if got := norm(sec.Find(".check-status").Text()); got != want {
					t.Errorf("%s = %q, want %q", title, got, want)
				}
				got := sec.Find("ul > li").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
				if !slices.Equal(got, c.listed[title]) {
					t.Errorf("%s lists %q, want %q", title, got, c.listed[title])
				}
			}
		})
	}
	t.Run("a field the module hides", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {
			Hide: m01bHide("remarks"),
			Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
				return docscreen.CheckExtra{Problems: []doctype.FieldError{{Field: "remarks", Message: "Remarks need the site name."}}}, nil
			},
		}}
		e.remount()
		sec := checkSection(uitest.Doc(t, e.get(t, doc42+"/edit/check")).Selection, "Details")
		if got := norm(sec.Find(".check-status").Text()); got != "Complete" {
			t.Errorf("Details = %q, want Complete", got)
		}
		if n := sec.Find("ul > li").Length(); n != 0 {
			t.Errorf("Details lists %d problems, want none", n)
		}
	})
	// Field "" is the document's own problem: the Check page's top summary
	// lists it, and no section changes.
	t.Run("the document's own", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		mount(e, []doctype.FieldError{{Message: "The vendor is on hold."}})
		rec := e.get(t, doc42+"/edit/check")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		page := uitest.Doc(t, rec)
		if got := bannerTexts(page.Find("#wizard"), "warning"); !slices.Equal(got, []string{"The vendor is on hold."}) {
			t.Errorf("top warnings = %q, want [The vendor is on hold.]", got)
		}
		for _, title := range []string{"Details", "Items"} {
			if got := norm(checkSection(page.Selection, title).Find(".check-status").Text()); got != "Complete" {
				t.Errorf("%s = %q, want Complete", title, got)
			}
		}
	})
	t.Run("no step owns it", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		mount(e, []doctype.FieldError{{Field: "no_such", Message: "Lost."}})
		if rec := e.get(t, doc42+"/edit/check"); rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500", rec.Code)
		}
	})
}

// CD-387 (4): the step form is marked unsaved only where it redraws posted
// values nothing saved (a step or new post's 422 and 409), never over stored
// values (a submit or move refusal), so the × asks only where typing is lost.
func TestR7_UnsavedMark(t *testing.T) {
	marked := func(t *testing.T, rec *httptest.ResponseRecorder) bool {
		t.Helper()
		page := uitest.Doc(t, rec)
		if page.Find("form#wizard-form").Length() != 1 {
			t.Fatalf("status %d: no step form; body %.300s", rec.Code, rec.Body)
		}
		_, ok := page.Find("form#wizard-form").Attr("data-unsaved")
		return ok
	}
	stale := &doc.Error{Kind: doc.KindStale, Message: "Someone else saved this draft."}
	invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields.",
		fields: []doctype.FieldError{{Field: "qty", Line: 2, Message: "Line 2: Quantity must be more than 0."}}}
	for _, c := range []struct {
		name   string
		result error
		action doctype.Action
		path   string
		form   url.Values
		status int
		want   bool
	}{
		{"step 422", nil, "", doc42 + "/edit/details", url.Values{"version": {"6"}, "supplier_id": {"7"}, "order_date": {"31-02-2026"}}, http.StatusUnprocessableEntity, true},
		{"step 409", stale, doctype.ActionSave, doc42 + "/edit/details", url.Values{"version": {"6"}, "supplier_id": {"7"}, "order_date": {"24-10-2026"}}, http.StatusConflict, true},
		{"new 422", nil, "", samplePath + "/new", url.Values{"supplier_id": {"7"}, "order_date": {"31-02-2026"}}, http.StatusUnprocessableEntity, true},
		{"submit 422", invalid, doctype.ActionSubmit, doc42 + "/submit", url.Values{"version": {"6"}}, http.StatusUnprocessableEntity, false},
		{"move 422", invalid, doctype.ActionSave, doc42 + "/lines/1/move?dir=down", url.Values{"version": {"6"}}, http.StatusUnprocessableEntity, false},
		{"move 409", stale, doctype.ActionSave, doc42 + "/lines/1/move?dir=down", url.Values{"version": {"6"}}, http.StatusConflict, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newLinesEnv(t, threeLines())
			for _, r := range []auth.Right{"create"} {
				e.az.Grant("Buyer", docscreentest.Sample, r, auth.ScopeAll)
			}
			if c.result != nil {
				e.docs.SetResult(docscreentest.Sample, 42, c.action, doc.Result{}, c.result)
			}
			rec := e.post(t, c.path, c.form)
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d; body %.300s", rec.Code, c.status, rec.Body)
			}
			if got := marked(t, rec); got != c.want {
				t.Errorf("data-unsaved = %v, want %v", got, c.want)
			}
		})
	}
}
