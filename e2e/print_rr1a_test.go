//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// rr1aPrintLines measures the print page's lines on a phone screen: the
// table never scrolls inside itself (nor is named or focused as a scroll
// box), and every cell of every line, Amount
// last, sits inside the screen whole, after its column's name.
const rr1aPrintLines = `(() => {
  const out = [];
  const t = document.querySelector("table.lines");
  if (!t) return ["no lines table"];
  if (t.scrollWidth > t.clientWidth + 1) out.push("the lines scroll inside the table: " + t.scrollWidth + " in " + t.clientWidth);
  if (t.hasAttribute("tabindex") || t.hasAttribute("aria-label")) out.push("the stacked table is still named and focused as a scroll box");
  const rows = t.querySelectorAll("tbody tr");
  if (rows.length === 0) out.push("no lines");
  for (const [i, tr] of [...rows].entries()) {
    for (const td of tr.querySelectorAll("td")) {
      const r = td.getBoundingClientRect();
      const name = "line " + (i + 1) + " " + td.className;
      if (r.left < -0.5 || r.right > innerWidth + 0.5) out.push(name + " runs from " + r.left + " to " + r.right + " in " + innerWidth);
      if (td.scrollWidth > td.clientWidth + 1) out.push(name + " is cut: " + td.scrollWidth + " in " + td.clientWidth);
      const label = getComputedStyle(td, "::before").content;
      if (!label || label === "none" || label === '""') out.push(name + " has no column name");
    }
  }
  return out;
})()`

// rr1aPrintTable is the lines table under print media: a real table with
// its header row and no column names before the cells.
const rr1aPrintTable = `(() => {
  const t = document.querySelector("table.lines");
  return [getComputedStyle(t).display, getComputedStyle(t.querySelector("thead")).display,
    getComputedStyle(t.querySelector("tbody tr")).display, getComputedStyle(t.querySelector("tbody td"), "::before").content].join(" ");
})()`

// TestE2E_PrintLinesStackOnPhone (RR1a 4): the module print pages (PO,
// GRN, claim, advance) with a long item name and a crore amount, at 320,
// 360 and 390 px: each line stacks, each cell after its column's name, so
// Amount is on screen and nothing scrolls inside the table or the page.
// The same holds with JavaScript off. Under print media the A4 table stands: table display, the header row
// repeating, no column names before the cells.
func TestE2E_PrintLinesStackOnPhone(t *testing.T) {
	h := Start(t)
	s := h.prepareSweep()
	const name = "Ordinary Portland Cement OPC 53 Grade, 50 kg bags, UltraTech or equal"
	ctx := context.Background()
	for _, q := range []string{
		`UPDATE purchase_order_line SET snap_item_name = ?1, amount = 1234567890000, rate = 12345678900000 WHERE parent_id = ?2`,
		`UPDATE goods_receipt_line SET snap_item_name = ?1, received_qty = 123456789000, accepted_qty = 123456789000 WHERE parent_id = ?3`,
		`UPDATE expense_claim_line SET amount = 1234567890000, description = ?1 WHERE parent_id = ?4`,
		`UPDATE employee_advance_line SET amount = 1234567890000, description = ?1 WHERE parent_id = ?5`,
	} {
		if _, err := h.App.DB().SQL().ExecContext(ctx, q, name, s.po.order.Approved, s.grn.ids.Approved, s.exp[expense.ClaimDocType].Approved, s.exp[expense.AdvanceDocType].Approved); err != nil {
			t.Fatal(err)
		}
	}
	for _, w := range []int64{phone320, phone360, phone390} {
		a := h.SignIn(admin, w)
		noJS := h.SignIn(admin, w)
		if _, err := chromedp.Call(noJS.ctx, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: true}); err != nil {
			t.Fatalf("JavaScript off: %v", err)
		}
		for dt, id := range map[string]int64{
			purchase.DocType:       s.po.order.Approved,
			goodsreceipt.DocType:   s.grn.ids.Approved,
			expense.ClaimDocType:   s.exp[expense.ClaimDocType].Approved,
			expense.AdvanceDocType: s.exp[expense.AdvanceDocType].Approved,
		} {
			// Without JavaScript each cell still shows its column's name:
			// the server writes data-label (RR1a fix 1).
			noJS.Open(fmt.Sprintf("/print/%s/%d", dt, id))
			noJS.Until("the print page without JavaScript", `document.querySelector("#print-btn") !== null`)
			if bad := Eval[[]string](noJS, rr1aPrintLines); len(bad) != 0 {
				t.Errorf("%s at %d px without JavaScript: %s", dt, w, strings.Join(bad, "; "))
			}
			a.Open(fmt.Sprintf("/print/%s/%d", dt, id))
			a.Until("the print page", `document.querySelector("#print-btn") !== null`)
			if bad := Eval[[]string](a, rr1aPrintLines); len(bad) != 0 {
				t.Errorf("%s at %d px: %s", dt, w, strings.Join(bad, "; "))
			}
			for _, p := range r9Print(a) {
				if !strings.Contains(p, "logo") {
					t.Errorf("%s at %d px: %s", dt, w, p)
				}
			}
			a.Do("print media", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "print"})
				return err
			}))
			if got, want := Eval[string](a, rr1aPrintTable), "table table-header-group table-row none"; got != want {
				t.Errorf("%s at %d px, print media: %q, want %q", dt, w, got, want)
			}
			a.Do("screen media", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
				_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: ""})
				return err
			}))
		}
	}
}

