//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// landingWidths are the widths R2 measures at.
var landingWidths = []int64{phone320, phone360, phone390, 768, 1280}

// tallPage scrolls s to the bottom of a page made taller than any answer, so
// a swap that keeps the scroll position stays scrolled down. The padding is on
// <body>, which no swap replaces.
func tallPage(s *Session) {
	s.t.Helper()
	settle(s)
	if y := Eval[float64](s, `(() => {
  document.body.style.paddingBottom = "4000px";
  window.scrollTo({top: document.documentElement.scrollHeight, behavior: "instant"});
  return window.scrollY;
})()`); y <= 0 {
		s.t.Fatalf("at %d px the page didn't scroll: scrollY %v", s.width, y)
	}
}

// pressInPlace clicks the visible button labelled label without scrolling to
// it (Press centres it first).
func pressInPlace(s *Session, label string) {
	s.t.Helper()
	find := `[...document.querySelectorAll("button")].find((b) => b.checkVisibility() && b.textContent.replace(/\s+/g, " ").trim() === ` + strconv.Quote(label) + `)`
	s.Until("a button "+strconv.Quote(label), find+` !== undefined`)
	s.Do("press "+label, chromedp.Evaluate[chromedp.Void](find+`.click()`))
}

// landedOnBlock fails unless the page is at scroll 0 with the success block
// focused and wholly in view below any sticky head.
func landedOnBlock(s *Session, what string) {
	s.t.Helper()
	s.Until(what+": the success block", `document.querySelector('#main .success-block[role="status"]') !== null`)
	settle(s)
	if y := Eval[float64](s, `window.scrollY`); y != 0 {
		s.t.Errorf("%s at %d px: scrollY %v, want 0", what, s.width, y)
	}
	if !Eval[bool](s, `document.activeElement?.matches("#main .success-block") === true`) {
		s.t.Errorf("%s at %d px: focus is on %s, want the success block", what, s.width, Eval[string](s, `document.activeElement.outerHTML.slice(0, 80)`))
	}
	got := Eval[[]float64](s, `(() => {
  const r = document.querySelector("#main .success-block").getBoundingClientRect();
  const head = [document.querySelector(".app-header"), document.querySelector(".wizard-head")].filter((e) => e?.checkVisibility()).map((e) => e.getBoundingClientRect().bottom);
  return [r.top, r.bottom, Math.max(0, ...head), innerHeight];
})()`)
	if got[0] < got[2]-0.5 || got[1] > got[3]+0.5 {
		s.t.Errorf("%s at %d px: block spans %v to %v, want it below the head (%v) and inside the viewport (%v)", what, s.width, got[0], got[1], got[2], got[3])
	}
}

// TestE2E_SubmitLandsOnTheSuccessBlock (R2; EXP H, GRN H3; UI v2 §5.13 item
// 2): Submit from a scrolled Check page lands at scroll 0 with the success
// block first in view and focused, at every width, and the tab title is the
// document page's.
func TestE2E_SubmitLandsOnTheSuccessBlock(t *testing.T) {
	h := Start(t)
	for _, width := range landingWidths {
		t.Run(strconv.FormatInt(width, 10), func(t *testing.T) {
			id := claimDraft(h).ID
			d := h.SignIn(deepak, width)
			d.Open(checkStep(id))
			tallPage(d)
			pressInPlace(d, sendApproval)
			landedOnBlock(d, "after Submit")
			titleIsPages(d, "after Submit", false)
		})
	}
}

