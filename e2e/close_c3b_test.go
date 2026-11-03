//go:build e2e

package e2e

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

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

// pressX presses the wizard head's ×.
func pressX(s *Session) {
	s.t.Helper()
	s.Do("press ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").click()`))
}

// TestE2E_WizardClose (UI v2 §5.6, C3b, CD-387 (4)): real docscreen and master
// wizard steps have the close ×, so the app header hides below 64rem. The ×
// goes to the list in one press, from a new PO, from every step of a saved
// draft and from a master wizard; it asks "Leave without saving?" first only
// when the step has unsaved changes. Keep editing stays with what was typed,
// and Leave goes to the list. Focus pages without a × keep the header.
func TestE2E_WizardClose(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	list := "/doc/" + fixture.Order
	p.Open(list + "/new")
	wantHeaderByWidth(t, p, "new PO, details step")
	pressX(p)
	p.Until("the list, unasked", `location.pathname === "`+list+`"`)

	// A step redrawn with a refusal holds values nothing saved: the × asks,
	// though nothing was typed since the redraw (data-unsaved).
	p.Open(list + "/new")
	p.Fill("order_date", "99-99-9999")
	p.Press(saveContinue)
	p.Until("the refused step", `document.querySelector("form#wizard-form[data-unsaved]") !== null`)
	pressX(p)
	p.Until("the leave dialog after a refusal", `document.getElementById("leave-wizard").open`)
	p.Press("Leave")
	p.Until("the list", `location.pathname === "`+list+`"`)

	// Enter on the ×: a clean step goes to the list; a changed one opens the
	// dialog, which stays open (the keypress after Enter must not press its
	// first button).
	p.Open(list + "/new")
	// A keyup whose keydown was elsewhere (Enter pressed in a field, focus
	// then on the ×) does nothing.
	p.Do("a stray keyup on ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").dispatchEvent(new KeyboardEvent("keyup", {key: "Enter", bubbles: true}))`))
	p.Do("let it act", chromedp.Sleep(400*time.Millisecond))
	if got := Eval[string](p, `location.pathname`); got != list+"/new" {
		t.Errorf("a stray keyup on × went to %q, want to stay on %s/new", got, list)
	}
	p.Do("focus ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").focus()`))
	p.Keys(kb.Enter)
	p.Until("the list after Enter, unasked", `location.pathname === "`+list+`"`)
	p.Open(list + "/new")
	p.FillID("f-remarks", "Bay 5")
	p.Do("focus ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").focus()`))
	p.Keys(kb.Enter)
	p.Until("the leave dialog after Enter", `document.getElementById("leave-wizard").open`)
	p.Do("let the key finish", chromedp.Sleep(400*time.Millisecond))
	if !Eval[bool](p, `document.getElementById("leave-wizard").open`) {
		t.Error("the leave dialog closed right after Enter opened it")
	}
	p.Press("Keep editing")
	p.Until("the dialog closed", `!document.getElementById("leave-wizard").open`)
	// Ctrl+Enter keeps the browser's own link behaviour: no dialog opens.
	// What the browser then does is its own (headless follows the link in
	// this tab), so showModal records any open in sessionStorage, which a
	// same-tab navigation keeps.
	p.Do("record dialog opens", chromedp.Evaluate[chromedp.Void](`(() => { sessionStorage.removeItem("r7-opened"); const show = HTMLDialogElement.prototype.showModal; HTMLDialogElement.prototype.showModal = function () { sessionStorage.setItem("r7-opened", this.id); return show.call(this); }; })()`))
	p.Do("focus ×", chromedp.Evaluate[chromedp.Void](`document.querySelector(".wizard-close").focus()`))
	p.Do("press Ctrl+Enter", chromedp.KeyEvent(kb.Enter, chromedp.KeyModifiers(kb.ModifierCtrl)))
	p.Do("let the key finish", chromedp.Sleep(400*time.Millisecond))
	if got := Eval[string](p, `sessionStorage.getItem("r7-opened") ?? ""`); got != "" {
		t.Errorf("Ctrl+Enter on × opened dialog %q, want none", got)
	}

	p.Open(list + "/new")
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
	// A saved draft's clean step and its check step: the list in one press.
	for _, step := range []string{"items", "check"} {
		p.Open(fmt.Sprintf("%s/%d/edit/%s", list, id, step))
		p.Until("the settle", `document.activeElement && document.activeElement.id === "step-title"`)
		pressX(p)
		p.Until("the list from "+step, `location.pathname === "`+list+`"`)
	}
	// A change on a saved draft's step asks, saying the draft keeps the rest.
	p.Open(fmt.Sprintf("%s/%d/edit/details", list, id))
	p.FillID("f-remarks", "Bay 4")
	pressX(p)
	p.Until("the leave dialog", `document.getElementById("leave-wizard").open`)
	if got := Eval[string](p, `document.querySelector("#leave-wizard p").innerText`); got != "What you changed on this step is lost. The draft keeps what was saved before." {
		t.Errorf("draft's dialog text = %q", got)
	}
	p.Press("Leave")
	p.Until("the list", `location.pathname === "`+list+`"`)

	a := h.SignIn(admin, phone390)
	a.Open("/m/party/new")
	wantHeaderByWidth(t, a, "party wizard, first step")
	pressX(a)
	a.Until("the party list, unasked", `location.pathname === "/m/party"`)
	a.Open("/m/party/new")
	a.Fill("code", "LAKSHMI")
	pressX(a)
	a.Until("the leave dialog", `document.getElementById("leave-wizard").open`)
	a.Press("Leave")
	a.Until("the party list", `location.pathname === "/m/party"`)
}
