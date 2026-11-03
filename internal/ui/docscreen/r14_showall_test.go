package docscreen_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// r14Reveal is the id of the first line Show all reveals (UI v2 §5.11,
// design-system §8.2): the fragment Show all links to.
const r14Reveal = "line-reveal"

// r14Cards are the line card numbers in the page's line list.
func r14Cards(page *goquery.Document) []string {
	return page.Find(".linelist ol.lines > li.lines-card .lines-no").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
}

// r14Page checks one page of a document with n lines: with 20 lines or
// fewer every card and no Show all; with more, the first 20 cards and
// "Show all {n} lines" linking to path?all=1#line-reveal, and no Find form
// (that is the Items step's); ?all=1 shows every card, the 21st the
// focusable target of the link. The grand total shows either way.
func r14Page(t *testing.T, e *docEnv, path string, n int) {
	t.Helper()
	what := "GET " + path + " with " + strconv.Itoa(n) + " lines"
	page := m01bOK(t, e.get(t, path), what)
	all := buttonsNamed(page.Selection, "Show all "+strconv.Itoa(n)+" lines")
	if !strings.Contains(norm(page.Find("main").Text()), "₹94,164.00") {
		t.Errorf("%s: no grand total ₹94,164.00 on the page", what)
	}
	if page.Find(".linelist form[method=\"get\"]").Length() != 0 {
		t.Errorf("%s: the line list has a Find form; only the Items step finds a line", what)
	}
	cards := r14Cards(page)
	if n <= 20 {
		if len(cards) != n || all.Length() != 0 {
			t.Errorf("%s: %d cards and %d Show all, want %d and none", what, len(cards), all.Length(), n)
		}
	} else {
		if len(cards) != 20 || cards[0] != "1" || cards[19] != "20" {
			t.Errorf("%s: cards %q, want lines 1 to 20", what, cards)
		}
		if all.Length() != 1 {
			t.Fatalf("%s: %d \"Show all %d lines\", want one", what, all.Length(), n)
		}
		href, _ := all.Attr("href")
		u, err := url.Parse(href)
		if err != nil || u.Path != path || u.Query().Get("all") != "1" || u.Fragment != r14Reveal {
			t.Errorf("%s: Show all href = %q, want %s?all=1#%s", what, href, path, r14Reveal)
		}
	}
	if page.Find("#"+r14Reveal).Length() != 0 {
		t.Errorf("%s: #%s without ?all=1", what, r14Reveal)
	}

	yes := m01bOK(t, e.get(t, path+"?all=yes"), what+" ?all=yes")
	wantShow := 0
	if n > 20 {
		wantShow = 1
	}
	if got, b := len(r14Cards(yes)), buttonsNamed(yes.Selection, "Show all "+strconv.Itoa(n)+" lines").Length(); got != min(n, 20) || b != wantShow || yes.Find("#"+r14Reveal).Length() != 0 {
		t.Errorf("%s ?all=yes: %d cards, %d Show all, %d #%s; want only ?all=1 to show all", what, got, b, yes.Find("#"+r14Reveal).Length(), r14Reveal)
	}

	full := m01bOK(t, e.get(t, path+"?all=1"), what+" ?all=1")
	if got := r14Cards(full); len(got) != n {
		t.Errorf("%s ?all=1: %d cards, want %d", what, len(got), n)
	}
	if buttonsNamed(full.Selection, "Show all "+strconv.Itoa(n)+" lines").Length() != 0 {
		t.Errorf("%s ?all=1: Show all is still there", what)
	}
	target := full.Find("#" + r14Reveal)
	if n <= 20 {
		if target.Length() != 0 {
			t.Errorf("%s ?all=1: #%s with nothing revealed", what, r14Reveal)
		}
		return
	}
	if target.Length() != 1 || !target.Is("li.lines-card") || norm(target.Find(".lines-no").Text()) != "21" {
		t.Fatalf("%s ?all=1: #%s is %d elements (%q), want line 21's card", what, r14Reveal, target.Length(), norm(target.Text()))
	}
	if ti, _ := target.Attr("tabindex"); ti != "-1" {
		t.Errorf("%s ?all=1: line 21's tabindex = %q, want -1 so the link's fragment focuses it", what, ti)
	}
}

