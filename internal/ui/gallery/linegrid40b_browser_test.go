//go:build e2e

package gallery

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-40b (CD-313, CD-308 (3); purchase UX §5.6): the display line grid,
// read-only PO-style rows (# / Item / Quantity / Unit / Rate / Tax / Amount
// and Change, Remove, Move up, Move down), has the editable grid's content
// minimums and no-overflow rules from 64rem.

// m40bPath serves the display sample on the gallery's own router.
const m40bPath = "/_gallery/m40b-display-grid"

// Sample values: the largest a purchase line holds (CD-169).
const (
	m40bQty  = "1,00,000"
	m40bUnit = "kg"
	m40bTax  = "GST 18%"
	m40bNote = "Last bought at ₹345.0000 from Shree Steel Traders on 12-10-2026 (PO/26-27/00031)"
)

// m40bDisplaySample is three display rows; line 2, with both moves and the
// note, holds the largest quantity, rate and amount, a 100-character item
// name and a 60-character code with no break opportunity as its Sub.
func m40bDisplaySample() components.LineListProps {
	var rows []components.LineGridRow
	var cards []components.LineCard
	for no := 1; no <= 3; no++ {
		base := "/_gallery/lines?line=" + strconv.Itoa(no)
		r := components.LineGridRow{
			No: no, Amount: types.FormatINR(types.MaxMoney), ChangeURL: base, RemoveURL: base, Version: "1",
			Cells: []components.GridCell{
				{Label: "Item", Text: uitest.LongName100, Sub: uitest.LongCode60},
				{Label: "Quantity", Text: m40bQty, Kind: "qty"},
				{Label: "Unit", Text: m40bUnit},
				{Label: "Rate", Text: largestRate, Kind: "rate"},
				{Label: "Tax", Text: m40bTax},
			},
		}
		if no > 1 {
			r.MoveUpURL = base
		}
		if no < 3 {
			r.MoveDownURL = base
		}
		if no == 2 {
			r.Note = m40bNote
		}
		rows = append(rows, r)
		cards = append(cards, components.LineCard{No: no, Item: uitest.LongName100, Code: uitest.LongCode60, QtyRate: m40bQty + " kg × " + largestRate, Amount: r.Amount, Tax: m40bTax})
	}
	return components.LineListProps{Cards: cards, Grid: rows, Total: 3, Display: true, TaxableTotal: uitest.MaxMoney}
}

// m40bHandler is the gallery with the display sample at m40bPath.
func m40bHandler(t *testing.T) http.Handler {
	t.Helper()
	h := Handler()
	r, ok := h.(chi.Router)
	if !ok {
		t.Fatalf("gallery Handler is %T, want a chi.Router", h)
	}
	r.Get(m40bPath, func(w http.ResponseWriter, req *http.Request) {
		render(w, req, http.StatusOK, titled("Display line grid"), components.LineList(m40bDisplaySample()))
	})
	return h
}

