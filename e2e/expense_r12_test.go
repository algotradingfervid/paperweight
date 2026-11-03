//go:build e2e

package e2e

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// r12Widths are the widths R12's layouts are checked at.
var r12Widths = []int64{320, 360, 390, 768, 1280}

// r12Bar is the sticky footer's measure on a page with a form: below 48rem
// the bar is sticky and its primary is at least 48 px tall and fills the bar; at every width the primary doesn't touch the
// form above it, and no control sits under the bar. It fails on a small
// inline button, whatever the page says it is.
const r12Bar = `(() => {
  const out = [];
  const bar = document.querySelector(".actionbar");
  if (!bar) return ["no sticky bar"];
  const b = bar.querySelector(".btn");
  const r = b.getBoundingClientRect();
  const form = document.querySelector("main form");
  if (bar.closest("form")) out.push("the bar is inside the form");
  if (r.top - form.getBoundingClientRect().bottom < 8) out.push("the button touches the form");
  if (r.height < 44) out.push("button " + r.height + " px tall");
  if (innerWidth < 768) {
    if (getComputedStyle(bar).position !== "sticky") out.push("bar is " + getComputedStyle(bar).position);
    if (r.height < 48) out.push("button " + r.height + " px tall");
    if (r.width < innerWidth - 64) out.push("button " + r.width + " px wide");
  }
  return out;
})()`

// §5.6: the advance line, Mark as paid and Undo paid have the footer the
// claim line has, at every width, on the plain page and on a refusal. Below
// 48rem Undo paid's danger button keeps its own width, as every danger button
// in the bar does; only a primary fills it.
func TestE2E_ExpenseAdvanceFooters(t *testing.T) {
	h := startExpense(t)
	adv := h.paidAdvance("6000.00")
	if _, err := h.App.DB().SQL().ExecContext(t.Context(), `UPDATE employee_advance SET paid_on = NULL WHERE id = ?`, adv); err != nil {
		t.Fatal(err)
	}
	draft := h.Do(deepak, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "advance_date": plainDate, "purpose": "Nagpur site visit"},
	})
	for _, width := range r12Widths {
		d := h.SignIn(deepak, width)
		d.Do("a tall window", chromedp.EmulateViewport(width, 800))
		d.Open(DocPath(expense.AdvanceDocType, draft.ID) + "/lines/new")
		d.WaitText(saveLine)
		settle(d)
		noOverflow(d, "the advance line page")
		r12WantBar(d, "the advance line page")

		m := h.SignIn(meena, width)
		m.Do("a tall window", chromedp.EmulateViewport(width, 800))
		m.Open("/modules/expense/advances/" + strconv.FormatInt(adv, 10) + "/paid")
		m.WaitText("Paid on")
		settle(m)
		noOverflow(m, "Mark as paid")
		r12WantBar(m, "Mark as paid")
		m.Fill("paid_on", "31-02-2026")
		m.Press("Mark as paid")
		m.WaitText("31-02-2026 is not a valid date")
		settle(m)
		noOverflow(m, "Mark as paid, refused")
		r12WantBar(m, "Mark as paid, refused")
	}
	m := h.SignIn(meena, phone390)
	m.Open("/modules/expense/advances/" + strconv.FormatInt(adv, 10) + "/paid")
	m.WaitText("Paid on")
	settle(m)
	m.Press("Mark as paid")
	m.WaitText("Marked as paid.")
	for _, width := range r12Widths {
		m := h.SignIn(meena, width)
		m.Do("a tall window", chromedp.EmulateViewport(width, 800))
		m.Open("/modules/expense/advances/" + strconv.FormatInt(adv, 10) + "/unpaid")
		m.WaitText("Undo paid")
		settle(m)
		noOverflow(m, "Undo paid")
		bad := Eval[[]string](m, r12Bar)
		if width < 768 && len(bad) == 1 && strings.HasSuffix(bad[0], "px wide") {
			bad = nil
		}
		if len(bad) != 0 {
			t.Errorf("Undo paid at %d px: %q", width, bad)
		}
	}
}

func r12WantBar(s *Session, what string) {
	s.t.Helper()
	if bad := Eval[[]string](s, r12Bar); len(bad) != 0 {
		s.t.Errorf("%s at %d px: %s", what, s.width, strings.Join(bad, "; "))
	}
}

// §5.13 item 3: the longest Purpose stays inside its fact at every width,
// beside the employee's name and the crore amount.
func TestE2E_ExpenseKeyFactsLongPurpose(t *testing.T) {
	h := startExpense(t)
	long := strings.Repeat("Pune Metro site ", 9)[:140]
	r := h.Do(deepak, expense.ClaimDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": strconv.FormatInt(h.employee(deepak), 10), "claim_date": plainDate, "purpose": long},
		Lines:  []map[string]string{{"expense_date": plainDate, "expense_type_id": h.idOf("expense_type", "TRAVEL"), "amount": "9999999.99", "no_receipt_reason": "Paid in cash."}},
	})
	id := h.Do(deepak, expense.ClaimDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}).ID
	for _, width := range r12Widths {
		d := h.SignIn(deepak, width)
		d.Open(DocPath(expense.ClaimDocType, id))
		d.WaitText("Claim date")
		settle(d)
		noOverflow(d, "the claim with a long purpose")
		bad := Eval[[]string](d, `[...document.querySelectorAll(".doc-head__facts > div")].flatMap((f) => {
  const r = f.getBoundingClientRect(), v = f.querySelector("dd");
  return v.scrollWidth > v.clientWidth + 1 || v.getBoundingClientRect().right > r.right + 1 ? [f.textContent] : [];
})`)
		if len(bad) != 0 {
			t.Errorf("at %d px these facts spill: %q", width, bad)
		}
		if got := Eval[string](d, `document.querySelector(".doc-head__facts").textContent`); !strings.Contains(got, strings.TrimSpace(long)) {
			t.Errorf("at %d px the facts lost the purpose: %q", width, got)
		}
	}
}

// §5.5 item 1: Claim details and advance Details are a card under the
// required line, inside the screen, with the step's footer below, at every
// width.
func TestE2E_ExpenseDetailsCards(t *testing.T) {
	h := startExpense(t)
	for _, width := range r12Widths {
		d := h.SignIn(deepak, width)
		for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
			d.Open("/doc/" + dt + "/new")
			d.WaitText("Fields marked")
			settle(d)
			noOverflow(d, dt+" Details")
			bad := Eval[[]string](d, `(() => {
  const c = document.querySelector("main .card");
  if (!c) return ["no card"];
  const r = c.getBoundingClientRect(), out = [];
  if (r.left < 0 || r.right > innerWidth) out.push("card " + r.left + " to " + r.right);
  const note = [...document.querySelectorAll("p.field__hint")].find((p) => p.textContent.startsWith("Fields marked"));
  if (note.getBoundingClientRect().bottom > r.top) out.push("the required line isn't above the card");
  const bar = document.querySelector(".actionbar").getBoundingClientRect();
  if (r.bottom > bar.top + scrollY + 1 && getComputedStyle(document.querySelector(".actionbar")).position !== "sticky") out.push("the bar overlaps the card");
  return out;
})()`)
			if len(bad) != 0 {
				t.Errorf("%s at %d px: %q", dt, width, bad)
			}
		}
	}
}
