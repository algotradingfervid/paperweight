//go:build e2e

package e2e

import (
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// listPaths are every module list, from the app's own docscreen Deps (not a
// test-local map), and the party list, the master with the most per row.
func (h *Harness) listPaths() []string {
	var out []string
	for _, n := range slices.Sorted(maps.Keys(h.App.DocDeps().Modules)) {
		out = append(out, "/doc/"+n)
	}
	return append(out, "/m/party")
}

// openAt loads path in a w × ht viewport, as a phone below 64rem, and waits
// for the first list row.
func (s *Session) openAt(w, ht int64, path string) {
	s.t.Helper()
	var opts []chromedp.EmulateViewportOption
	if w < 1024 {
		opts = append(opts, chromedp.EmulateMobile, chromedp.EmulateTouch)
	}
	s.Do("open "+path, chromedp.EmulateViewport(w, ht, opts...), chromedp.Navigate(s.h.URL+path))
	s.Until("the first row of "+path, `document.querySelector("ul.datalist-rows > li") !== null`)
}

// listTopJS measures the list top on a phone: the first row's top against
// the bottom nav's, the bar's New and Export when shown, the quick chips'
// overlaps and spill past the viewport.
const listTopJS = `(() => {
  const vis = (e) => e.checkVisibility();
  const row = document.querySelector("ul.datalist-rows > li").getBoundingClientRect();
  const nav = document.querySelector("nav.bottomnav");
  const limit = nav && vis(nav) ? nav.getBoundingClientRect().top : innerHeight;
  const out = [];
  if (row.top + 72 > limit) out.push("first row starts at " + Math.round(row.top) + ", under the fold at " + Math.round(limit));
  for (const a of document.querySelectorAll(".filterbar-result a.btn")) {
    if (vis(a)) out.push("the bar shows " + a.innerText.trim() + " on a phone");
  }
  const chips = [...document.querySelectorAll(".filterbar-chips a")].filter(vis).map((a) => [a.innerText.trim(), a.getBoundingClientRect()]);
  for (const [i, [t, r]] of chips.entries()) {
    if (r.left < 0 || r.right > innerWidth) out.push("chip " + t + " spills past the viewport");
    for (const [u, q] of chips.slice(i + 1)) {
      if (r.left < q.right && q.left < r.right && r.top < q.bottom && q.top < r.bottom) out.push("chips " + t + " and " + u + " overlap");
    }
  }
  if (!document.querySelector('details.filterbar-panel a[href*="export.csv"]')) out.push("no Export in the Filters panel");
  return out;
})()`

// R5 (UI v2 §5.12, CD-366, A-23): on phones of 320, 360 and 390 px and a
// 768 px tablet (shell M5, GRN M3), every
// module list and the party list show the first row's top two lines above
// the bottom nav; New is the bottom nav's and Export is in the Filters
// panel, so the bar shows neither; the quick chips wrap with no overlap or
// spill, no target is under 44 px and nothing scrolls sideways. Opened at
// each width, the Filters sheet stays inside the viewport with full-size
// targets.
func TestE2E_ListTopOnPhones(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, phone390)
	for _, sz := range [][2]int64{{320, 568}, {360, 640}, {390, 664}, {768, 1024}} {
		for _, p := range h.listPaths() {
			s.openAt(sz[0], sz[1], p)
			what := p + " at " + strconv.FormatInt(sz[0], 10)
			for _, m := range Eval[[]string](s, listTopJS) {
				t.Errorf("%s: %s", what, m)
			}
			layoutProblems(t, s, what)
			s.Do("open Filters", chromedp.Click("details.filterbar-panel > summary"))
			s.Until("the Filters sheet", `document.querySelector(".filterbar-sheet").checkVisibility()`)
			if out := Eval[string](s, `(() => {
  const r = document.querySelector(".filterbar-sheet").getBoundingClientRect();
  return r.left < 0 || r.right > innerWidth ? "sheet " + Math.round(r.left) + "-" + Math.round(r.right) + " of " + innerWidth : "";
})()`); out != "" {
				t.Errorf("%s: %s", what, out)
			}
			layoutProblems(t, s, what+" with Filters open")
		}
	}
}