// R14: the check page and the document page show a long line list's first
// 20 lines and "Show all {n} lines" (design-system §8.2), tested at the
// threshold and one over it.
func TestR14_CheckAndPageShowAll(t *testing.T) {
	for _, n := range []int{20, 21} {
		t.Run(strconv.Itoa(n)+" lines check", func(t *testing.T) {
			e := newLinesEnv(t, manyLines(n))
			r14Page(t, e, doc42+"/edit/check", n)
		})
		t.Run(strconv.Itoa(n)+" lines page", func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			setLineItems()
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateApproved), manyLines(n))
			r14Page(t, e, doc42, n)
		})
	}
}

// R14 on the real engine: Show all counts the module's cards by place, not
// by the number a module's LineCard gives them (GRN numbers a card by its
// PO line); the display grid keeps every row; the project the module hides
// (Hooked's card shows its first Link) stays out of the revealed cards.
func TestR14_ModuleCardsShowAll(t *testing.T) {
	m := m47CardModule()
	m.Hide = m01bHide("project_id")
	card := m.LineCard
	m.LineCard = func(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
		lc := card(c, l)
		lc.No = 100 + l.No
		return lc
	}
	e, id := m20Env(t, 21, m)
	v, err := e.d.View(t.Context(), e.open, docscreentest.Hooked, id)
	if err != nil {
		t.Fatalf("View: %v", err)
	}
	name, code := v.Lines[20].Card.Item, v.Lines[20].Card.Code
	if code != "PRJ-METRO" || name == "" {
		t.Fatalf("View's line 21 card = %q, %q; want its project, PRJ-METRO", name, code)
	}
	check := func(path string) {
		t.Helper()
		page := m01bOK(t, e.get(t, path), "GET "+path)
		if got := r14Cards(page); len(got) != 20 || got[19] != "120" {
			t.Errorf("GET %s: cards %q, want the first 20 (101 to 120)", path, got)
		}
		if buttonsNamed(page.Selection, "Show all 21 lines").Length() != 1 {
			t.Errorf("GET %s: no \"Show all 21 lines\"", path)
		}
		if n := page.Find(".line-editor .linegrid-row").Length(); n != 21 {
			t.Errorf("GET %s: %d display grid rows, want all 21", path, n)
		}
		full := m01bOK(t, e.get(t, path+"?all=1"), "GET "+path+"?all=1")
		target := full.Find("#" + r14Reveal)
		if norm(target.Find(".lines-no").Text()) != "121" {
			t.Errorf("GET %s?all=1: #%s is %q, want the 21st card (121)", path, r14Reveal, norm(target.Text()))
		}
		got := norm(target.Text())
		if !strings.Contains(got, m47CardNote) {
			t.Errorf("GET %s?all=1: the 21st card %q lacks the module's note", path, got)
		}
		for _, s := range []string{name, code} {
			if strings.Contains(got, s) {
				t.Errorf("GET %s?all=1: the 21st card %q names the hidden project's %q", path, got, s)
			}
		}
	}
	check(m01bURL(id) + "/edit/check")
	sub, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSubmit, doc.ActionInput{Version: m01bRecord(t, e, id).Version})
	if err != nil || sub.State != doctype.StateApproved {
		t.Fatalf("submitting Hooked %d: state %s, err %v; want approved", id, sub.State, err)
	}
	check(m01bURL(id))
}

// r14Redraw checks a redraw's line list: every card, the 21st the target,
// when all; else the first 20 and Show all.
func r14Redraw(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, all bool) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("%s = %d, want %d; body %.400s", what, rec.Code, status, rec.Body)
	}
	page := uitest.Doc(t, rec)
	cards, show, target := len(r14Cards(page)), buttonsNamed(page.Selection, "Show all 21 lines").Length(), page.Find("#"+r14Reveal).Length()
	if all && (cards != 21 || show != 0 || target != 1) {
		t.Errorf("%s: %d cards, %d Show all, %d #%s; want all 21, no Show all, the target", what, cards, show, target, r14Reveal)
	}
	if !all && (cards != 20 || show != 1 || target != 0) {
		t.Errorf("%s: %d cards, %d Show all, %d #%s; want 20 and Show all", what, cards, show, target, r14Reveal)
	}
}

