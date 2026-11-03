//go:build e2e

package e2e

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const tablet768 = 768

// pickerStackJS reports how an open Picker paints, given its list's id. Its
// overlay is the phone sheet, or from 48rem the dropdown. Trapped names each
// ancestor that makes a stacking context, so the overlay's z-index counts
// only inside that ancestor. Covered names each point of the overlay, and its
// Close button, where something else paints on top; hit-testing follows paint
// order, so this is the painted result. Under names the chrome the overlay
// was seen on top of (top bar, sticky bar, another disclosure), which proves
// the points tested something. The phone sheet makes the page behind it inert
// and hit-testing skips inert elements, so the check lifts inert while it looks.
const pickerStackJS = `((id) => {
  const search = document.getElementById(id).closest(".picker__search");
  const box = getComputedStyle(search).position === "fixed" ? search : document.getElementById(id);
  const name = (e) => e.tagName.toLowerCase() + "." + String(e.className).trim().split(/\s+/).join(".");
  const traps = (e) => {
    const c = getComputedStyle(e), out = [];
    const item = /flex|grid/.test(getComputedStyle(e.parentElement).display);
    if (c.position === "fixed" || c.position === "sticky") out.push("position " + c.position);
    if ((c.position !== "static" || item) && c.zIndex !== "auto") out.push("z-index " + c.zIndex);
    if (c.isolation === "isolate") out.push("isolation");
    if (c.opacity !== "1") out.push("opacity " + c.opacity);
    if (c.mixBlendMode !== "normal") out.push("mix-blend-mode");
    if (c.contain !== "none") out.push("contain " + c.contain);
    if (c.willChange !== "auto") out.push("will-change " + c.willChange);
    for (const p of ["transform", "translate", "rotate", "scale", "filter", "backdropFilter", "perspective", "clipPath", "maskImage", "viewTransitionName"]) {
      if (c[p] !== "none") out.push(p);
    }
    return out;
  };
  const trapped = [], covered = [], under = new Set();
  const inert = [...document.querySelectorAll("[inert]")];
  inert.forEach((e) => { e.inert = false; });
  for (let e = box.parentElement; e && e !== document.documentElement; e = e.parentElement) {
    const t = traps(e);
    if (t.length) trapped.push(name(e) + ": " + t.join(", "));
  }
  const look = (what, x, y) => {
    const at = document.elementsFromPoint(x, y);
    if (at.length === 0) return;
    if (!box.contains(at[0])) covered.push(what + " " + Math.round(x) + "," + Math.round(y) + ": " + name(at[0]));
    for (const e of at) {
      if (e.closest(".app-header, .wizard-head")) under.add("top bar");
      if (e.closest(".actionbar")) under.add("sticky bar");
      if (e.closest("details.disclosure") && !e.closest("details.disclosure").contains(box)) under.add("disclosure");
    }
  };
  const r = box.getBoundingClientRect();
  const bottom = Math.min(r.bottom, innerHeight);
  // 12 px in, past the dropdown's rounded corners, which hit-testing skips.
  for (let y = Math.max(r.top, 0) + 12; y < bottom - 12; y += 16) {
    for (const x of [r.left + 12, (r.left + r.right) / 2, r.right - 12]) look("overlay", x, y);
  }
  const close = search.querySelector("[data-picker-close]");
  if (close.checkVisibility()) {
    const c = close.getBoundingClientRect();
    look("Close", c.left + c.width / 2, c.top + c.height / 2);
  }
  inert.forEach((e) => { e.inert = true; });
  return {trapped, covered, under: [...under]};
})`

// pickerStack is what pickerStackJS returns.
type pickerStack struct {
	Trapped []string `json:"trapped"`
	Covered []string `json:"covered"`
	Under   []string `json:"under"`
}

// openPickerOnTop opens the Picker of field and checks that nothing traps or
// covers it, and that it was seen on top of each of over.
func openPickerOnTop(s *Session, field string, over ...string) {
	s.t.Helper()
	list := components.FieldID(field) + "-list"
	clickOn(s, `input[aria-controls="`+list+`"]`)
	s.Until(field+" open", `document.getElementById("`+list+`").closest(".picker__search").hasAttribute("data-open") && !!document.querySelector("#`+list+` [role=option]")`)
	s.Do(field+" settled", chromedp.Evaluate[chromedp.Void](`Promise.all(document.getAnimations().map((a) => a.finished))`, chromedp.EvalAwaitPromise))
	got := Eval[pickerStack](s, pickerStackJS+`(`+strconv.Quote(list)+`)`)
	if len(got.Trapped) != 0 {
		s.t.Errorf("%s at %d px is trapped by %q", field, s.width, got.Trapped)
	}
	if len(got.Covered) != 0 {
		s.t.Errorf("%s at %d px is covered at %q", field, s.width, got.Covered)
	}
	for _, want := range over {
		if !slices.Contains(got.Under, want) {
			s.t.Errorf("%s at %d px: never seen over the %s (seen over %q)", field, s.width, want, got.Under)
		}
	}
}

