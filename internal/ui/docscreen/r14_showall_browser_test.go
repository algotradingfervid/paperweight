//go:build e2e

package docscreen_test

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// r14Before is the line list as the browser lays it out before Show all.
type r14Before struct {
	Cards, GridRows int  // shown cards and display grid rows
	ShowAll         bool // "Show all 21 lines" is shown
	Problems        []string
}

// r14BeforeJS measures the line list: shown cards and grid rows, and
// whether Show all is shown, at least 44 px tall and inside the list.
const r14BeforeJS = `(() => {
  const shown = (el) => !!el && el.getClientRects().length > 0 && getComputedStyle(el).visibility !== "hidden";
  const list = document.querySelector(".linelist");
  if (!list) return {Problems: ["no line list"]};
  const out = {Cards: [...list.querySelectorAll("li.lines-card")].filter(shown).length,
    GridRows: [...list.querySelectorAll(".linegrid-row")].filter(shown).length, Problems: []};
  const a = [...list.querySelectorAll("a")].find((x) => x.textContent.trim() === "Show all 21 lines");
  out.ShowAll = shown(a);
  if (out.ShowAll) {
    const r = a.getBoundingClientRect(), l = list.getBoundingClientRect();
    if (r.height < 44 - 0.5) out.Problems.push("Show all is " + r.height + " px tall, want at least 44");
    if (r.left < l.left - 0.5 || r.right > l.right + 0.5) out.Problems.push("Show all (" + r.left + "-" + r.right + ") leaves the line list (" + l.left + "-" + l.right + ")");
    if (a.scrollWidth > a.clientWidth + 1) out.Problems.push("Show all's label overflows");
  }
  return out;
})()`

// r14AfterJS lists what is wrong after Show all: every card shown, focus on
// the 21st card, which is in the viewport and not under the sticky head or
// footer.
const r14AfterJS = `(() => {
  const out = [];
  const cards = [...document.querySelectorAll(".linelist li.lines-card")];
  if (cards.length !== 21) out.push(cards.length + " cards, want 21");
  const t = document.getElementById("line-reveal");
  if (!t || t !== cards[20]) return out.concat(["#line-reveal is not the 21st card"]);
  if (document.activeElement !== t) out.push("focus is on " + (document.activeElement.id || document.activeElement.tagName) + ", want the 21st card");
  const r = t.getBoundingClientRect();
  if (r.top < 0 || r.top >= innerHeight) out.push("the 21st card's top " + r.top + " is outside the viewport (" + innerHeight + ")");
  const x = r.left + r.width / 2, y = Math.min(r.top + 12, innerHeight - 1);
  const at = document.elementFromPoint(x, y);
  if (!at || !t.contains(at)) out.push("the 21st card's top is covered by " + (at ? at.tagName + "." + at.className : "nothing"));
  return out;
})()`

// R14 in the browser: on the check page and the document page of a
// 21-line document, at 320, 360, 390, 768 and 1280, the cards stop at 20
// with "Show all 21 lines" (44 px tall, inside the list); Enter on it opens
// every line with focus on the 21st, in view and clear of the sticky bars,
// with no sideways scroll, with or without the module's display grid.
func TestR14Browser_ShowAllFocusesTheFirstRevealedLine(t *testing.T) {
	ctx := uibrowser.New(t)
	csp := uibrowser.CSPProblems(ctx)
	for _, mc := range []struct {
		name string
		m    docscreen.Module
	}{
		{"cards", docscreen.Module{}},
		{"grid", m47CardModule()},
	} {
		e, id := m20Env(t, 21, mc.m)
		srv := m26Server(t, e)
		pages := []string{m01bURL(id) + "/edit/check", m01bURL(id)}
		for i, path := range pages {
			if i == 1 {
				sub, err := e.engine.Do(t.Context(), e.open, docscreentest.Hooked, id, doc.ActionSubmit, doc.ActionInput{Version: m01bRecord(t, e, id).Version})
				if err != nil || sub.State != doctype.StateApproved {
					t.Fatalf("submitting Hooked %d: state %s, err %v", id, sub.State, err)
				}
			}
			for _, width := range []int64{320, 360, 390, 768, 1280} {
				what := mc.name + " " + path + " at " + strconv.FormatInt(width, 10)
				if err := uibrowser.At(ctx, width, srv+path); err != nil {
					t.Fatal(err)
				}
				before, err := m26Step(ctx, what, chromedp.Evaluate[r14Before](r14BeforeJS))
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				for _, p := range before.Problems {
					t.Errorf("%s: %s", what, p)
				}
				r14Overflow(ctx, t, what)
				if !before.ShowAll || before.Cards != 20 || before.GridRows != 0 {
					t.Fatalf("%s: Show all shown %v, %d cards and %d grid rows shown; want Show all and 20 cards (the grid shows from a 64rem list, wider than these pages give it)", what, before.ShowAll, before.Cards, before.GridRows)
				}
				err = m26Steps(ctx, what+": Enter on Show all",
					chromedp.Evaluate[chromedp.Void](`document.querySelector(".linelist .lines-more a").focus()`),
					chromedp.KeyEvent(kb.Enter),
				)
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				if err := r14Loaded(ctx, what); err != nil {
					t.Fatal(err)
				}
				after, err := m26Step(ctx, what, chromedp.Evaluate[[]string](r14AfterJS))
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				for _, p := range after {
					t.Errorf("%s after Show all: %s", what, p)
				}
				r14Overflow(ctx, t, what+" after Show all")
			}
		}
	}
	for _, p := range csp() {
		t.Errorf("CSP: %s", p)
	}
}

// r14Loaded waits, under one step deadline, for the ?all=1 page to load:
// the poll's page context goes when the navigation starts, so it is
// polled again on the new page.
func r14Loaded(ctx context.Context, what string) error {
	deadline := time.Now().Add(uibrowser.StepTimeout)
	for {
		_, err := m26Step(ctx, what+": wait for ?all=1", chromedp.Poll[chromedp.Void](`document.readyState === "complete" && location.search === "?all=1" && location.hash === "#line-reveal" && !!document.getElementById("line-reveal")`, chromedp.WithPollingTimeout(2*time.Second)))
		if err == nil || time.Now().After(deadline) {
			return err
		}
	}
}

// r14Overflow fails t for any sideways scroll on the page.
func r14Overflow(ctx context.Context, t *testing.T, what string) {
	t.Helper()
	over, err := uibrowser.Overflow(ctx)
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
	for _, p := range over {
		t.Errorf("%s: overflow: %s", what, p)
	}
}
