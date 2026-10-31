//go:build e2e

package e2e

import (
	"context"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const (
	// largestLine is the largest line amount the app accepts (types.MaxMoney),
	// and largestShown is how the pages show it.
	largestLine  = "10000000000.00"
	largestShown = "₹10,00,00,00,000.00"
	largestRate  = "100000000.0000"

	// gridFrom is the line editor's width from which the grid shows (64rem).
	gridFrom = 1024
	desk1280 = 1280

	editorWidth = `Math.round(document.getElementById("line-editor").getBoundingClientRect().width)`
)

// lineFitJS is what doesn't fit on the page, and how many of each thing it
// checked, so a check that found nothing to check fails (lessons §10). A
// problem is the document scrolling sideways, a box whose content is wider
// than it (an ellipsis is fine), a shown grid amount or grid money input
// or number input that doesn't show its value whole (a rate input is
// checked like a money input), a grid amount, line
// card amount or grid action whose text wraps, a grid row or head with more
// or fewer cells than the grid has columns (it wraps onto a second line of
// columns), or a row whose columns don't line up with the head's.
const lineFitJS = `(() => {
  const de = document.documentElement, out = [];
  const n = {gridAmounts: 0, moneyInputs: 0, rateInputs: 0, numberInputs: 0, gridWraps: 0, cardAmounts: 0, actions: 0, heads: 0, rows: 0};
  if (de.scrollWidth > de.clientWidth) out.push("document scrollWidth " + de.scrollWidth + " > " + de.clientWidth);
  const name = (el) => el.tagName.toLowerCase() + (el.id ? "#" + el.id : "") +
    (typeof el.className === "string" && el.className.trim() ? "." + el.className.trim().split(/\s+/).join(".") : "");
  for (const el of document.querySelectorAll("body *")) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents" || el instanceof SVGElement) continue;
    if (el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0 && cs.textOverflow !== "ellipsis")
      out.push("content wider than box: " + name(el) + " " + el.scrollWidth + " > " + el.clientWidth + " overflow-x:" + cs.overflowX);
  }
  // oneLine reports whether el's last text is on one line.
  const oneLine = (el) => {
    const w = document.createTreeWalker(el, NodeFilter.SHOW_TEXT, (t) => t.textContent.trim() && t.parentElement.checkVisibility() && !t.parentElement.closest(".visually-hidden") ? NodeFilter.FILTER_ACCEPT : NodeFilter.FILTER_SKIP);
    let text = null;
    for (let t = w.nextNode(); t; t = w.nextNode()) text = t;
    if (!text) return null;
    const r = document.createRange();
    r.selectNodeContents(text);
    return r.getClientRects().length === 1;
  };
  const wraps = (el, what, key) => {
    const one = oneLine(el);
    if (one === null) return;
    n[key]++;
    if (!one) out.push(what + " wraps: " + name(el) + " " + JSON.stringify(el.textContent.trim()));
  };
  for (const el of document.querySelectorAll(".linegrid-amount, .linegrid input.num")) {
    if (!el.checkVisibility()) continue;
    if (el.tagName !== "INPUT") n.gridAmounts++;
    else if (el.matches("[hx-post='/ui/validate/money']")) n.moneyInputs++;
    else if (el.matches("[hx-post='/ui/validate/rate']")) n.rateInputs++;
    else n.numberInputs++;
    if (el.scrollWidth > el.clientWidth) out.push("value clipped: " + name(el) + " " + el.scrollWidth + " > " + el.clientWidth);
    if (el.tagName !== "INPUT") wraps(el, "amount", "gridWraps");
  }
  for (const el of document.querySelectorAll(".lines-amount")) if (el.checkVisibility()) wraps(el, "line card amount", "cardAmounts");
  for (const el of document.querySelectorAll(".linegrid-actions .btn")) if (el.checkVisibility()) wraps(el, "action", "actions");
  // The head and each row fill the grid's columns once, and each head label
  // starts where its column's cells do, in every row.
  for (const head of document.querySelectorAll(".linegrid-head")) {
    if (!head.checkVisibility()) continue;
    n.heads++;
    const tracks = getComputedStyle(head.parentElement).gridTemplateColumns.split(" ").length;
    const shown = (els) => els.filter((e) => getComputedStyle(e).display !== "none");
    const lefts = (els) => els.map((e) => Math.round(e.getBoundingClientRect().left));
    const headCells = shown([...head.children]);
    if (headCells.length !== tracks) out.push("the head has " + headCells.length + " cells for " + tracks + " columns");
    const want = lefts(headCells);
    for (const row of head.parentElement.querySelectorAll(".linegrid-row")) {
      n.rows++;
      const cells = shown([...row.querySelector(".linegrid-form").children, row.querySelector(".linegrid-actions")]);
      if (cells.length !== tracks) out.push(name(row) + " has " + cells.length + " cells for " + tracks + " columns");
      const got = lefts(cells);
      if (JSON.stringify(got) !== JSON.stringify(want)) out.push("columns differ from the head's: " + JSON.stringify(got) + " vs " + JSON.stringify(want));
    }
  }
  return {problems: out, counts: n};
})()`

// lineFit is lineFitJS's answer.
type lineFit struct {
	Problems []string `json:"problems"`
	Counts   struct {
		GridAmounts  int `json:"gridAmounts"`
		MoneyInputs  int `json:"moneyInputs"`
		RateInputs   int `json:"rateInputs"`
		NumberInputs int `json:"numberInputs"`
		GridWraps    int `json:"gridWraps"`
		CardAmounts  int `json:"cardAmounts"`
		Actions      int `json:"actions"`
		Heads        int `json:"heads"`
		Rows         int `json:"rows"`
	} `json:"counts"`
}

// fitCase is one draft the test opens: its Items step, check step and
// document page. actions is how many grid buttons its rows have; money is
// whether its lines have a money input and a shown amount; rate whether
// they have a rate input and a quantity input.
type fitCase struct {
	name, dt, user, shown string
	id                    int64
	actions               int
	money, rate           bool
}

// TestE2E_LineGridFitsLargestAmount is M0-35: the grid's amount column and
// money inputs show ₹10,00,00,00,000.00 whole on one line at desktop widths
// with the grid, from the 64rem breakpoint up, and its actions keep their
// labels on one line; no page with such a line scrolls sideways at any sweep
// width. fixture.Claim has the three-cell layout. fixture.Quote is the
// five-cell one with a money input (M0-40c; expense.Advance had it until it
// showed cards, CD-315): Item, Quantity, Rate, Amount, Description, its
// amount input in the fifth column beside the rate and quantity inputs, in
// one line at the largest amount and in three for Move up and Move down.
// fixture.Order is the five-cell layout of a purchase line
// (Item, Quantity, Rate, Unit, Description; no amount), with the largest
// rate, ₹10,00,00,000.0000, whose number cells and actions must fit at
// 64rem too.
func TestE2E_LineGridFitsLargestAmount(t *testing.T) {
	h := Start(t)
	ctx := context.Background()
	if err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE fixture_item SET name = ? WHERE id = ?`, uitest.LongName100, h.M.Item["MS-ANGLE-50"])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	// The largest rate (types.MaxRate), on a quantity that keeps the line
	// within the largest line amount.
	orderLine := map[string]string{"item_id": strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10), "qty": "100.000", "rate": largestRate, "uom": "kg", "description": uitest.LongName100}
	order := h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
			"order_date": plainDate, "amount": largestLine,
		},
		Lines: []map[string]string{orderLine, orderLine, orderLine},
	})
	emp := strconv.FormatInt(h.employee(deepak), 10)
	claim := h.Do(deepak, fixture.Claim, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": emp, "claim_date": plainDate},
		Lines:  []map[string]string{{"expense_type": uitest.LongName100, "description": uitest.LongName100, "amount": largestLine}},
	})
	quoteLine := map[string]string{"item_id": orderLine["item_id"], "qty": "1.000", "rate": largestRate, "amount": largestLine, "description": uitest.LongName100}
	quote := func(lines ...map[string]string) int64 {
		r := h.Do(priya, fixture.Quote, 0, doctype.ActionSave, doc.ActionInput{
			Values: map[string]string{"quote_date": plainDate, "remarks": uitest.LongName100},
			Lines:  lines,
		})
		if r.ID == 0 || len(r.Problems) != 0 {
			t.Fatalf("quote draft: %+v", r)
		}
		return r.ID
	}
	cases := []fitCase{
		{"claim", fixture.Claim, deepak, largestShown, claim.ID, 1, true, false},
		{"quote", fixture.Quote, priya, largestShown, quote(quoteLine), 1, true, true},
		// Three lines at the largest amount total 3 × types.MaxMoney on purpose:
		// a draft is never submitted here, only measured for width.
		{"quote, three lines", fixture.Quote, priya, largestShown, quote(quoteLine, quoteLine, quoteLine), 7, true, true},
		{"order", fixture.Order, priya, uitest.LongName100, order.ID, 7, false, true},
	}
	for _, c := range cases {
		path := DocPath(c.dt, c.id)
		items := path + "/edit/items"
		s := h.SignIn(c.user, desk1280)
		s.Open(items)
		// The viewport at which the line editor is exactly 64rem wide: at
		// 1280 it is narrower, so the cards show there.
		at64 := desk1280 - (Eval[int64](s, editorWidth) - gridFrom)
		desks := []int64{desk1280, at64, desktop}
		widths := slices.Compact(slices.Sorted(slices.Values(slices.Concat(screens.Widths, desks))))
		for _, page := range []string{items, path + "/edit/check", path} {
			for _, w := range widths {
				s.width = w
				s.Open(page)
				s.WaitText(c.shown)
				grid := page == items && w >= at64
				if grid {
					s.Until("the grid", `document.querySelector(".linegrid-row")?.checkVisibility() === true`)
					if got := Eval[int64](s, editorWidth); w == at64 && got != gridFrom {
						t.Errorf("%s %s at %d: the line editor is %d px wide, want %d", c.name, page, w, got, gridFrom)
					}
				}
				fit := Eval[lineFit](s, lineFitJS)
				for _, p := range fit.Problems {
					t.Errorf("%s %s at %d: %s", c.name, page, w, p)
				}
				if !slices.Contains(desks, w) {
					continue
				}
				n := fit.Counts
				switch {
				case grid && (n.GridAmounts == 0 || n.Actions < c.actions || n.Heads == 0 || n.Rows == 0):
					t.Errorf("%s %s at %d: checked %+v, want a grid amount cell, %d actions, a head and a row", c.name, page, w, n, c.actions)
				case grid && c.money && (n.MoneyInputs == 0 || n.GridWraps == 0):
					t.Errorf("%s %s at %d: checked %+v, want a money input and a wrap-checked amount", c.name, page, w, n)
				case grid && c.rate && (n.RateInputs == 0 || n.NumberInputs == 0):
					t.Errorf("%s %s at %d: checked %+v, want a rate input and a quantity input", c.name, page, w, n)
				case !grid && c.money && n.CardAmounts == 0:
					t.Errorf("%s %s at %d: checked no line card amount", c.name, page, w)
				}
			}
		}
	}
}