// r14Hdrs are a redraw's request headers: htmx with HX-Current-URL cur,
// or none ("" is a full-page post).
func r14Hdrs(htmx bool, cur string) []string {
	var h []string
	if htmx {
		h = append(h, "HX-Request", "true")
	}
	if cur != "" {
		h = append(h, "HX-Current-URL", "http://example.test"+cur)
	}
	return h
}

// R14 review: a refused htmx post redraws its page without a new address,
// so the address bar keeps ?all=1, and the redraw shows every line too:
// the document page (a stale approve), the check page (a submit refused
// with an error no other step owns) and the Items step (a refused step
// save). Only htmx's HX-Current-URL on the same page counts: another
// page's ?all=1, ?all=yes, no HX-Request or no header draws 20.
func TestR14_RedrawKeepsAllLines(t *testing.T) {
	type redraw struct {
		name, path, post string
		status           int
		setup            func(e *docEnv)
	}
	pages := []redraw{
		{"document page", doc42, doc42 + "/approve", http.StatusConflict, func(e *docEnv) {
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StatePending), manyLines(21))
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove, doc.Result{}, f3Conflict)
		}},
		{"check page", doc42 + "/edit/check", doc42 + "/submit", http.StatusUnprocessableEntity, func(e *docEnv) {
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), manyLines(21))
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{}, statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{{Message: "Something is wrong."}}})
		}},
		{"items step", doc42 + "/edit/items", doc42 + "/edit/items", http.StatusUnprocessableEntity, func(e *docEnv) {
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), manyLines(21))
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{}, statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{{Field: "qty", Line: 2, Message: "Quantity is wrong."}}})
		}},
	}
	for _, p := range pages {
		for _, c := range []struct {
			name string
			htmx bool
			cur  string
			all  bool
		}{
			{"htmx at ?all=1", true, p.path + "?all=1", true},
			{"htmx at the page", true, p.path, false},
			{"htmx at ?all=yes", true, p.path + "?all=yes", false},
			{"htmx at another page's ?all=1", true, doc42 + "/edit/details?all=1", false},
			{"full post with the header", false, p.path + "?all=1", false},
			{"htmx with no current URL", true, "", false},
		} {
			t.Run(p.name+" "+c.name, func(t *testing.T) {
				e := newFormEnv(t, allRights...)
				setLineItems()
				p.setup(e)
				rec := e.post(t, p.post, url.Values{"version": {"6"}}, r14Hdrs(c.htmx, c.cur)...)
				r14Redraw(t, "POST "+p.post, rec, p.status, c.all)
			})
		}
	}
}

// R14 review: the Items step's Show all goes to ?all=1#line-reveal too, and
// there the 21st card is the target; at 20 lines there is none.
func TestR14_ItemsShowAllTarget(t *testing.T) {
	e := newLinesEnv(t, manyLines(21))
	page := m01bOK(t, e.get(t, doc42+"/edit/items"), "GET items")
	all := buttonsNamed(page.Selection, "Show all 21 lines")
	if href, _ := all.Attr("href"); href != doc42+"/edit/items?all=1#"+r14Reveal {
		t.Errorf("Show all href = %q, want %s/edit/items?all=1#%s", href, doc42, r14Reveal)
	}
	full := m01bOK(t, e.get(t, doc42+"/edit/items?all=1"), "GET items?all=1")
	target := full.Find("#" + r14Reveal)
	if target.Length() != 1 || !target.Is("li.lines-card") || norm(target.Find(".lines-no").Text()) != "21" || target.AttrOr("tabindex", "") != "-1" {
		t.Errorf("items ?all=1: #%s = %d, %q; want line 21's card, tabindex -1", r14Reveal, target.Length(), norm(target.Text()))
	}
	e = newLinesEnv(t, manyLines(20))
	if n := m01bOK(t, e.get(t, doc42+"/edit/items?all=1"), "GET 20 lines items?all=1").Find("#" + r14Reveal).Length(); n != 0 {
		t.Errorf("20 lines, items ?all=1: %d #%s, want none", n, r14Reveal)
	}
}
