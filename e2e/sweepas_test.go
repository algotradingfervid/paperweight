//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// signinHold is how long the test keeps each request for /signin from the
// server, as a loaded machine does: longer than the picker's delay:250ms.
const signinHold = time.Second

// TestE2E_SweepAs_PageWithAPendingSearch: the search the combobox still owes
// must not run on the old page without a session. It would be answered with
// HX-Redirect, and that navigation would abort the sign-in's own.
func TestE2E_SweepAs_PageWithAPendingSearch(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	ctx := uibrowser.New(t)
	as := h.sweepAs()
	step, cancel := context.WithTimeout(ctx, 2*uibrowser.StepTimeout)
	defer cancel()
	if err := as(step, admin); err != nil {
		t.Fatal(err)
	}
	if err := uibrowser.At(ctx, phone360, h.URL+"/doc/"+goodsreceipt.DocType+"/new"); err != nil {
		t.Fatal(err)
	}
	go func() {
		for ev, err := range chromedp.Events(ctx, fetch.RequestPaused) {
			if err != nil {
				return
			}
			go func() {
				time.Sleep(signinHold)
				_, _ = chromedp.Call(ctx, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
			}()
		}
	}()
	if _, err := chromedp.Call(step, fetch.Enable, fetch.EnableParams{Patterns: []*fetch.RequestPattern{{URLPattern: "*/signin*"}}}); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Do(step, chromedp.SendKeys(chromedp.CSS(`input[role=combobox]`), "PO")); err != nil {
		t.Fatal(err)
	}
	if err := as(step, priya); err != nil {
		t.Fatalf("sign in after the search: %v", err)
	}
}