// TestE2E_PrintScrollBoxFollowsWidth (RR1a fix 5): turning or resizing the
// screen, across 48rem or not, checks the scroll box again. Wide, where an order
// line's long code makes the lines scroll in their own box, the table is a
// named tab stop, also when the page opens wide; back at phone width it
// stacks and is neither.
func TestE2E_PrintScrollBoxFollowsWidth(t *testing.T) {
	h := Start(t)
	s := h.prepareSweep()
	// One unbroken code: wide, the table keeps its columns and scrolls.
	name := strings.Repeat("OPC53ULTRATECH50KG", 8)
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE purchase_order_line SET snap_item_name = ?1, amount = 1234567890000, rate = 12345678900000 WHERE parent_id = ?2`, name, s.po.order.Approved); err != nil {
		t.Fatal(err)
	}
	const box = `(() => { const t = document.querySelector("table.lines"); return [t.getAttribute("tabindex"), t.getAttribute("aria-label"), t.hasAttribute("data-allow-overflow"), t.scrollWidth > t.clientWidth].join(" "); })()`
	a := h.SignIn(admin, phone360)
	a.Open(fmt.Sprintf("/print/%s/%d", purchase.DocType, s.po.order.Approved))
	a.Until("the print page", `document.querySelector("#print-btn") !== null`)
	if got, want := Eval[string](a, box), "  false false"; got != want {
		t.Fatalf("at %d px: %q, want %q", phone360, got, want)
	}
	// The browser reports the change a frame later: wait for the result.
	const wide = 780
	a.Do("widen", chromedp.EmulateViewport(wide, 800))
	a.Until("the wide table named as a scroll box", `innerWidth === `+fmt.Sprint(wide)+` && `+box+` === "0 Lines, scrolls sideways true true"`)
	a.Do("narrow", chromedp.EmulateViewport(phone360, 800, chromedp.EmulateMobile, chromedp.EmulateTouch))
	a.Until("the stacked table no longer a scroll box", `innerWidth === `+fmt.Sprint(phone360)+` && `+box+` === "  false false"`)
	// Opened wide, the table is a scroll box from the start.
	b := h.SignIn(admin, wide)
	b.Open(fmt.Sprintf("/print/%s/%d", purchase.DocType, s.po.order.Approved))
	b.Until("the print page", `document.querySelector("#print-btn") !== null`)
	if got, want := Eval[string](b, box), "0 Lines, scrolls sideways true true"; got != want {
		t.Errorf("opened at %d px: %q, want %q", wide, got, want)
	}
	// A resize that stays above 48rem checks again too (RR1a fix 2): with
	// a shorter code, at 1200 px the table fits and is no scroll box; at
	// 800 px it scrolls and is one.
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE purchase_order_line SET snap_item_name = ?1 WHERE parent_id = ?2`, strings.Repeat("OPC53ULTRATECH50KG", 3), s.po.order.Approved); err != nil {
		t.Fatal(err)
	}
	const wider, narrower = 1200, 800
	c := h.SignIn(admin, wider)
	c.Open(fmt.Sprintf("/print/%s/%d", purchase.DocType, s.po.order.Approved))
	c.Until("the print page", `document.querySelector("#print-btn") !== null`)
	if got, want := Eval[string](c, box), "  false false"; got != want {
		t.Fatalf("opened at %d px: %q, want %q (scrollWidth %v)", wider, got, want, Eval[float64](c, `document.querySelector("table.lines").scrollWidth`))
	}
	c.Do("narrow above 48rem", chromedp.EmulateViewport(narrower, 800))
	c.Until("the table named as a scroll box at 800 px", `innerWidth === `+fmt.Sprint(narrower)+` && `+box+` === "0 Lines, scrolls sideways true true"`)
}
