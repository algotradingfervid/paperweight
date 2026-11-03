//go:build e2e

package uibrowser

import (
	"context"

	"github.com/chromedp/chromedp"
)

// docDisplayJS lists what is wrong with a document page's display on the open
// page (UI review R9): a timeline word or a figure that spans two lines, a
// connector that crosses a word or leaves its dots, steps that overlap, a
// figure past its box, a kv row whose value left the label's row, a covered
// footer button, a footer that isn't on screen, and the 80rem Actions card's
// order and look.
const docDisplayJS = `(() => {
  const out = [];
  const shown = (sel) => [...document.querySelectorAll(sel)].filter((e) => e.checkVisibility());
  const hit = (a, b) => a.left < b.right - 0.5 && b.left < a.right - 0.5 && a.top < b.bottom - 0.5 && b.top < a.bottom - 0.5;
  const text = (e) => e.textContent.replace(/\s+/g, " ").trim().slice(0, 40);
  // token picks the words to check; a word wider than el can't stay whole.
  const split = (el, token) => {
    const walk = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    for (let n = walk.nextNode(); n; n = walk.nextNode()) {
      if (n.parentElement.closest(".visually-hidden")) continue;
      for (const m of n.data.matchAll(/\S+/g)) {
        if (token && !token.test(m[0])) continue;
        const r = document.createRange();
        r.setStart(n, m.index);
        r.setEnd(n, m.index + m[0].length);
        const rects = [...r.getClientRects()].filter((c) => c.width > 0);
        const lines = new Set(rects.map((c) => Math.round(c.top)));
        if (lines.size > 1 && rects.reduce((sum, c) => sum + c.width, 0) <= el.getBoundingClientRect().width) out.push("the word " + m[0] + " spans " + lines.size + " lines");
      }
    }
  };
  for (const e of shown(".lifecycle__label, .lifecycle__note, .lifecycle__date, .doc-head__amount, .summary dd.num, .doc-head__facts dd.num, .lines-amount, .lines-total, .linegrid-amount, .linegrid-total")) split(e);
  // Dates and document numbers: a hyphen or slash between digits.
  for (const e of shown("h1, .summary dd:not(.code), .doc-head__facts dd:not(.code), .lines-meta, #main p:not(.banner *)")) split(e, /\d[-\/]\d/);
  const whole = shown(".summary dd .whole")[0];
  if (whole) {
    const dd = whole.closest("dd"), sel = getSelection();
    sel.selectAllChildren(dd);
    if (sel.toString().trim() !== dd.textContent.trim()) out.push("copying " + text(dd) + " gives " + sel.toString());
    sel.removeAllRanges();
  }

  for (const list of shown(".lifecycle")) {
    const steps = [...list.children];
    const words = [...list.querySelectorAll(".lifecycle__label, .lifecycle__note, .lifecycle__date")].map((e) => e.getBoundingClientRect());
    const dots = steps.map((s) => s.querySelector(".lifecycle__dot").getBoundingClientRect());
    steps.forEach((s, i) => {
      const box = s.getBoundingClientRect();
      if (i > 0 && hit(box, steps[i - 1].getBoundingClientRect())) out.push("steps " + (i - 1) + " and " + i + " overlap");
      for (const w of s.querySelectorAll(".lifecycle__label, .lifecycle__note, .lifecycle__date")) {
        const r = w.getBoundingClientRect();
        if (r.left < box.left - 0.5 || r.right > box.right + 0.5) out.push(text(w) + " leaves its step");
      }
      if (i === 0) return;
      const c = getComputedStyle(s, "::before");
      const line = {left: box.left + parseFloat(c.left), top: box.top + parseFloat(c.top), width: parseFloat(c.width), height: parseFloat(c.height)};
      line.right = line.left + line.width;
      line.bottom = line.top + line.height;
      if (words.some((w) => hit(line, w))) out.push("the connector into step " + i + " crosses a word");
      const gap = Math.max(line.left - dots[i - 1].right, line.top - dots[i - 1].bottom, dots[i].left - line.right, dots[i].top - line.bottom);
      if (gap > 8 + 0.5) out.push("the connector into step " + i + " stops " + gap + " px from its dots");
    });
  }

  for (const e of shown(".doc-head__amount")) {
    const r = document.createRange();
    r.selectNodeContents(e);
    if (r.getBoundingClientRect().right > e.closest(".doc-head").getBoundingClientRect().right + 0.5) out.push("the amount " + text(e) + " is wider than the header");
  }
  for (const row of shown(".summary-row")) {
    const dt = row.querySelector("dt").getBoundingClientRect(), dd = row.querySelector("dd").getBoundingClientRect(), r = row.getBoundingClientRect();
    const words = document.createRange();
    words.selectNodeContents(row.querySelector("dd"));
    const own = dd.top >= dt.bottom - 0.5;
    if (own && words.getBoundingClientRect().width <= 0.6 * r.width - 16) out.push(text(row) + ": the value left the label's row though it fits");
    if (dd.right > r.right + 0.5) out.push(text(row) + ": the value is wider than its row");
  }
  for (const list of shown(".summary")) {
    const left = new Set([...list.querySelectorAll(":scope > .summary-row")].filter((r) => {
      return r.querySelector("dd").getBoundingClientRect().top < r.querySelector("dt").getBoundingClientRect().bottom - 0.5;
    }).map((r) => Math.round(r.querySelector("dd").getBoundingClientRect().left)));
    if (document.documentElement.clientWidth >= 360 && left.size > 1) out.push("the values of one list start at " + [...left].join(", "));
  }

  const wide = innerWidth >= 1280;
  for (const bar of shown(".doc-actions .actionbar")) {
    if (!wide) {
      const r = bar.getBoundingClientRect();
      if (r.bottom > innerHeight + 0.5 || r.top < 0) out.push("the footer is off screen: " + r.top + " to " + r.bottom);
      for (const b of bar.querySelectorAll(":scope > .btn")) {
        const c = b.getBoundingClientRect(), at = document.elementFromPoint(c.left + c.width / 2, c.top + c.height / 2);
        if (!at || !b.contains(at)) out.push(text(b) + " is covered by " + (at ? at.className || at.tagName : "nothing"));
      }
    }
  }
  if (wide) {
    if (shown(".doc-actions .actionbar > [data-open-dialog]").length) out.push("the ellipsis shows beside the Actions card");
    const last = shown(".doc-actions .actionbar > .btn").pop(), more = shown(".doc-actions .more-actions")[0];
    if (last && more && last.getBoundingClientRect().bottom > more.getBoundingClientRect().top + 0.5) out.push("More actions sit above the bar's buttons");
    for (const b of shown(".doc-actions .actionbar")) {
      if (getComputedStyle(b).boxShadow !== "none" && getComputedStyle(b).display !== "contents") out.push("the Actions card draws a line");
    }
  } else if (shown(".more-actions").length && document.querySelector(".doc-actions")) out.push("More actions show on the page below 80rem");
  for (const e of shown(".doc-actions .btn-error, #doc-more .btn-error")) out.push(text(e) + " is a filled danger button");
  return out;
})()`

// DocDisplay returns the display problems on the open document page.
func DocDisplay(ctx context.Context) ([]string, error) {
	return withDeadline(ctx, StepTimeout, "document display", func(ctx context.Context) ([]string, error) {
		return chromedp.Run(ctx, chromedp.Evaluate[[]string](docDisplayJS))
	})
}
