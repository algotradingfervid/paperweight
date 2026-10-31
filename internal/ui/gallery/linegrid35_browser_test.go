//go:build e2e

package gallery

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	// gridFrom is the line editor's width from which the grid shows (64rem).
	gridFrom      = 1024
	editorWidthJS = `Math.round(document.getElementById("line-editor").getBoundingClientRect().width)`
)

// gridEval is js's value on the page, under one step deadline.
func gridEval[T any](t *testing.T, ctx context.Context, what, js string) T {
	t.Helper()
	sctx, cancel := context.WithTimeoutCause(ctx, uibrowser.StepTimeout, fmt.Errorf("%s: step deadline %v exceeded: %w", what, uibrowser.StepTimeout, context.DeadlineExceeded))
	defer cancel()
	v, err := chromedp.Run(sctx, chromedp.Evaluate[T](js))
	if err != nil {
		t.Fatalf("%s: %v (%v)", what, err, context.Cause(sctx))
	}
	return v
}

// widestGridRowJS checks the lines page's line 2, the five-cell row with the
// largest rate and line amount and both move buttons: what doesn't fit in
// it, and the head's and its columns, which must line up one for one with
// the grid's tracks.
const widestGridRowJS = `(() => {
  const out = [], row = document.getElementById("grid-row-2").closest(".linegrid-row");
  const grid = row.parentElement, head = grid.querySelector(".linegrid-head");
  const name = (el) => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") + (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  for (const el of [row, ...row.querySelectorAll("*")]) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents" || el instanceof SVGElement || el.clientWidth === 0) continue;
    if (el.scrollWidth > el.clientWidth + 1 && cs.textOverflow !== "ellipsis") out.push("content wider than box: " + name(el) + " " + el.scrollWidth + " > " + el.clientWidth);
  }
  const rate = document.getElementById("grid-2-rate");
  if (rate.value !== "10,00,00,000.0000") out.push("rate holds " + JSON.stringify(rate.value));
  const amount = row.querySelector(".linegrid-amount");
  if (!amount.textContent.includes("₹10,00,00,00,000.00")) out.push("amount is " + JSON.stringify(amount.textContent));
  const lines = (el) => { const r = document.createRange(); r.selectNodeContents(el); return r.getClientRects().length; };
  if (lines(amount.lastChild) !== 1) out.push("the amount wraps");
  const buttons = [...row.querySelectorAll(".linegrid-actions .btn")];
  if (buttons.length !== 3) out.push(buttons.length + " actions, want Remove, Move up and Move down");
  for (const b of buttons) if (lines(b.querySelector("span")) !== 1) out.push("action wraps: " + JSON.stringify(b.textContent.trim()));
  const tracks = getComputedStyle(grid).gridTemplateColumns.split(" ").length;
  const shown = (els) => els.filter((e) => getComputedStyle(e).display !== "none");
  const headCells = shown([...head.children]), cells = shown([...row.querySelector(".linegrid-form").children, row.querySelector(".linegrid-actions")]);
  if (headCells.length !== tracks || cells.length !== tracks) out.push("head " + headCells.length + " and row " + cells.length + " cells for " + tracks + " columns");
  const lefts = (els) => JSON.stringify(els.map((e) => Math.round(e.getBoundingClientRect().left)));
  if (lefts(cells) !== lefts(headCells)) out.push("columns differ from the head's: " + lefts(cells) + " vs " + lefts(headCells));
  if (row.getBoundingClientRect().width > grid.clientWidth + 0.5) out.push("row " + row.getBoundingClientRect().width + " wider than the grid " + grid.clientWidth);
  return out;
})()`

// TestGalleryBrowser_LineGridWidestRow (M0-35): with the line editor exactly
// 64rem wide and at 1440, a purchase-style row (Item, Quantity, Unit, Rate,
// Tax) holding ₹10,00,00,000.0000 and ₹10,00,00,00,000.00, with Remove,
// Move up and Move down, fits: nothing in it overflows, the amount and the
// actions keep to one line, and its columns are the head's.
func TestGalleryBrowser_LineGridWidestRow(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	const wide = 1440
	if err := uibrowser.At(ctx, wide, s.URL+"/_gallery/lines"); err != nil {
		t.Fatal(err)
	}
	editor := gridEval[int64](t, ctx, "the line editor's width", editorWidthJS)
	at64 := wide - (editor - gridFrom)
	for _, w := range []int64{at64, wide} {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/lines"); err != nil {
			t.Fatal(err)
		}
		if got := gridEval[int64](t, ctx, "the line editor's width", editorWidthJS); w == at64 && got != gridFrom {
			t.Errorf("%d: the line editor is %d px wide, want %d", w, got, gridFrom)
		}
		if !gridEval[bool](t, ctx, "the grid shown", `document.getElementById("grid-2-rate")?.checkVisibility() === true`) {
			t.Fatalf("%d: line 2's grid row is not shown", w)
		}
		for _, p := range gridEval[[]string](t, ctx, "the widest row", widestGridRowJS) {
			t.Errorf("%d: %s", w, p)
		}
	}
}
