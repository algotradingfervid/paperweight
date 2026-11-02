//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// footerTotalText is the Items footer total's text, "" without one.
const footerTotalText = `(document.querySelector("#wizard .actionbar #footer-total")?.innerText ?? "").replace(/\s+/g, " ").trim()`

// TestE2E_POItemsFooterTotal (A-22, CD-378) at 390 px: a draft PO's Items
// footer shows "{n} items · Taxable value ₹…" in the sticky footer, and after
// a line is saved it shows the new count and total. "Save and finish later"
// sits in the page flow directly after the footer, not inside it; Enter in a
// field presses the primary (the step moves on), never Save and finish later.
func TestE2E_POItemsFooterTotal(t *testing.T) {
	h := startPurchase(t)
	id := h.metroDraft(priya) // one line: WIRE-18G 10 KGS at ₹96
	p := h.SignIn(priya, phone390)
	base := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) + "/edit"
	p.Open(base + "/items")
	onStep(p, "Step 3 of 4")
	if got := Eval[string](p, footerTotalText); got != "1 item · Taxable value ₹960.00" {
		t.Errorf("footer total = %q, want 1 item · Taxable value ₹960.00", got)
	}
	// The finish-later row follows the sticky footer in the page flow
	// (CD-378 (1)), not inside it.
	if !Eval[bool](p, `(() => {
  const bar = document.querySelector("#wizard .actionbar");
  const later = bar?.nextElementSibling;
  return !!later && later.matches(".actionbar-later") && !!later.querySelector("button[value=finish]") && !bar.querySelector(".actionbar-later");
})()`) {
		t.Errorf("Save and finish later isn't the row right after the footer\n%s", p.page())
	}

	addLine(p, "CEMENT-OPC", "40", "1,300")
	onStep(p, "Step 3 of 4")
	p.Until("the footer total after the line save", footerTotalText+` === "2 items · Taxable value ₹52,960.00"`)

	// Enter in a field presses the primary: Details saves and moves on to
	// Terms, where Save and finish later would leave the wizard.
	p.Open(base + "/details")
	onStep(p, "Step 1 of 4")
	p.Fill("order_date", fixtureDate)
	p.Keys(kb.Enter)
	p.Until("Enter moved on to Terms", `location.pathname === `+strconv.Quote(base+"/terms"))
	onStep(p, "Step 2 of 4")
}

// bigLines is n PO lines of WIRE-18G, 1 KGS at ₹6,00,00,000 each.
func (h *Harness) bigLines(n int) []map[string]string {
	h.t.Helper()
	item, uom := h.idOf("item", "WIRE-18G"), h.idOf("uom", "KGS")
	out := make([]map[string]string, n)
	for i := range out {
		out[i] = map[string]string{"item_id": item, "uom_id": uom, "qty": "1", "price": "60000000"}
	}
	return out
}

// summaryJS is what is wrong with the Items footer's summary: more than one
// 24 px line, or a part of it that ends past the summary's box (a block box
// keeps its own width when its content overruns, so each part is measured).
const summaryJS = `(() => {
  const s = document.querySelector("#wizard .actionbar .actionbar-summary");
  if (!s) return ["no summary row"];
  const out = [], r = s.getBoundingClientRect();
  if (r.height > 24.5) out.push("summary height " + r.height + " > one 24 px line");
  const parts = [...s.querySelectorAll("#footer-total > *")];
  if (parts.length === 0) out.push("no parts to measure");
  for (const e of parts) {
    if (e.getBoundingClientRect().width <= 1) continue; // the visually hidden label
    const end = e.getBoundingClientRect().right;
    if (end > r.right + 0.5) out.push(JSON.stringify(e.textContent) + " ends at " + end + " past " + r.right);
  }
  return out;
})()`

// labelFrom is the viewport width from which the footer shows the total's
// label; below it the label is visually hidden (actionbar-total-label).
const labelFrom = 400

// TestE2E_POItemsFooterOneLine (A-12 with A-22, CD-378 (2)): a draft PO at the
// worst case, 125 lines totalling ₹7,50,00,00,000.00 (GST on more passes the
// money limit), keeps its Items footer summary on one line and inside its box
// at every listed width: the widths each side of labelFrom and the phone
// widths below it. So does the same text with the largest Money value, one
// digit longer, written into the page. The page doesn't scroll sideways, and
// focusing each field of the step leaves it above the footer (footerJS, as
// A-12).
func TestE2E_POItemsFooterOneLine(t *testing.T) {
	h := startPurchase(t)
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
		},
		Lines: h.bigLines(125),
	})
	items := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(r.ID, 10) + "/edit/items"
	const want = "125 items · Taxable value ₹7,50,00,00,000.00"
	for _, w := range []int64{320, phone360, 384, phone390, labelFrom - 1, labelFrom} {
		p := h.SignIn(priya, w)
		p.Open(items)
		onStep(p, "Step 3 of 4")
		if got := Eval[string](p, footerTotalText); got != want {
			t.Errorf("%d px: footer total = %q, want %q", w, got, want)
		}
		hidden := Eval[bool](p, `getComputedStyle(document.querySelector("#footer-total .actionbar-total-label")).position === "absolute"`)
		if hidden != (w < labelFrom) {
			t.Errorf("%d px: label visually hidden = %v, want %v", w, hidden, w < labelFrom)
		}
		if bad := Eval[[]string](p, summaryJS); len(bad) != 0 {
			t.Errorf("%d px: %q", w, bad)
		}
		noOverflow(p, "the 125-line Items step")
		Eval[bool](p, `(document.querySelector("#footer-total").lastElementChild.textContent = `+strconv.Quote(largestShown)+`, true)`)
		if bad := Eval[[]string](p, summaryJS); len(bad) != 0 {
			t.Errorf("%d px, largest Money: %q", w, bad)
		}
		noOverflow(p, "the largest Money in the footer")
		if bad := Eval[[]string](p, footerJS); len(bad) != 0 {
			t.Errorf("%d px: fields under the footer: %q", w, bad)
		}
	}
}