// TestE2E_ApproveLandsOnTheSuccessBlock (R2): Approve from a scrolled document
// page lands the same way.
func TestE2E_ApproveLandsOnTheSuccessBlock(t *testing.T) {
	h := Start(t)
	for _, width := range []int64{phone360, 1280} {
		t.Run(strconv.FormatInt(width, 10), func(t *testing.T) {
			r := claimDraft(h)
			h.Do(deepak, fixture.Claim, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
			p := h.SignIn(priya, width)
			p.Open(DocPath(fixture.Claim, r.ID))
			tallPage(p)
			pressInPlace(p, "Approve")
			landedOnBlock(p, "after Approve")
		})
	}
}

// pageTitle is the title a full load of the current URL gives.
func pageTitle(s *Session) string {
	s.t.Helper()
	return Eval[string](s, `fetch(location.href, {credentials: "same-origin"}).then((r) => r.text()).then((t) => new DOMParser().parseFromString(t, "text/html").title)`, chromedp.EvalAwaitPromise)
}

// titleIsPages fails unless the tab title is the one a full load of the
// current URL gives, with "Error: " in front when refused.
func titleIsPages(s *Session, what string, refused bool) {
	s.t.Helper()
	settle(s)
	want := pageTitle(s)
	if refused {
		want = "Error: " + want
	}
	if got := Eval[string](s, `document.title`); got != want {
		s.t.Errorf("%s: title %q, want %q", what, got, want)
	}
}

// refusedTitle waits for the error summary, then checks the title.
func refusedTitle(s *Session, what string) {
	s.t.Helper()
	s.Until(what+": the error summary", `!!document.querySelector("#main .error-summary")`)
	titleIsPages(s, what, true)
}

// TestE2E_TitleFollowsEveryHtmxMove (R2; PO L, EXP M; UI v2 §5.14): after
// each htmx move the tab title is the new page's, and a refusal's carries
// "Error: ": document wizard steps, the line page, Check, a master record and a
// master wizard. User admin posts whole pages, which already set it.
func TestE2E_TitleFollowsEveryHtmxMove(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)

	p.Open("/doc/" + fixture.Order + "/new")
	first := Eval[string](p, `document.title`)
	orderDetails(p)
	p.Press(saveContinue)
	onStep(p, "Step 2 of 3")
	titleIsPages(p, "Items", false)
	if got := Eval[string](p, `document.title`); got == first {
		t.Errorf("Items kept the first step's title %q", got)
	}
	p.Press("Back")
	onStep(p, "Step 1 of 3")
	titleIsPages(p, "back on Details", false)
	Eval[bool](p, `(document.getElementById("f-amount").value = "not a number", true)`)
	p.Press(saveContinue)
	refusedTitle(p, "Details refused")
	p.Fill("amount", "94,164.00")
	p.Press(saveContinue)
	onStep(p, "Step 2 of 3")
	titleIsPages(p, "Items after the refusal", false)

	p.Press(addItem)
	p.WaitText(saveLine)
	titleIsPages(p, "the line page", false)
	Eval[bool](p, `(document.getElementById("f-qty").value = "not a number", true)`)
	p.Press(saveLine)
	refusedTitle(p, "the line page refused")
	p.Open(DocPath(fixture.Order, p.DocID()) + "/edit/items")
	onStep(p, "Step 2 of 3")
	p.Press(saveContinue)
	onStep(p, "Step 3 of 3")
	titleIsPages(p, "Check", false)

	a := h.SignIn(admin, phone390)
	a.Open("/m/uom/" + h.idOf("uom", "KGS"))
	a.Fill("code", "BAD CODE!")
	a.Press("Save")
	refusedTitle(a, "a master record refused")

	a.Open("/m/party/new")
	a.Press("Save and continue")
	refusedTitle(a, "a master wizard step refused")
	a.Fill("code", "LAKSHMI")
	a.Fill("name", "Lakshmi Traders")
	pickRadio(a, "role", "Vendor")
	a.Press("Save and continue")
	a.Until("the next step", `location.pathname !== "/m/party/new"`)
	titleIsPages(a, "the master wizard's next step", false)

	a.Open("/users/new")
	a.Fill("name", "Second Priya")
	a.Fill("email", priya)
	a.Fill("password", "a long enough password 2026")
	a.Press("Add user")
	// A plain POST to /users: its answer is the New user page, refused.
	a.Until("a new user refused", `!!document.querySelector("#main .error-summary")`)
	if got := Eval[string](a, `document.title`); got != "Error: New user · Paperweight" {
		t.Errorf("a new user refused: title %q", got)
	}
}
