package docscreen_test

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// R1 (EXP review blocker): the line and remove pages post through htmx, so a
// dropped connection reaches the network banner. Each keeps its plain POST.

// linePageHeaders is what htmx sends for the page form; a grid row names itself.
var linePageHeaders = []string{"HX-Request", "true", "HX-Target", "main"}

func TestLines_PageFormsPostThroughHTMX(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	for _, path := range []string{doc42 + "/lines/new", doc42 + "/lines/1", doc42 + "/lines/1/remove"} {
		f := uitest.Doc(t, e.get(t, path)).Find(`#main form[action="` + path + `"]`)
		if f.Length() != 1 {
			t.Fatalf("%s: %d forms posting to the page, want 1", path, f.Length())
		}
		if got := f.AttrOr("hx-post", ""); got != path {
			t.Errorf("%s: hx-post = %q", path, got)
		}
		if got := f.AttrOr("hx-target", ""); got != "#main" {
			t.Errorf("%s: hx-target = %q, want #main", path, got)
		}
		if got := f.AttrOr("method", ""); got != "post" {
			t.Errorf("%s: method = %q, want post: a plain POST still works", path, got)
		}
	}
}

// The grid is on this document too, and a page post must not get a row back.
func TestLines_PagePostOnAGridDocumentIsNotARowSave(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	page := uitest.Doc(t, e.get(t, doc42+"/edit/items"))
	if page.Find("form#grid-row-1").Length() != 1 {
		t.Fatal("the items step has no grid: this test needs one")
	}
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
	form := url.Values{"item_id": {"1"}, "qty": {"7"}, "rate": {"10"}, "version": {"6"}}

	res := e.post(t, doc42+"/lines/1", form, linePageHeaders...)
	if res.Code != http.StatusOK || res.Header().Get("HX-Redirect") != doc42+"/edit/items" {
		t.Fatalf("saved: status %d, HX-Redirect %q, want 200 and the items step; body %.300s", res.Code, res.Header().Get("HX-Redirect"), res.Body)
	}
	if res.Body.Len() != 0 {
		t.Errorf("saved: body %.200s, want none", res.Body)
	}

	form.Set("qty", "not a number")
	res = e.post(t, doc42+"/lines/1", form, linePageHeaders...)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("refused: status %d, body %.300s", res.Code, res.Body)
	}
	body := res.Body.String()
	// The page's <title> leads, "Error: " in front (R2), then #main.
	title, frag, _ := strings.Cut(strings.TrimSpace(body), "</title>")
	if !strings.HasPrefix(title, "<title>Error: ") || !strings.HasPrefix(frag, `<main id="main"`) {
		t.Errorf("refused: answer starts %.120s, want the Error: title and the #main fragment", body)
	}
	if !strings.Contains(body, `value="not a number"`) {
		t.Errorf("refused: the typed value is lost: %.300s", body)
	}
}

// A page post for a line removed since the page was read redraws its form with
// the 409, as a plain one does (F2); a grid row has no form, so it gets the 404.
func TestLines_GonePagePostRedraws(t *testing.T) {
	e, id := m01bHooked(t, docscreen.Module{})
	base := m01bURL(id)
	form := e.m01bLine1(strconv.Itoa(m01bRecord(t, e, id).Version + 5))
	form.Set("qty", "9")
	if res := e.post(t, base+"/lines/3", form, linePageHeaders...); res.Code != http.StatusConflict {
		t.Errorf("page post: status %d, want 409; body %.200s", res.Code, res.Body)
	}
	if res := e.post(t, base+"/lines/3", form, "HX-Request", "true", "HX-Target", "grid-row-3"); res.Code != http.StatusNotFound {
		t.Errorf("grid post: status %d, want 404", res.Code)
	}
}

// Past draft, a line POST that isn't a 409 still goes to the document page; htmx
// can't follow a 303 into a page swap, so it gets HX-Redirect.
func TestLines_RemoveAnswersHTMXWithRedirect(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
	res := e.post(t, doc42+"/lines/1/remove", url.Values{"version": {"6"}}, linePageHeaders...)
	if res.Code != http.StatusOK || res.Header().Get("HX-Redirect") != doc42+"/edit/items" {
		t.Fatalf("status %d, HX-Redirect %q; body %.300s", res.Code, res.Header().Get("HX-Redirect"), res.Body)
	}
	plain := newLinesEnv(t, threeLines())
	plain.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
	res = plain.post(t, doc42+"/lines/1/remove", url.Values{"version": {"6"}})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != doc42+"/edit/items" {
		t.Errorf("plain: status %d, Location %q", res.Code, res.Header().Get("Location"))
	}
}

func TestLines_DraftViewRedirectsHTMX(t *testing.T) {
	e := newFormEnv(t, allRights...)
	setLineItems()
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateApproved), doc.Record{Version: 6})
	res := e.get(t, doc42+"/lines/new", linePageHeaders...)
	if res.Code != http.StatusOK || res.Header().Get("HX-Redirect") != doc42 {
		t.Errorf("status %d, HX-Redirect %q; body %.200s", res.Code, res.Header().Get("HX-Redirect"), res.Body)
	}
}

// A new line has no grid row to answer with, whatever htmx names as its target.
func TestLines_NewLineIsNeverARowSave(t *testing.T) {
	e := newLinesEnv(t, threeLines())
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
	form := url.Values{"item_id": {"1"}, "qty": {"7"}, "rate": {"10"}, "version": {"6"}}
	res := e.post(t, doc42+"/lines/new", form, "HX-Request", "true", "HX-Target", "grid-row-1")
	if res.Code != http.StatusOK || res.Header().Get("HX-Redirect") != doc42+"/edit/items" {
		t.Errorf("status %d, HX-Redirect %q, want the items step; body %.200s", res.Code, res.Header().Get("HX-Redirect"), res.Body)
	}
}

// With the module's Line, Save line sits in the bar outside the form, and a
// post disables it with the form's own buttons, or a double tap sends twice.
func TestLines_ModuleLineDisablesItsBarButton(t *testing.T) {
	e, id, _ := m03Env(t, nil)
	page := uitest.Doc(t, e.get(t, m01bURL(id)+"/lines/1"))
	const want = "#line-form button[type=submit], button[form=line-form]"
	if got := page.Find("form#line-form").AttrOr("hx-disabled-elt", ""); got != want {
		t.Errorf("hx-disabled-elt = %q, want %q", got, want)
	}
}
