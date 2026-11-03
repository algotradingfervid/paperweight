//go:build e2e

package e2e

import (
	"slices"
	"testing"
)

// rowsJS measures every list drawn as aligned rows (its header shown): the
// header is no taller than its tallest shown label, each label takes at most
// 2 lines and breaks no word inside itself; each row is no taller than its
// tallest shown cell, and a short value (24 characters or fewer) takes at
// most 3 lines. A low-priority value hidden below 64rem takes no height
// (RR1c). It returns the problems, the rows measured and the hidden
// low-priority cells seen.
const rowsJS = `(() => {
  const vis = (e) => e.checkVisibility({visibilityProperty: true});
  const lh = (e) => parseFloat(getComputedStyle(e).lineHeight);
  const pad = (e) => { const s = getComputedStyle(e); return parseFloat(s.paddingTop) + parseFloat(s.paddingBottom); };
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
  let rows = 0, hidden = 0;
  for (const list of document.querySelectorAll(".datalist")) {
    const head = list.querySelector(":scope > .datalist-head");
    if (!head || getComputedStyle(head).display === "none") continue;
    const labels = [...head.children].filter(vis);
    const tallest = Math.max(0, ...labels.map((l) => l.getBoundingClientRect().height));
    const hh = head.getBoundingClientRect().height;
    if (hh > tallest + pad(head) + 2) out.push("header is " + Math.round(hh) + " px, its tallest label " + Math.round(tallest) + " px");
    for (const l of labels) {
      const t = l.innerText.trim();
      if (l.getBoundingClientRect().height > 2.5 * lh(l)) out.push("header " + t + " takes more than 2 lines");
      for (const w of brokenWords(l)) out.push("header " + t + ": word " + w + " breaks inside");
    }
    list.querySelectorAll("ul.datalist-rows > li > a.datalist-link").forEach((a, row) => {
      rows++;
      const cells = [...a.querySelectorAll("dl, p.datalist-cell")];
      hidden += cells.filter((c) => c.dataset.priority === "low" && !vis(c)).length;
      const shown = cells.filter(vis);
      const top = a.getBoundingClientRect().top + parseFloat(getComputedStyle(a).paddingTop);
      const bottom = Math.max(top, ...shown.map((c) => c.getBoundingClientRect().bottom));
      const h = a.getBoundingClientRect().height, want = Math.max(bottom - a.getBoundingClientRect().top + parseFloat(getComputedStyle(a).paddingBottom), parseFloat(getComputedStyle(a).minHeight) || 0);
      if (h > want + 2) out.push("row " + row + " is " + Math.round(h) + " px, its cells need " + Math.round(want) + " px");
      for (const c of shown) {
        const t = c.innerText.trim();
        if (t.length <= 24 && c.getBoundingClientRect().height > 3.5 * lh(c)) out.push("row " + row + ": " + t + " takes more than 3 lines");
      }
    });
  }
  return {problems: out, rows, hidden};
})()`

// rowsResult is what rowsJS returns.
type rowsResult struct {
	Problems []string `json:"problems"`
	Rows     int      `json:"rows"`
	Hidden   int      `json:"hidden"`
}

// RR1c 1: on every report, module list and the party list, at 1016 px (no
// sidebar, the list just under 64rem), 1024, 1280 and 1320 px (with the
// sidebar, under 64rem) and 1440 px, aligned rows and their header are as
// tall as their shown content: a low-priority value hidden in a track of no
// width takes no height, no header label breaks inside a word or takes more
// than 2 lines, and no short value takes more than 3 lines.
func TestE2E_ListRowsFitTheirContent(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, desktop)
	paths := slices.Concat(h.reportPaths(), h.listPaths())
	rows, hidden := 0, 0
	for _, p := range paths {
		for _, w := range []int64{1016, 1024, 1280, 1320, 1440} {
			s.openAt(w, 900, p)
			r := Eval[rowsResult](s, rowsJS)
			for _, m := range r.Problems {
				t.Errorf("%s at %d: %s", p, w, m)
			}
			rows += r.Rows
			hidden += r.Hidden
		}
	}
	if rows == 0 {
		t.Error("no aligned row measured: the check saw nothing")
	}
	if hidden == 0 {
		t.Error("no hidden low-priority value seen below 64rem: the case the check is for never came up")
	}
}
