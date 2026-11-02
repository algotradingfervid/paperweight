//go:build e2e

package e2e

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
)

// headerShown reports whether the app header is displayed.
func headerShown(s *Session) bool {
	return Eval[string](s, `getComputedStyle(document.querySelector(".app-header")).display`) != "none"
}

// wantHeaderByWidth checks, on the page s is on, that the app header is
// hidden at 390 and 1000 px and shown at 1024 px (UI v2 §5.6, focus mode).
func wantHeaderByWidth(t *testing.T, s *Session, what string) {
	t.Helper()
	for _, c := range []struct {
		width int64
		shown bool
	}{{390, false}, {1000, false}, {1024, true}} {
		s.Do("resize", chromedp.EmulateViewport(c.width, 800))
		if got := headerShown(s); got != c.shown {
			t.Errorf("%s at %d px: app header shown = %v, want %v", what, c.width, got, c.shown)
		}
	}
	s.Do("resize back", chromedp.EmulateViewport(s.width, 800))
}

// TestE2E_WizardClose (UI v2 §5.6, C3b): real docscreen and master wizard
// steps have the close ×, so the app header hides below 64rem. A new PO's ×
// asks "Leave without saving?"; Keep editing stays with what was typed, and
// Leave goes to the list. A saved draft's × goes straight to the document,
// and from there (its check step) to the list.
// A master wizard's × asks too. Focus pages without a × keep the header.
func TestE2E_WizardClose(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	list := "/doc/" + fixture.Order
	p.Open(list + "/new")
	wantHeaderByWidth(t, p, "new PO, details step")

	p.FillID("f-remarks", "Bay 3")
	p.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
	p.Until("the leave dialog", `document.getElementById("leave-wizard").open`)
	if got := Eval[string](p, `document.getElementById("leave-wizard-title").innerText`); got != "Leave without saving?" {
		t.Errorf("dialog title = %q", got)
	}
	p.Press("Keep editing")
	p.Until("the dialog closed", `!document.getElementById("leave-wizard").open`)
	if got := Eval[string](p, `document.getElementById("f-remarks").value + "|" + location.pathname`); got != "Bay 3|"+list+"/new" {
		t.Errorf("after Keep editing: %q, want Bay 3 on %s/new", got, list)
	}
	p.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
	p.Until("the leave dialog", `document.getElementById("leave-wizard").open`)
	p.Press("Leave")
	p.Until("the list", `location.pathname === "`+list+`"`)

	p.Open(list + "/new")
	orderDetails(p)
	p.Press(saveContinue)
	p.WaitText("Step 2 of 3")
	p.Until("the settle", `document.activeElement && document.activeElement.id === "step-title"`)
	id := p.DocID()
	wantHeaderByWidth(t, p, "saved draft, items step")
	if Eval[bool](p, `document.getElementById("leave-wizard") !== null`) {
		t.Error("a saved draft's step has the leave dialog")
	}
	// Focus pages without a × keep the app header at 390 px, so no page is a
	// dead end: the draft's add-line page and a submitted document's page.
	for _, path := range []string{fmt.Sprintf("%s/%d/lines/new", list, id), DocPath(fixture.Order, pendingPO(h))} {
		p.Open(path)
		p.Until("the page", `document.querySelector("body.focus-mode") !== null`)
		if Eval[bool](p, `document.querySelector(".wizard-close") !== null`) {
			t.Errorf("%s has a close ×, want none", path)
		}
		if !headerShown(p) {
			t.Errorf("%s at 390 px: app header hidden on a focus page without a ×, want shown", path)
		}
	}
	p.Open(fmt.Sprintf("%s/%d/edit/items", list, id))
	p.Until("the settle", `document.activeElement && document.activeElement.id === "step-title"`)

	// A draft's document page is its check step; there the × goes to the list.
	check := fmt.Sprintf("%s/%d/edit/check", list, id)
	p.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
	p.Until("the document page", `location.pathname === "`+check+`"`)
	p.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
	p.Until("the list", `location.pathname === "`+list+`"`)

	a := h.SignIn(admin, phone390)
	a.Open("/m/party/new")
	wantHeaderByWidth(t, a, "party wizard, first step")
	a.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
	a.Until("the leave dialog", `document.getElementById("leave-wizard").open`)
	a.Press("Leave")
	a.Until("the party list", `location.pathname === "/m/party"`)
}
