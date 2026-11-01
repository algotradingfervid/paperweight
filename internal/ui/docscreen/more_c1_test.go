package docscreen_test

import (
	"net/http"
	"net/url"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// UIV2-C1 (UI v2 §5.5 item 3, A-10): Field.More puts a field in its card's
// "More details" group at the end of the generic step and of the generic
// line page. The group is open when a field shown inside it has a value or
// an error; a field the module hides is never inside it, never named in its
// summary and never opens it.

const c1Type = "docscreentest.MoreC1"

var c1Once sync.Once

func registerC1() {
	c1Once.Do(func() {
		doctype.Register(doctype.DocType{
			Name: c1Type, Label: "More Order", PluralLabel: "More Orders", Table: "docscreentest_more", LinesTable: "docscreentest_more_line",
			Submittable: true,
			Fields: []doctype.Field{
				{Name: "ref", Label: "Reference", Type: doctype.Text, Required: true},
				{Name: "project", Label: "Project", Type: doctype.Text, More: true},
				{Name: "need_by", Label: "Required by", Type: doctype.Date, More: true},
				{Name: "secret", Label: "Secret", Type: doctype.Text, More: true},
				{Name: "urgent", Label: "Urgent", Type: doctype.Check, More: true},
				{Name: "no_print", Label: "No print", Type: doctype.Check, Invert: "Print", More: true},
				{Name: "item", Label: "Item", Type: doctype.Text, Required: true, OnLine: true},
				{Name: "line_note", Label: "Line note", Type: doctype.Text, OnLine: true, More: true},
				{Name: "line_secret", Label: "Line secret", Type: doctype.Text, OnLine: true, More: true},
			},
		})
	})
}

const c1Doc = "/doc/" + c1Type + "/50"

// newC1Env is the document harness with Priya holding every right on
// c1Type, whose module hides hide.
func newC1Env(t *testing.T, hide ...string) *docEnv {
	t.Helper()
	registerC1()
	e := newDocEnv(t)
	for _, r := range append([]auth.Right{"create"}, allRights...) {
		e.az.Grant("Buyer", c1Type, r, auth.ScopeAll)
	}
	e.d.Modules = map[string]docscreen.Module{c1Type: {Hide: m01bHide(hide...)}}
	e.remount()
	return e
}

// seedC1 stores draft 50 at version 3 with ref and one line, plus values.
func seedC1(e *docEnv, values map[string]string, line map[string]string) {
	rec := doc.Record{Version: 3, Values: map[string]string{"ref": "R-1"}, Lines: []map[string]string{{"id": "1", "item": "Bolt"}}}
	for k, v := range values {
		rec.Values[k] = v
	}
	for k, v := range line {
		rec.Lines[0][k] = v
	}
	docscreentest.Seed(e.docs, draftInfo(c1Type, "More Order", 50), rec)
}

// c1More is the page's More details group: exactly one, inside the form.
func c1More(t *testing.T, page *goquery.Document) *goquery.Selection {
	t.Helper()
	d := page.Find("form details.disclosure")
	if d.Length() != 1 {
		t.Fatalf("%d More details groups in the form, want 1", d.Length())
	}
	return d
}

func c1Open(d *goquery.Selection) bool {
	_, ok := d.Attr("open")
	return ok
}

func c1Get(t *testing.T, e *docEnv, target string) *goquery.Document {
	t.Helper()
	rec := e.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d; body %.300s", target, rec.Code, rec.Body)
	}
	return uitest.Doc(t, rec)
}

// The new page's step puts the shown More fields, in declared order, in one
// closed group after the other inputs; the hidden one is nowhere.
func TestMoreC1_StepGroupsMoreFields(t *testing.T) {
	e := newC1Env(t, "secret", "line_secret")
	page := c1Get(t, e, "/doc/"+c1Type+"/new")
	d := c1More(t, page)
	if got := norm(d.Find("summary").Text()); got != "More details · project, required by, urgent, print" {
		t.Errorf("summary = %q", got)
	}
	if c1Open(d) {
		t.Error("More details is open on a new page with nothing typed")
	}
	for _, id := range []string{"f-project", "f-need_by", "f-urgent"} {
		if d.Find("#"+id).Length() != 1 {
			t.Errorf("#%s is not inside More details", id)
		}
	}
	if page.Find("#f-ref").Length() != 1 || d.Find("#f-ref").Length() != 0 {
		t.Error("Reference should be outside More details")
	}
	if page.Find("[name=secret]").Length() != 0 {
		t.Error("the hidden Secret is rendered")
	}
	if d.Prev().Find("[name=ref]").Length() == 0 {
		t.Errorf("More details doesn't follow the other inputs; previous sibling %q", d.Prev().Text())
	}
}

