//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// r7bHead measures the line page's wizard head (UI v2 §5.6, CD-387 (6)):
// below 64rem the app header is hidden and the head is the sticky top bar,
// still at the top after scrolling to the end; from 64rem the header shows.
// At every width the ‹ is at least 44 × 44, sits before the text without
// covering it, and the meta line and the h1 keep to one line each.
const r7bHead = `(() => {
  const out = [];
  const head = document.querySelector("#main .wizard-head");
  if (!head) return ["no wizard head"];
  const back = head.querySelector("a.wizard-close"), h1 = head.querySelector("h1"), meta = head.querySelector(".wizard-head__meta");
  const narrow = innerWidth < 1024;
  const header = getComputedStyle(document.querySelector(".app-header")).display !== "none";
  if (header === narrow) out.push("app header shown " + header);
  if (narrow && getComputedStyle(head).position !== "sticky") out.push("head is " + getComputedStyle(head).position);
  const b = back.getBoundingClientRect(), t = h1.getBoundingClientRect();
  if (b.width < 44 || b.height < 44) out.push("‹ " + b.width + " × " + b.height);
  if (b.right > t.left + 0.5) out.push("‹ ends at " + b.right + ", past the h1's start " + t.left);
  for (const [name, e] of [["h1", h1], ["meta", meta]]) {
    const lh = parseFloat(getComputedStyle(e).lineHeight);
    if (e.getBoundingClientRect().height > lh * 1.5) out.push(name + " wraps: " + e.getBoundingClientRect().height + " px for line height " + lh);
  }
  scrollTo(0, document.documentElement.scrollHeight);
  if (narrow && head.getBoundingClientRect().top < -0.5) out.push("head scrolled away: top " + head.getBoundingClientRect().top);
  scrollTo(0, 0);
  return out;
})()`

// r7bFooter measures the line page's sticky footer: Save line is the bar's
// primary, outside the line form and posting it; below 48rem the bar is
// sticky and the button at least 48 px tall and nearly the bar's width; at
// every width it is at least 44 px tall, and at the end of the page it sits
// at least 8 px below the form's end, never over it.
const r7bFooter = `(() => {
  const out = [];
  const bar = document.querySelector("#main .actionbar"), form = document.querySelector("#line-form");
  if (!bar || !form) return ["bar " + !!bar + ", form " + !!form];
  const b = bar.querySelector(".btn");
  if (b.textContent.trim() !== "Save line" || b.getAttribute("form") !== "line-form" || bar.closest("form")) out.push("bar's button " + b.textContent.trim() + " form=" + b.getAttribute("form"));
  scrollTo(0, document.documentElement.scrollHeight);
  const r = b.getBoundingClientRect();
  if (r.top - form.getBoundingClientRect().bottom < 8) out.push("at the end the button is " + (r.top - form.getBoundingClientRect().bottom) + " px below the form");
  if (r.height < 44) out.push("button " + r.height + " px tall");
  if (innerWidth < 768) {
    if (getComputedStyle(bar).position !== "sticky") out.push("bar is " + getComputedStyle(bar).position);
    if (r.height < 48) out.push("button " + r.height + " px tall");
    if (r.width < innerWidth - 64) out.push("button " + r.width + " px wide");
  }
  scrollTo(0, 0);
  return out;
})()`

// TestE2E_LinePageFocus (CD-387 (6), UI v2 §5.6): every module's line page
// (purchase order and request, expense claim and advance, and the fixture
// order) is in focus mode at 320, 360, 390, 768 and 1280 px: the wizard
// head with its ‹ is the top bar below 64rem, the sticky footer holds Save
// line, focusing each field leaves it above the footer, and nothing scrolls
// sideways. The ‹ goes back to the Items step.
func TestE2E_LinePageFocus(t *testing.T) {
	h := startExpense(t)
	po := h.metroDraft(priya)
	pr := h.Do(priya, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "request_date": plainDate},
		Lines:  []map[string]string{{"item_id": h.idOf("item", "BOLT-M12"), "uom_id": h.idOf("uom", "NOS"), "qty": "25"}},
	}).ID
	adv := h.Do(deepak, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "advance_date": plainDate, "purpose": "Nagpur site visit"},
	}).ID
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	p.Press(saveContinue)
	p.WaitText("Step 2 of 3")
	order := p.DocID()
	d := h.SignIn(deepak, phone390)
	claim := newClaim(d, "", "")

	pages := []struct {
		who, path, meta string
	}{
		{priya, DocPath(purchase.DocType, po) + "/lines/1", "Purchase order"},
		{priya, DocPath(purchase.RequestDocType, pr) + "/lines/new", "Purchase request"},
		{priya, DocPath(fixture.Order, order) + "/lines/new", "Purchase order"},
		{deepak, DocPath(expense.ClaimDocType, claim) + "/lines/new", "Expense claim"},
		{deepak, DocPath(expense.AdvanceDocType, adv) + "/lines/new", "Employee advance"},
	}
	lineFooter := strings.ReplaceAll(footerJS, "#wizard", "#main")
	for _, width := range r12Widths {
		s := map[string]*Session{priya: h.SignIn(priya, width), deepak: h.SignIn(deepak, width)}
		for _, c := range pages {
			u := s[c.who]
			if _, err := chromedp.Call(u.ctx, emulation.SetFocusEmulationEnabled, emulation.SetFocusEmulationEnabledParams{Enabled: true}); err != nil {
				t.Fatal(err)
			}
			u.Open(c.path)
			u.WaitText(saveLine)
			settle(u)
			what := c.path
			if got := Eval[string](u, `document.querySelector("#main .wizard-head__meta")?.textContent.trim() ?? ""`); got != c.meta {
				t.Errorf("%s at %d px: meta line %q, want %q", what, width, got, c.meta)
			}
			if bad := Eval[[]string](u, r7bHead); len(bad) != 0 {
				t.Errorf("%s at %d px: %s", what, width, strings.Join(bad, "; "))
			}
			if bad := Eval[[]string](u, r7bFooter); len(bad) != 0 {
				t.Errorf("%s at %d px: %s", what, width, strings.Join(bad, "; "))
			}
			if bad := Eval[*[]string](u, lineFooter); bad == nil || len(*bad) != 0 {
				t.Errorf("%s at %d px: fields under the footer: %v", what, width, bad)
			}
			noOverflow(u, what)
		}
	}
	// The ‹ goes back to the Items step in one press.
	p.Open(DocPath(purchase.DocType, po) + "/lines/1")
	settle(p)
	p.Do("press ‹", chromedp.Evaluate[chromedp.Void](`document.querySelector("#main .wizard-head a.wizard-close").click()`))
	items := DocPath(purchase.DocType, po) + "/edit/items"
	p.Until("the Items step", `location.pathname === "`+items+`"`)
}
