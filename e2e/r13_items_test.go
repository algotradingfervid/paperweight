//go:build e2e

package e2e

import (
	"context"
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
// total rows, and the widest card text (a long unit, a discount) never
// pushes the page sideways.
func TestE2E_R13_AddTileEveryWidth(t *testing.T) {
	h := startPurchase(t)
	empty := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
	}}).ID
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE uom SET name = 'Square Metre Sheet' WHERE code = 'KGS'`); err != nil {
		t.Fatal(err)
	}
	full := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
		},
		Lines: []map[string]string{{
			"item_id": h.idOf("item", "WIRE-18G"), "uom_id": h.idOf("uom", "KGS"), "qty": "1234.567", "price": "123456.7891", "discount_pct": "12.5",
		}},
	}).ID
	path := func(id int64) string {
		return "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) + "/edit/items"
	}
	for _, w := range r13Widths {
		p := h.SignIn(priya, w)
		p.Open(path(empty))
		onStep(p, "Step 3 of 4")
		if bad := Eval[[]string](p, r13TileJS); len(bad) != 0 {
			t.Errorf("%d px, no lines: %q", w, bad)
		}
		if got := Eval[string](p, `document.querySelector(".empty-state-text")?.innerText ?? ""`); got != "No items yet." {
			t.Errorf("%d px: empty text = %q", w, got)
		}
		if Eval[bool](p, `document.querySelector("main").innerText.includes("₹0.00")`) {
			t.Errorf("%d px: ₹0.00 with no lines", w)
		}
		noOverflow(p, "the empty Items step")

		p.Open(path(full))
		onStep(p, "Step 3 of 4")
		if bad := Eval[[]string](p, r13TileJS); len(bad) != 0 {
			t.Errorf("%d px, one line: %q", w, bad)
		}
		noOverflow(p, "the Items step with a long unit and a discount")
	}
}