// A-10, one row per field with the others blank: a value in any shown More
// field opens the group; a Check left false, a blank value and a value in
// the hidden field don't.
func TestMoreC1_OpenWhenAnyShownFieldHasAValue(t *testing.T) {
	for _, c := range []struct {
		name   string
		values map[string]string
		open   bool
	}{
		{"blank", nil, false},
		{"project", map[string]string{"project": "Panvel"}, true},
		{"need_by", map[string]string{"need_by": "2026-10-30"}, true},
		{"urgent true", map[string]string{"urgent": "true"}, true},
		{"urgent false", map[string]string{"urgent": "false"}, false},
		{"no_print true", map[string]string{"no_print": "true"}, true},
		{"hidden secret", map[string]string{"secret": "s3"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newC1Env(t, "secret")
			seedC1(e, c.values, nil)
			page := c1Get(t, e, c1Doc+"/edit/details")
			d := c1More(t, page)
			if c1Open(d) != c.open {
				t.Errorf("open = %v, want %v", c1Open(d), c.open)
			}
			if norm(d.Find("summary").Text()) != "More details · project, required by, urgent, print" {
				t.Errorf("summary = %q names the hidden field or misses one", norm(d.Find("summary").Text()))
			}
		})
	}
}

// A 422 redraw opens the group for an error on a field inside it, and for a
// typed value inside it; an error on the hidden field opens nothing.
func TestMoreC1_RedrawOpensOnErrorOrTypedValue(t *testing.T) {
	invalid := func(field string) error {
		return statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.",
			fields: []doctype.FieldError{{Field: field, Message: "Check this field."}}}
	}
	for _, c := range []struct {
		name  string
		err   string
		typed url.Values
		open  bool
	}{
		{"error inside", "need_by", url.Values{"ref": {"R-1"}}, true},
		{"error outside", "ref", url.Values{"ref": {""}}, false},
		{"typed inside", "ref", url.Values{"ref": {""}, "project": {"Panvel"}}, true},
		{"error on hidden", "secret", url.Values{"ref": {"R-1"}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newC1Env(t, "secret")
			seedC1(e, nil, nil)
			e.docs.SetResult(c1Type, 50, doctype.ActionSave, doc.Result{}, invalid(c.err))
			rec := e.post(t, c1Doc+"/edit/details", c.typed, "HX-Request", "true")
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
			}
			d := c1More(t, uitest.Doc(t, rec))
			if c1Open(d) != c.open {
				t.Errorf("open = %v, want %v", c1Open(d), c.open)
			}
			if c.err == "need_by" && d.Find("#f-need_by-msg").Text() == "" {
				t.Error("the error isn't shown on Required by inside More details")
			}
		})
	}
	t.Run("new page", func(t *testing.T) {
		e := newC1Env(t)
		e.docs.SetResult(c1Type, 0, doctype.ActionSave, doc.Result{}, invalid("project"))
		rec := e.post(t, "/doc/"+c1Type+"/new", url.Values{"ref": {"R-1"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
		}
		if !c1Open(c1More(t, uitest.Doc(t, rec))) {
			t.Error("an error on Project leaves More details closed on the new page")
		}
	})
}

// With every More field hidden there is no group at all.
func TestMoreC1_AllHiddenNoGroup(t *testing.T) {
	e := newC1Env(t, "project", "need_by", "secret", "urgent", "no_print")
	seedC1(e, map[string]string{"project": "Panvel"}, nil)
	page := c1Get(t, e, c1Doc+"/edit/details")
	if n := page.Find("details.disclosure").Length(); n != 0 {
		t.Errorf("%d More details groups with every More field hidden, want 0", n)
	}
}

// The generic line page groups the line's More fields the same way.
func TestMoreC1_LinePage(t *testing.T) {
	for _, c := range []struct {
		name   string
		target string
		line   map[string]string
		open   bool
	}{
		{"new line", c1Doc + "/lines/new", nil, false},
		{"line with note", c1Doc + "/lines/1", map[string]string{"line_note": "Fragile"}, true},
		{"line with hidden secret", c1Doc + "/lines/1", map[string]string{"line_secret": "s3"}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newC1Env(t, "line_secret")
			seedC1(e, nil, c.line)
			page := c1Get(t, e, c.target)
			d := c1More(t, page)
			if got := norm(d.Find("summary").Text()); got != "More details · line note" {
				t.Errorf("summary = %q", got)
			}
			if c1Open(d) != c.open {
				t.Errorf("open = %v, want %v", c1Open(d), c.open)
			}
			if d.Find("#f-line_note").Length() != 1 || d.Find("#f-item").Length() != 0 {
				t.Error("Line note should be inside More details and Item outside")
			}
			if page.Find("[name=line_secret]").Length() != 0 {
				t.Error("the hidden line field is rendered")
			}
		})
	}
	t.Run("line error", func(t *testing.T) {
		e := newC1Env(t, "line_secret")
		seedC1(e, nil, nil)
		e.docs.SetResult(c1Type, 50, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusUnprocessableEntity,
			msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{{Field: "line_note", Line: 1, Message: "Line 1: Check this field."}}})
		rec := e.post(t, c1Doc+"/lines/1", url.Values{"item": {"Bolt"}})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
		}
		if !c1Open(c1More(t, uitest.Doc(t, rec))) {
			t.Error("a line error on Line note leaves More details closed")
		}
	})
}
