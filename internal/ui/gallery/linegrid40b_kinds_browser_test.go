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

// M0-40b review (CD-313, CD-308 (3)): the display grid's number minimums
// hold for a money cell and in a 3-cell order too. Each sample's widest
// row has the largest quantity, rate and money values; every number keeps
// to one line, right-aligned with its head label, and nothing overflows.

const m40bKindsPath = "/_gallery/m40b-display-kinds"

// m40bKindsSamples are the samples by name: three cells (Item, Quantity,
// Value) and five with Value as the last number (Item, Quantity, Unit,
// Rate, Value).
var m40bKindsSamples = map[string][]components.GridCell{
	"three": {
		{Label: "Item", Text: uitest.LongName100, Sub: uitest.LongCode60},
		{Label: "Quantity", Text: m40bQty, Kind: "qty"},
		{Label: "Value", Text: types.FormatINR(types.MaxMoney), Kind: "money"},
	},
	"five": {
		{Label: "Item", Text: uitest.LongName100, Sub: uitest.LongCode60},
		{Label: "Quantity", Text: m40bQty, Kind: "qty"},
		{Label: "Unit", Text: m40bUnit},
		{Label: "Rate", Text: largestRate, Kind: "rate"},
		{Label: "Value", Text: types.FormatINR(types.MaxMoney), Kind: "money"},
	},
}

// m40bKindsHandler is the gallery with sample ?s= at m40bKindsPath: three
// rows, line 2 with both moves and a note.
func m40bKindsHandler(t *testing.T) http.Handler {
	t.Helper()
	h := Handler()
	r, ok := h.(chi.Router)
	if !ok {
		t.Fatalf("gallery Handler is %T, want a chi.Router", h)
	}
	r.Get(m40bKindsPath, func(w http.ResponseWriter, req *http.Request) {
		cells := m40bKindsSamples[req.URL.Query().Get("s")]
		var rows []components.LineGridRow
		var cards []components.LineCard
		for no := 1; no <= 3; no++ {
			base := "/_gallery/lines?line=" + strconv.Itoa(no)
			row := components.LineGridRow{No: no, Cells: cells, Amount: types.FormatINR(types.MaxMoney), ChangeURL: base, RemoveURL: base, Version: "1"}
			if no > 1 {
				row.MoveUpURL = base
			}
			if no < 3 {
				row.MoveDownURL = base
			}
			if no == 2 {
				row.Note = m40bNote
			}
			rows = append(rows, row)
			cards = append(cards, components.LineCard{No: no, Item: uitest.LongName100, Amount: row.Amount})
		}
		render(w, req, http.StatusOK, titled("Display line grid kinds"), components.LineList(components.LineListProps{Cards: cards, Grid: rows, Total: 3, Display: true}))
	})
	return h
}

// m40bKindsJS checks line 2 (fmt arg: the number cells' labels, JSON):
// each number is on one line and its right edge is its head label's;
// nothing in the row overflows and the grid doesn't scroll.
const m40bKindsJS = `((labels) => {
  const out = [], grid = document.querySelector(".line-editor .linegrid");
  if (!grid || !grid.checkVisibility()) return ["the display grid is not shown"];
  const head = grid.querySelector(".linegrid-head"), row = grid.querySelectorAll(".linegrid-row")[1];
  const norm = (s) => s.replace(/\s+/g, " ").trim();
  const box = (n) => { const r = document.createRange(); r.selectNodeContents(n); return r; };
  for (const el of [row, ...row.querySelectorAll("*")]) {
    const cs = getComputedStyle(el);
    if (cs.display === "none" || cs.display === "contents" || el instanceof SVGElement || el.clientWidth === 0) continue;
    if (el.scrollWidth > el.clientWidth + 1 && cs.textOverflow !== "ellipsis") out.push("content wider than box: " + el.className + " " + el.scrollWidth + " > " + el.clientWidth);
  }
  if (grid.scrollWidth > grid.clientWidth + 1) out.push("the grid scrolls: " + grid.scrollWidth + " > " + grid.clientWidth);
  const heads = [...head.children], nums = [...row.querySelectorAll("[data-kind]")];
  if (nums.length !== labels.length) out.push(nums.length + " number cells, want " + labels.length);
  nums.forEach((n, i) => {
    const lines = new Set([...box(n).getClientRects()].filter((r) => r.width > 0).map((r) => Math.round(r.top))).size;
    if (lines !== 1) out.push(labels[i] + " " + JSON.stringify(norm(n.textContent)) + " takes " + lines + " lines, want 1");
    const h = heads.find((c) => norm(c.textContent) === labels[i]);
    if (!h) { out.push("no head label " + labels[i]); return; }
    const hr = box(h.firstChild).getBoundingClientRect().right, vr = box(n).getBoundingClientRect().right;
    if (Math.abs(hr - vr) > 1.5) out.push(labels[i] + ": the value ends at " + Math.round(vr) + ", its head label at " + Math.round(hr));
  });
  return out;
})(%s)`

// TestGalleryBrowser_DisplayLineGridKinds (M0-40b review): the three- and
// five-cell samples with a money cell fit with the line editor exactly
// 64rem wide and at 1440.
func TestGalleryBrowser_DisplayLineGridKinds(t *testing.T) {
	s := httptest.NewServer(m40bKindsHandler(t))
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	const wide = 1440
	width := `Math.round(document.querySelector(".line-editor").getBoundingClientRect().width)`
	for name, labels := range map[string]string{"three": `["Quantity","Value"]`, "five": `["Quantity","Rate","Value"]`} {
		url := s.URL + m40bKindsPath + "?s=" + name
		if err := uibrowser.At(ctx, wide, url); err != nil {
			t.Fatal(err)
		}
		at64 := wide - (gridEval[int64](t, ctx, "the line editor's width", width) - gridFrom)
		for _, w := range []int64{at64, wide} {
			if err := uibrowser.At(ctx, w, url); err != nil {
				t.Fatal(err)
			}
			if got := gridEval[int64](t, ctx, "the line editor's width", width); w == at64 && got != gridFrom {
				t.Errorf("%s at %d: the line editor is %d px wide, want %d", name, w, got, gridFrom)
			}
			for _, p := range gridEval[[]string](t, ctx, name+" sample's widest row", fmt.Sprintf(m40bKindsJS, labels)) {
				t.Errorf("%s at %d: %s", name, w, p)
			}
			probs, err := uibrowser.Overflow(ctx)
			if err != nil {
				t.Fatalf("%s at %d: overflow: %v", name, w, err)
			}
			for _, p := range probs {
				t.Errorf("%s at %d: overflow: %s", name, w, p)
			}
		}
	}
}