// m40bDisplayJS checks line 2 of the display grid (fmt args: the note, the
// code, the item name, the quantity, the unit, the rate, the tax, the
// amount, all JSON): the grid shows and the cards don't; nothing in the row
// or its note overflows; #, Quantity, Unit, Rate, Tax and Amount each keep
// to one line; the row's cells are the head's columns, the numbers right-
// aligned with their head labels and the texts left-aligned; Sub is a
// second line under the item name in a muted colour; the four actions fit
// their cell, keep to one line and don't overlap; the note is a full-width
// row under the line.
const m40bDisplayJS = `((note, code, item, qty, unit, rate, tax, amount) => {
  const out = [], ed = document.querySelector(".line-editor"), grid = ed && ed.querySelector(".linegrid");
  if (!grid || !grid.checkVisibility()) return ["the display grid is not shown"];
  const cards = ed.closest(".linelist")?.querySelector(".lines");
  if (cards && cards.checkVisibility()) out.push("the line cards show beside the grid");
  const rows = [...grid.querySelectorAll(".linegrid-row")], head = grid.querySelector(".linegrid-head");
  if (rows.length !== 3 || !head) return [rows.length + " rows and head " + !!head + ", want 3 rows and a head"];
  const row = rows[1];
  const norm = (s) => s.replace(/\s+/g, " ").trim();
  const textNode = (root, text) => { const w = document.createTreeWalker(root, NodeFilter.SHOW_TEXT); for (let n = w.nextNode(); n; n = w.nextNode()) if (norm(n.data) === text) return n; return null; };
  const range = (n) => { const r = document.createRange(); r.selectNodeContents(n); return r; };
  const box = (n) => range(n).getBoundingClientRect();
  const lineCount = (n) => new Set([...range(n).getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top))).size;
  const name = (el) => el.tagName.toLowerCase() + (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  const items = (el) => [...el.children].flatMap((c) => { const cs = getComputedStyle(c); if (cs.display === "contents") return items(c); return cs.display === "none" || cs.position === "absolute" ? [] : [c]; });
  const noteText = textNode(grid, note);
  if (!noteText) out.push("line 2's note is not in the grid");
  // Nothing overflows in the row or its note.
  const noteItem = (() => { if (!noteText) return null; let e = noteText.parentElement; while (e.parentElement && !getComputedStyle(e.parentElement).display.includes("grid")) e = e.parentElement; return e; })();
  for (const root of [row, noteItem].filter(Boolean)) for (const el of [root, ...root.querySelectorAll("*")]) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents" || el instanceof SVGElement || el.clientWidth === 0) continue;
    if (el.scrollWidth > el.clientWidth + 1 && cs.textOverflow !== "ellipsis") out.push("content wider than box: " + name(el) + " " + el.scrollWidth + " > " + el.clientWidth);
  }
  if (row.getBoundingClientRect().width > grid.clientWidth + 0.5) out.push("row " + row.getBoundingClientRect().width + " wider than the grid " + grid.clientWidth);
  if (grid.scrollWidth > grid.clientWidth + 1) out.push("the grid scrolls: " + grid.scrollWidth + " > " + grid.clientWidth);
  if (document.documentElement.scrollWidth > document.documentElement.clientWidth) out.push("the page scrolls sideways");
  // The values: each found, and the short ones on one line.
  const found = {};
  for (const [k, v] of Object.entries({ "#": "2", item, code, qty, unit, rate, tax, amount })) {
    found[k] = textNode(row, v);
    if (!found[k]) { out.push("line 2 has no text " + JSON.stringify(v)); continue; }
    if (!["item", "code"].includes(k) && lineCount(found[k]) !== 1) out.push(k + " " + JSON.stringify(v) + " takes " + lineCount(found[k]) + " lines, want 1");
  }
  // The row's columns are the head's.
  const headCells = items(head), cells = items(row).filter((c) => !noteText || !c.contains(noteText));
  const tracks = getComputedStyle(grid).gridTemplateColumns.split(" ").length;
  if (headCells.length !== tracks || cells.length !== tracks) out.push("head " + headCells.length + " and row " + cells.length + " cells for " + tracks + " columns");
  const lefts = (els) => JSON.stringify(els.map((e) => Math.round(e.getBoundingClientRect().left)));
  if (lefts(cells) !== lefts(headCells)) out.push("columns differ from the head's: " + lefts(cells) + " vs " + lefts(headCells));
  // Numbers sit right-aligned under their right-aligned labels; texts left.
  const labels = headCells.map((c) => norm(c.textContent));
  for (const [k, label, side] of [["#", "#", "left"], ["item", "Item", "left"], ["qty", "Quantity", "right"], ["unit", "Unit", "left"], ["rate", "Rate", "right"], ["tax", "Tax", "left"], ["amount", "Amount", "right"]]) {
    const i = labels.indexOf(label), h = i < 0 ? null : textNode(headCells[i], label);
    if (!h || !found[k]) { out.push("no head label " + label + " or its value"); continue; }
    const hb = box(h), vb = box(found[k]), cell = cells[i]?.getBoundingClientRect();
    if (Math.abs(hb[side] - vb[side]) > 1.5) out.push(label + ": the value's " + side + " edge " + Math.round(vb[side]) + " is not the head label's " + Math.round(hb[side]));
    if (cell && side === "right" && Math.abs(vb.right - cell.right) > 1.5 + parseFloat(getComputedStyle(cells[i]).paddingRight)) out.push(label + " is not right-aligned in its cell: " + Math.round(vb.right) + " vs " + Math.round(cell.right));
  }
  // Sub is a muted second line under the item name.
  if (found.item && found.code) {
    if (box(found.code).top < box(found.item).bottom - 1) out.push("the code is not under the item name");
    if (getComputedStyle(found.code.parentElement).color === getComputedStyle(found.item.parentElement).color) out.push("the code is not muted: same colour as the item name");
  }
  // The actions fit their cell, on one line each, without overlapping.
  const actions = [...row.querySelectorAll("a, button")];
  const want = ["Change line 2", "Remove line 2", "Move up line 2", "Move down line 2"];
  const got = actions.map((a) => a.getAttribute("aria-label"));
  if (JSON.stringify(got) !== JSON.stringify(want)) out.push("actions " + JSON.stringify(got) + ", want " + JSON.stringify(want));
  const actCell = cells[cells.length - 1]?.getBoundingClientRect();
  const rects = actions.map((a) => a.getBoundingClientRect());
  actions.forEach((a, i) => {
    const r = rects[i], label = textNode(a, norm(a.textContent));
    if (label && lineCount(label) !== 1) out.push("action wraps: " + JSON.stringify(norm(a.textContent)));
    if (actCell && (r.left < actCell.left - 0.5 || r.right > actCell.right + 0.5)) out.push("action " + JSON.stringify(norm(a.textContent)) + " leaves its cell: " + Math.round(r.left) + "–" + Math.round(r.right) + " in " + Math.round(actCell.left) + "–" + Math.round(actCell.right));
    if (r.right > grid.getBoundingClientRect().right + 0.5) out.push("action " + JSON.stringify(norm(a.textContent)) + " ends past the grid");
    for (let j = 0; j < i; j++) { const o = rects[j]; if (r.left < o.right - 0.5 && o.left < r.right - 0.5 && r.top < o.bottom - 0.5 && o.top < r.bottom - 0.5) out.push("actions overlap: " + got[j] + " and " + got[i]); }
  });
  // The note is a full-width row under the line.
  if (noteItem) {
    const nb = noteItem.getBoundingClientRect(), first = headCells[0].getBoundingClientRect(), last = headCells[headCells.length - 1].getBoundingClientRect();
    if (Math.abs(nb.left - first.left) > 1 || Math.abs(nb.right - last.right) > 1) out.push("the note spans " + Math.round(nb.left) + "–" + Math.round(nb.right) + ", want the full grid " + Math.round(first.left) + "–" + Math.round(last.right));
    const bottom = Math.max(...cells.map((c) => c.getBoundingClientRect().bottom));
    if (nb.top < bottom - 0.5) out.push("the note starts at " + Math.round(nb.top) + ", above the line's cells' bottom " + Math.round(bottom));
  }
  return out;
})(%s, %s, %s, %s, %s, %s, %s, %s)`

