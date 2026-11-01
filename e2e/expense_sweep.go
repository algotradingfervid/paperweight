//go:build e2e

package e2e

import (
	"context"
	"strconv"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// longExpenseEmployee gives Priya's employee row the longest name before her
// claims and advances are saved, so their employee snapshots carry it to
// every expense page, list row and report (UX §4's longest values).
func (h *Harness) longExpenseEmployee() {
	h.t.Helper()
	if _, err := h.App.DB().SQL().ExecContext(context.Background(),
		`UPDATE employee SET name = ? WHERE id = ?`, uitest.LongName100, h.employee(priya)); err != nil {
		h.t.Fatalf("long employee name: %v", err)
	}
}

// The text MOD-EXP's stale answers share (DOC-069; expense msgPaidStale).
const expenseStale = "after you opened it"

// expenseFormScreens are MOD-EXP S5's 422 and 409 variants of the expense
// pages that post a form of their own: each DocType's line page on line 1
// of the long draft, as Priya, and Mark as paid and Undo paid as Meena,
// with their refusals (already paid, not paid). Waits use ids and text,
// not component classes. With them, the two document page views
// docscreen.Screens doesn't give (UX §5.7, §5.8, §5.11): Ravi, Priya's
// reporting manager, reading her draft and on her pending document as its
// approver.
func expenseFormScreens(s sweepState) []screens.Screen {
	const line, paid = "/doc/{doctype}/{id}/lines/{no}", "/modules/expense/advances/{id}/"
	const page = "/doc/{doctype}/{id}"
	var out []screens.Screen
	for _, dt := range []string{expense.ClaimDocType, expense.AdvanceDocType} {
		url := DocPath(dt, s.exp[dt].Draft) + "/lines/1"
		form := `document.querySelector('form[action$="/lines/1"]')`
		out = append(out,
			screens.Screen{Name: dt + " draft reader", Route: page, URL: DocPath(dt, s.exp[dt].Draft), As: ravi},
			screens.Screen{Name: dt + " pending approver", Route: page, URL: DocPath(dt, s.exp[dt].Pending), As: ravi},
			screens.Screen{Name: dt + " line 422", Route: line, URL: url, As: priya, Setup: expensePost(form,
				`f.querySelectorAll("input[inputmode=numeric], input[inputmode=decimal]").forEach((e) => { e.value = "x"; });`, "#error-summary-title")},
			screens.Screen{Name: dt + " line 409", Route: line, URL: url, As: priya, Setup: expensePost(form, staleVersion, alertWith(expenseStale))},
		)
	}
	advance := func(id int64, page string) string {
		return "/modules/expense/advances/" + strconv.FormatInt(id, 10) + "/" + page
	}
	unpaid, done := s.unpaid, s.exp[expense.AdvanceDocType].Approved
	const paidForm = `document.querySelector('form[action$="/paid"]')`
	const undoForm = `document.querySelector('form[action$="/unpaid"]')`
	return append(out,
		screens.Screen{Name: "advance mark as paid 422", Route: paid + "paid", URL: advance(unpaid, "paid"), As: meena,
			Setup: expensePost(paidForm, `f.elements.paid_on.value = "x";`, "#error-summary-title")},
		screens.Screen{Name: "advance mark as paid 409", Route: paid + "paid", URL: advance(unpaid, "paid"), As: meena,
			Setup: expensePost(paidForm, staleVersion, alertWith(expenseStale))},
		screens.Screen{Name: "advance mark as paid refused", Route: paid + "paid", URL: advance(done, "paid"), As: meena,
			Setup: expensePost(paidForm, "", alertWith("already marked as paid"))},
		screens.Screen{Name: "advance undo paid 409", Route: paid + "unpaid", URL: advance(done, "unpaid"), As: meena,
			Setup: expensePost(undoForm, staleVersion, alertWith(expenseStale))},
		screens.Screen{Name: "advance undo paid refused", Route: paid + "unpaid", URL: advance(unpaid, "unpaid"), As: meena,
			Setup: expensePost(undoForm, "", alertWith("isn't marked as paid"))},
	)
}

// staleVersion posts the version before the page's, so the save is DOC's 409.
const staleVersion = `f.elements.version.value = String(Number(f.elements.version.value) - 1);`

// alertWith is an XPath to an alert holding text.
func alertWith(text string) string {
	return `//*[@role="alert"][contains(., ` + strconv.Quote(text) + `)]`
}

// expensePost runs change on the form form finds (as f), posts it and waits
// until wait is visible: chromedp's plain-string Search, a CSS selector or
// an XPath.
func expensePost(form, change, wait string) func(context.Context) error {
	return func(ctx context.Context) error {
		js := `(() => {
  const f = ` + form + `;
  ` + change + `
  f.noValidate = true;
  f.requestSubmit();
})()`
		return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](js), chromedp.WaitVisible(wait))
	}
}