// layoutProblems reports sideways scroll and small targets on the page.
func layoutProblems(t *testing.T, s *Session, what string) {
	t.Helper()
	over, err := uibrowser.Overflow(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	small, err := uibrowser.SmallTargets(s.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range slices.Concat(over, small) {
		t.Errorf("%s: %s", what, m)
	}
}

// columnsJS checks a list's aligned rows, every row: each header label
// starts where its column's cells start (figures end where they end), takes
// at most 2 lines, every document number and date (NoWrap) sits on one line
// (a NoWrap value of several tokens, a report's "EX/26-27/00001 · line 1",
// breaks only between them), and no word of a name (a Wide cell, its code aside) breaks inside itself
// (GRN M3). Low-priority values below 64rem are hidden in tracks of no
// width (RR1b) and not measured. It returns the problems and how many Wide
// cells it checked.
const columnsJS = `(() => {
  const vis = (e) => e.checkVisibility({visibilityProperty: true});
  const brokenWords = (el) => {
    const out = [];
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      for (const m of n.data.matchAll(/\S+/g)) {
        const rg = document.createRange();
        rg.setStart(n, m.index);
        rg.setEnd(n, m.index + m[0].length);
        if (new Set([...rg.getClientRects()].filter((q) => q.width > 0).map((q) => Math.round(q.top))).size > 1) out.push(m[0]);
      }
    }
    return out;
  };
  const out = [];
  let wide = 0;
  const head = [...document.querySelectorAll(".datalist-head > span")].filter(vis);
  if (head.length === 0) return {problems: ["no header row"], wide};
  for (const h of head) {
    if (h.getBoundingClientRect().height > 2.5 * parseFloat(getComputedStyle(h).lineHeight)) out.push("header " + h.innerText.trim() + " takes more than 2 lines");
  }
  document.querySelectorAll("ul.datalist-rows > li a").forEach((link, row) => {
    const cells = [...link.querySelectorAll("dl")].filter((d) => vis(d) || (d.classList.contains("datalist-blank") && d.dataset.priority !== "low"));
    if (cells.length !== head.length) { out.push("row " + row + ": header has " + head.length + " labels, the row " + cells.length + " cells"); return; }
    head.forEach((h, i) => {
      const a = h.getBoundingClientRect(), c = cells[i].getBoundingClientRect();
      const off = h.classList.contains("num") ? Math.abs(a.right - c.right) : Math.abs(a.left - c.left);
      if (off > 2) out.push("row " + row + ": " + h.innerText.trim() + " is " + Math.round(off) + " px off its cells");
    });
    for (const dd of link.querySelectorAll(".datalist-nowrap dd")) {
      if (!vis(dd)) continue;
      if (/\s/.test(dd.innerText.trim())) {
        for (const w of brokenWords(dd)) out.push("row " + row + ": token " + w + " breaks inside");
      } else if (dd.getBoundingClientRect().height > parseFloat(getComputedStyle(dd).lineHeight) * 1.5) out.push("row " + row + ": " + dd.innerText.trim() + " breaks over lines");
    }
    for (const dd of link.querySelectorAll("dl.datalist-wide dd")) {
      // A master shows as "{Code} {Name}" (CD-68): the code, capitals and
      // digits with no break opportunity, may break; the name's words never.
      if (!vis(dd)) continue;
      wide++;
      for (const w of brokenWords(dd)) if (!/^[A-Z0-9/-]+$/.test(w)) out.push("row " + row + ": name word " + w + " breaks inside");
    }
  });
  return {problems: out, wide};
})()`

// columnsResult is what columnsJS returns.
type columnsResult struct {
	Problems []string `json:"problems"`
	Wide     int      `json:"wide"`
}

// R5 (GRN M3, M4, EXP claims list): at 1280 and 1440 px every module list
// and the party list draw aligned rows whose header labels sit on their
// cells in at most 2 lines, with document numbers and dates unbroken and no
// name broken inside a word, on every row; at 1024 px, where the sidebar
// leaves less than 48rem, they stay cards and an expense claim's card shows
// its claim date. No width scrolls sideways or has a small target.
func TestE2E_ListColumns1024And1280(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, desktop)
	wide := 0
	for _, p := range h.listPaths() {
		for _, w := range []int64{1280, 1440} {
			s.openAt(w, 900, p)
			at := p + " at " + strconv.FormatInt(w, 10)
			r := Eval[columnsResult](s, columnsJS)
			for _, m := range r.Problems {
				t.Errorf("%s: %s", at, m)
			}
			if w == 1280 {
				wide += r.Wide
			}
			layoutProblems(t, s, at)
		}
		s.openAt(1024, 800, p)
		layoutProblems(t, s, p+" at 1024")
	}
	if wide == 0 {
		t.Error("no Wide cell checked at 1280: the name check measured nothing")
	}
	s.openAt(1024, 800, "/doc/expense.Claim")
	date := Eval[string](s, `(() => {
  const d = [...document.querySelectorAll("ul.datalist-rows > li:first-child dl")].find((d) => d.querySelector("dt").innerText.trim() === "Claim date");
  return d && d.checkVisibility() ? d.querySelector("dd").innerText.replace(/[\s·]+$/, "").trim() : "";
})()`)
	if len(date) != len("24-10-2026") || strings.Count(date, "-") != 2 {
		t.Errorf("claim card at 1024 shows claim date %q, want dd-mm-yyyy", date)
	}
}

// R5: with JavaScript off the list still filters. The Filters disclosure
// opens natively, a tick and Show results are a plain GET that lists only
// approved orders, with the filter in the URL; a quick chip is a link that
// switches the state and is then the current one.
func TestE2E_ListFilterWithoutJS(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, desktop)
	if _, err := chromedp.Call(s.ctx, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: true}); err != nil {
		t.Fatalf("JavaScript off: %v", err)
	}
	dt, _ := doctype.Lookup(purchase.DocType)
	approved := Scalar[int](h, `SELECT COUNT(*) FROM `+dt.Table+` WHERE state = 'approved'`)
	drafts := Scalar[int](h, `SELECT COUNT(*) FROM `+dt.Table+` WHERE state = 'draft'`)
	if approved == 0 || drafts == 0 || approved > 100 {
		t.Fatalf("seeded %d approved and %d draft orders: want some of each, one page of approved", approved, drafts)
	}
	s.openAt(1280, 900, "/doc/"+purchase.DocType+"?size=100") // the size rides through the form
	s.Do("open Filters", chromedp.Click("details.filterbar-panel > summary"))
	s.Do("tick Approved", chromedp.Click(`details.filterbar-panel input[value="state:in:approved"]`))
	s.Do("Show results", chromedp.Click(`details.filterbar-panel button[type="submit"]`))
	s.Until("the filtered URL", `location.search.includes("f=state%3Ain%3Aapproved") && location.search.includes("size=100")`)
	s.Until("the filtered list", `document.querySelector("ul.datalist-rows") !== null`)
	states := Eval[[]string](s, `[...document.querySelectorAll("ul.datalist-rows > li .badge")].map((b) => b.innerText.trim())`)
	if len(states) != approved || slices.ContainsFunc(states, func(v string) bool { return v != "Approved" }) {
		t.Errorf("rows' states = %q, want %d Approved", states, approved)
	}
	if got := Eval[string](s, `document.querySelector('ul[aria-label="Quick filters"] a[aria-current="true"]').innerText.replace(/\s+/g, " ").trim()`); got != "Approved "+strconv.Itoa(approved) {
		t.Errorf("current quick chip = %q", got)
	}
	href := Eval[string](s, `[...document.querySelectorAll('ul[aria-label="Quick filters"] a')].find((a) => a.innerText.trim().startsWith("Draft")).getAttribute("href")`)
	s.Do("the Draft chip", chromedp.Click(`ul[aria-label="Quick filters"] a[href="`+href+`"]`))
	s.Until("the draft URL", `location.search.includes("f=state%3Ain%3Adraft")`)
	s.Until("the draft list", `document.querySelector("ul.datalist-rows") !== null`)
	if got := Eval[string](s, `document.querySelector('ul[aria-label="Quick filters"] a[aria-current="true"]').innerText.replace(/\s+/g, " ").trim()`); got != "Draft "+strconv.Itoa(drafts) {
		t.Errorf("current quick chip after Draft = %q", got)
	}
	if js := Eval[bool](s, `typeof htmx !== "undefined"`); js {
		t.Error("htmx ran: the page wasn't drawn without JavaScript")
	}
}