// TestGalleryBrowser_DisplayLineGrid (M0-40b, CD-313): with the line editor
// exactly 64rem wide and at 1440, the display grid's widest row (1,00,000
// kg, ₹10,00,00,000.0000, ₹10,00,00,00,000.00, a 100-character item and a
// 60-character code, four actions and a note) fits: no overflow, one line
// per value, columns and alignment of the head, the actions in their cell
// and the note across the grid. No page overflow and no small targets.
func TestGalleryBrowser_DisplayLineGrid(t *testing.T) {
	s := httptest.NewServer(m40bHandler(t))
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	const wide = 1440
	if err := uibrowser.At(ctx, wide, s.URL+m40bPath); err != nil {
		t.Fatal(err)
	}
	editor := gridEval[int64](t, ctx, "the line editor's width", `Math.round(document.querySelector(".line-editor").getBoundingClientRect().width)`)
	at64 := wide - (editor - gridFrom)
	q := func(s string) string { return strconv.Quote(s) }
	js := fmt.Sprintf(m40bDisplayJS, q(m40bNote), q(uitest.LongCode60), q(uitest.LongName100), q(m40bQty), q(m40bUnit), q(largestRate), q(m40bTax), q(types.FormatINR(types.MaxMoney)))
	for _, w := range []int64{at64, wide} {
		if err := uibrowser.At(ctx, w, s.URL+m40bPath); err != nil {
			t.Fatal(err)
		}
		if got := gridEval[int64](t, ctx, "the line editor's width", `Math.round(document.querySelector(".line-editor").getBoundingClientRect().width)`); w == at64 && got != gridFrom {
			t.Errorf("%d: the line editor is %d px wide, want %d", w, got, gridFrom)
		}
		for _, p := range gridEval[[]string](t, ctx, "the display grid's widest row", js) {
			t.Errorf("%d: %s", w, p)
		}
		probs, err := uibrowser.Overflow(ctx)
		if err != nil {
			t.Fatalf("%d: overflow: %v", w, err)
		}
		for _, p := range probs {
			t.Errorf("%d: overflow: %s", w, p)
		}
		small, err := uibrowser.SmallTargets(ctx)
		if err != nil {
			t.Fatalf("%d: target size: %v", w, err)
		}
		for _, p := range small {
			t.Errorf("%d: small target: %s", w, p)
		}
	}
}
