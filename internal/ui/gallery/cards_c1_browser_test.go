//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestCardsBrowser_SwappedCardFocusesHeading saves the cards page's bill
// card, whose answer swaps the card with FocusHeading set, and checks that
// app.js put focus on the new card's heading (CD-358). At load nothing
// takes focus there: the heading is focused after a swap only.
func TestCardsBrowser_SwappedCardFocusesHeading(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 390, s.URL+"/_gallery/cards"); err != nil {
		t.Fatal(err)
	}
	step := func(what string, a ...chromedp.Action[chromedp.Void]) {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		if err := chromedp.Do(sctx, a...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	eval := func(what, js string) string {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		got, err := chromedp.Run(sctx, chromedp.Evaluate[string](js))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return got
	}
	const active = `(document.activeElement.tagName + " " + document.activeElement.textContent.trim())`
	if got := eval("at load", active); got == "H2 Bill details" {
		t.Errorf("focus at load = %q, want it elsewhere", got)
	}
	step("save the bill",
		chromedp.SendKeys(chromedp.CSS("#c-bill"), "B-77"),
		chromedp.Click(chromedp.CSS(`#bill-form button[type=submit]`)),
		chromedp.Poll[chromedp.Void](`document.querySelector("#bill-form [role=status]") !== null && document.activeElement.matches("h2")`))
	if got := eval("after save", active); got != "H2 Bill details" {
		t.Errorf("focus after the swap = %q, want H2 Bill details", got)
	}
	if got := eval("tabindex", `document.activeElement.getAttribute("tabindex")`); got != "-1" {
		t.Errorf("focused heading tabindex = %q, want -1", got)
	}
}
