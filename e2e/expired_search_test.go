//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// TestE2E_SessionEndsDuringSearch: a search sent after the session ended sends
// the user to sign in, and signing in returns to the page, not to that search's
// bare fragment.
func TestE2E_SessionEndsDuringSearch(t *testing.T) {
	h := Start(t)
	h.prepareSweep()
	s := h.SignIn(admin, desktop)
	page := "/doc/" + goodsreceipt.DocType + "/new"
	s.Open(page)
	if _, err := chromedp.Call(s.ctx, network.ClearBrowserCookies, cdp.Empty{}); err != nil {
		t.Fatal(err)
	}
	s.Do("type a search", chromedp.SendKeys(chromedp.CSS(`input[role=combobox]`), "PO"))
	s.Until("the sign-in page", `location.pathname === "/signin"`)
	s.Fill("email", admin)
	s.Fill("password", password)
	s.Press("Sign in")
	s.Until("the page again", `location.pathname === `+strconv.Quote(page)+` && document.querySelector("input[role=combobox]") !== null`)
}
