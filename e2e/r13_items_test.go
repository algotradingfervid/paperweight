//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// r13Widths are the supported widths the plan's layout checks loop over.
var r13Widths = []int64{320, phone360, phone390, 768, 1280}

// r13TileJS is what is wrong with the Add an item tile: its height, its
// dashed edge, a width short of its list's, or a label cut off.
const r13TileJS = `(() => {
  const add = document.querySelector("[data-add-line]");
  if (!add) return ["no add tile"];
  const box = add.getBoundingClientRect(), list = add.closest(".linelist").getBoundingClientRect();
  const s = getComputedStyle(add), bad = [];
  if (Math.round(box.height) !== 56) bad.push("height " + box.height);
  if (Math.abs(box.width - list.width) > 1) bad.push("width " + box.width + " of " + list.width);
  if (s.borderTopStyle !== "dashed" || s.borderLeftStyle !== "dashed") bad.push("border " + s.borderTopStyle);
  if (parseFloat(s.borderTopWidth) < 1) bad.push("border width " + s.borderTopWidth);
  if (add.scrollWidth > add.clientWidth) bad.push("label wider than the tile");
  return bad;
})()`

// TestE2E_R13_AddTileEveryWidth (UI v2 §5.11): Add an item is a 56 px dashed
// full-width tile at every supported width, with no lines and with one, on a
// PO and on a request; with no lines the step shows No items yet. and no
// total rows, and the widest card text (a long quantity and rate, a discount)
// never pushes the page sideways.
func TestE2E_R13_AddTileEveryWidth(t *testing.T) {
	h := startPurchase(t)
	order := map[string]string{
		"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
	}
	request := map[string]string{"site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "request_date": plainDate}
	save := func(dt string, values map[string]string, lines []map[string]string) int64 {
		return h.Do(priya, dt, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines}).ID
	}
	for _, c := range []struct {
		dt, step    string
		empty, full int64
	}{
		{purchase.DocType, "Step 3 of 4", save(purchase.DocType, order, nil), save(purchase.DocType, order, []map[string]string{{
			"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "1234.567", "price": "123456.7891", "discount_pct": "12.5",
		}})},
		{purchase.RequestDocType, "Step 2 of 3", save(purchase.RequestDocType, request, nil), save(purchase.RequestDocType, request, []map[string]string{{
			"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "1234.567",
		}})},
	} {
		path := func(id int64) string {
			return "/doc/" + c.dt + "/" + strconv.FormatInt(id, 10) + "/edit/items"
		}
		for _, w := range r13Widths {
			p := h.SignIn(priya, w)
			p.Open(path(c.empty))
			onStep(p, c.step)
			if bad := Eval[[]string](p, r13TileJS); len(bad) != 0 {
				t.Errorf("%s %d px, no lines: %q", c.dt, w, bad)
			}
			if got := Eval[string](p, `document.querySelector(".empty-state-text")?.innerText ?? ""`); got != "No items yet." {
				t.Errorf("%s %d px: empty text = %q", c.dt, w, got)
			}
			if Eval[bool](p, `document.querySelector("main").innerText.includes("₹0.00")`) {
				t.Errorf("%s %d px: ₹0.00 with no lines", c.dt, w)
			}
			noOverflow(p, c.dt+" empty Items step")

			p.Open(path(c.full))
			onStep(p, c.step)
			if bad := Eval[[]string](p, r13TileJS); len(bad) != 0 {
				t.Errorf("%s %d px, one line: %q", c.dt, w, bad)
			}
			noOverflow(p, c.dt+" Items step with a long line")
		}
	}
}