// TestE2E_DisclosedPickerOnTop_ExpenseLine (CD-379 F1): on the expense line
// page each Picker inside a disclosure opens above the header, the sticky
// bar and the disclosures after it. Tax code is in Bill in company GSTIN;
// Project and Site are in Project or site; both groups are nested in More
// details (UI v2 E7).
func TestE2E_DisclosedPickerOnTop_ExpenseLine(t *testing.T) {
	h := startExpense(t)
	cases := []struct {
		width int64
		field string
		over  []string
	}{
		{phone320, "tax_code_id", []string{"top bar", "sticky bar", "disclosure"}},
		{phone320, "project_id", []string{"top bar", "sticky bar"}},
		{phone320, "site_id", []string{"top bar", "sticky bar"}},
		{phone360, "tax_code_id", []string{"top bar", "sticky bar", "disclosure"}},
		{phone360, "project_id", []string{"top bar", "sticky bar"}},
		{phone360, "site_id", []string{"top bar", "sticky bar"}},
		{tablet768, "tax_code_id", []string{"sticky bar", "disclosure"}},
		{tablet768, "project_id", nil}, // a short dropdown, clear of the bar
		{tablet768, "site_id", []string{"sticky bar"}},
	}
	d := h.SignIn(deepak, phone360)
	line := DocPath(expense.ClaimDocType, newClaim(d, "", "")) + "/lines/new"
	d.Open(line)
	d.WaitText(saveLine)
	// The disclosure keeps daisyUI's open animation and its rounded look.
	look := Eval[string](d, `(() => { const e = document.querySelector("details.disclosure"), c = getComputedStyle(e); return c.borderTopLeftRadius + " | " + c.transitionProperty + " | " + getComputedStyle(e, "::details-content").transitionProperty; })()`)
	if parts := strings.Split(look, " | "); len(parts) != 3 || parts[0] == "0px" || parts[1] != "grid-template-rows" || !strings.Contains(parts[2], "height") {
		t.Errorf("disclosure radius | transition | content transition = %q", look)
	}
	for _, c := range cases {
		d.width = c.width
		d.Open(line)
		d.WaitText(saveLine)
		settle(d)
		openMoreDetails(d) // both groups sit in the line's More details (UI v2 E7)
		openDisclosure(d, "Bill in company GSTIN")
		openDisclosure(d, "Project or site")
		openPickerOnTop(d, c.field, c.over...)
	}
}

// TestE2E_DisclosedPickerOnTop_Party: Payment terms under More details on the
// party wizard, the disclosed Picker outside the expense module.
func TestE2E_DisclosedPickerOnTop_Party(t *testing.T) {
	h := startPurchase(t)
	p := h.SignIn(admin, phone360)
	// At 768 px the carded form (UI v2 R10) puts the card's padding between
	// the field and the bar, so a one-option dropdown ends at the bar's edge:
	// two more terms make it long enough to reach over the bar.
	at := "2026-10-24T05:00:00.000Z"
	for _, code := range []string{"NET45", "NET60"} {
		if _, err := h.App.DB().SQL().ExecContext(context.Background(),
			`INSERT INTO payment_terms (code, name, created_at, updated_at) VALUES (?, ?, ?, ?)`, code, code+" days", at, at); err != nil {
			t.Fatal(err)
		}
	}
	for width, over := range map[int64][]string{phone320: {"top bar", "sticky bar"}, phone360: {"top bar", "sticky bar"}, tablet768: {"sticky bar"}} {
		p.width = width
		p.Open("/m/party/new")
		settle(p)
		openDisclosure(p, "More details · payment terms")
		openPickerOnTop(p, "default_payment_terms_id", over...)
	}
}

// TestE2E_PickerOnTop_PurchaseProject: the PO's Project is not inside a
// disclosure (no PO Picker is); it is the reference that a Picker outside one
// paints on top.
func TestE2E_PickerOnTop_PurchaseProject(t *testing.T) {
	h := startPurchase(t)
	p := h.SignIn(admin, phone360)
	for width, over := range map[int64][]string{phone360: {"top bar", "sticky bar"}, tablet768: nil} {
		p.width = width
		p.Open("/modules/purchase")
		p.Press("New purchase order")
		onStep(p, "Step 1 of 4")
		openPickerOnTop(p, "project_id", over...)
	}
}
