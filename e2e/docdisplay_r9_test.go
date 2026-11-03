//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// docDisplayWidths are the widths R9 is measured at.
var docDisplayWidths = []int64{320, 360, 390, 768, 1024, 1280}

// docSheetJS opens the ellipsis' sheet, lists what is wrong with it (a target
// under 44 px, a filled button) and closes it. It is empty when the page has
// no ellipsis showing.
const docSheetJS = `(() => {
  const open = [...document.querySelectorAll(".actionbar > [data-open-dialog]")].find((e) => e.checkVisibility());
  if (!open) return [];
  const out = [];
  const dlg = document.getElementById(open.getAttribute("data-open-dialog"));
  if (open.getBoundingClientRect().height < 44 || open.getBoundingClientRect().width < 44) out.push("the ellipsis is under 44 px");
  open.click();
  if (!dlg.open) return ["the ellipsis didn't open its sheet"];
  const items = [...dlg.querySelectorAll(".more-actions-items .btn")];
  if (!items.length) out.push("the sheet has no items");
  for (const b of items) {
    const r = b.getBoundingClientRect();
    if (r.height < 44) out.push(b.textContent.trim() + " is under 44 px in the sheet");
    if (getComputedStyle(b).backgroundColor !== "rgba(0, 0, 0, 0)") out.push(b.textContent.trim() + " is filled in the sheet");
    const [red, green] = getComputedStyle(b).color.match(/[0-9.]+/g).map(Number);
    if (b.matches(".btn--danger-quiet") !== red > green + 60) out.push(b.textContent.trim() + " is in the wrong colour: " + getComputedStyle(b).color);
  }
  dlg.close();
  return out;
})()`

// TestE2E_DocDisplayEveryDocument (UI review R9): every document page and
// Check page in the sweep, in every state each module's Screens visit, at
// 320 to 1280 px: uibrowser.DocDisplay's checks on real data, and the
// ellipsis' sheet opens with Close, Cancel and Print at 44 px, none filled.
func TestE2E_DocDisplayEveryDocument(t *testing.T) {
	h := Start(t)
	list := h.appScreens(h.prepareSweep())
	ctx := uibrowser.NewFor(t, sweepBrowser)
	opt := uibrowser.Options{Widths: docDisplayWidths, SignIn: h.sweepAs(), Page: func(ctx context.Context, _ screens.Screen) []string {
		bad, err := uibrowser.DocDisplay(ctx)
		if err != nil {
			return []string{err.Error()}
		}
		sheet, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](docSheetJS))
		if err != nil {
			return []string{err.Error()}
		}
		return append(bad, sheet...)
	}}
	types, states := map[string]bool{}, 0
	for _, s := range list {
		if s.Route != "/doc/{doctype}/{id}" && !strings.HasSuffix(s.URL, "/edit/check") {
			continue
		}
		for _, n := range []string{purchase.DocType, purchase.RequestDocType, goodsreceipt.DocType, expense.ClaimDocType, expense.AdvanceDocType, fixture.Order} {
			if strings.HasPrefix(s.URL, "/doc/"+n+"/") {
				types[n] = true
			}
		}
		states++
		uibrowser.Check(t, ctx, s, h.URL, opt)
	}
	if len(types) != 6 || states < 30 {
		t.Errorf("swept %d pages of %d DocTypes %v, want at least 30 of 6", states, len(types), types)
	}
}

// r9Print fetches what a phone sees of the print page at path.
func r9Print(s *Session) []string {
	return Eval[[]string](s, `(() => {
  const out = [];
  const m = document.querySelector('meta[name="viewport"]');
  if (!m || !m.content.includes("width=device-width")) out.push("no viewport meta");
  if (document.documentElement.scrollWidth > innerWidth) out.push("the page scrolls sideways: " + document.documentElement.scrollWidth + " in " + innerWidth);
  for (const b of document.querySelectorAll(".toolbar button")) {
    const r = b.getBoundingClientRect(), c = getComputedStyle(b);
    if (r.height < 44 || r.width < 44) out.push(b.id + " is " + r.width + " x " + r.height);
    if (c.borderTopStyle === "outset" || c.borderTopWidth === "2px") out.push(b.id + " is an unstyled button");
  }
  const logo = document.querySelector("img.logo");
  if (!logo) out.push("no logo");
  else {
    if (!logo.complete || logo.naturalWidth === 0) out.push("the logo didn't load");
    if (logo.getBoundingClientRect().right > innerWidth) out.push("the logo runs off the page");
  }
  return out;
})()`)
}

// TestE2E_PrintOnPhone (UI review R9, PO, GRN L9, EXP): the print page on a
// phone has a viewport, no sideways scroll, styled 44 px buttons and a logo
// that fits, with the brand logo too (no company logo set, CD-140). None of
// it reaches paper: under print media the page's own rules stand (no padding,
// a real table, no toolbar).
func TestE2E_PrintOnPhone(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	path := fmt.Sprintf("/print/%s/%d", fixture.Order, id)
	for _, w := range []int64{phone320, phone360, phone390} {
		s := h.SignIn(priya, w)
		s.Open(path)
		s.Until("the print page", `document.querySelector("#print-btn") !== null`)
		for _, p := range r9Print(s) {
			t.Errorf("%d px: %s", w, p)
		}
		s.Do("print media", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
			_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "print"})
			return err
		}))
		got := Eval[string](s, `[getComputedStyle(document.body).paddingTop, getComputedStyle(document.querySelector("table")).display, getComputedStyle(document.querySelector(".toolbar")).display].join(" ")`)
		if want := "0px table none"; got != want {
			t.Errorf("%d px, print media: %q, want %q", w, got, want)
		}
	}
}

// TestE2E_PrintLongValuesEverySize (R9 review): the real module print pages
// (PO, GRN, claim, advance) with long unbroken values in every free-text
// slot, at every supported width: no sideways scroll, and axe passes, so the
// lines table's own scroll box is focusable and labelled.
func TestE2E_PrintLongValuesEverySize(t *testing.T) {
	h := Start(t)
	s := h.prepareSweep()
	long := strings.ReplaceAll(uitest.LongCode60, " ", "")
	ctx := context.Background()
	for _, q := range []string{
		`UPDATE purchase_order SET vendor_quote_ref = ?1, terms = ?1 || ' ' || ?1 WHERE id = ?2`,
		`UPDATE goods_receipt SET remarks = ?1, vehicle_no = ?1 WHERE id = ?3`,
		`UPDATE expense_claim SET purpose = ?1 WHERE id = ?4`,
		`UPDATE employee_advance SET purpose = ?1 WHERE id = ?5`,
	} {
		if _, err := h.App.DB().SQL().ExecContext(ctx, q, long, s.po.order.Approved, s.grn.ids.Approved, s.exp[expense.ClaimDocType].Approved, s.exp[expense.AdvanceDocType].Approved); err != nil {
			t.Fatal(err)
		}
	}
	bctx := uibrowser.NewFor(t, sweepBrowser)
	opt := uibrowser.Options{Widths: screens.Widths, Overflow: true, Axe: true, SignIn: h.sweepAs()}
	for dt, id := range map[string]int64{
		purchase.DocType:       s.po.order.Approved,
		goodsreceipt.DocType:   s.grn.ids.Approved,
		expense.ClaimDocType:   s.exp[expense.ClaimDocType].Approved,
		expense.AdvanceDocType: s.exp[expense.AdvanceDocType].Approved,
	} {
		uibrowser.Check(t, bctx, screens.Screen{Name: "print " + dt, Route: "/print/{doctype}/{id}", URL: fmt.Sprintf("/print/%s/%d", dt, id), As: admin}, h.URL, opt)
	}
}

// TestE2E_DraftTimelineSameForEveryViewer (R9 review): a draft's timeline
// doesn't depend on who reads it, though APR's preview does: two readers see
// the Pending approval step of an Approvable DocType.
func TestE2E_DraftTimelineSameForEveryViewer(t *testing.T) {
	h := startPurchase(t)
	id := h.metroDraft(priya)
	const steps = `[...document.querySelectorAll(".lifecycle__label")].map((e) => e.textContent.trim().replace(/\s+/g, " ")).join(" > ")`
	const want = "Draft > Pending approval > Approved > Sent > Received"
	for _, who := range []string{ravi, meena} {
		s := h.SignIn(who, desktop)
		s.Open(DocPath(purchase.DocType, id))
		s.Until("the header", `document.querySelector(".lifecycle") !== null`)
		if got := Eval[string](s, steps); got != want {
			t.Errorf("%s sees %q, want %q", who, got, want)
		}
	}
}

// TestE2E_StaleWithdrawFromSheet (R9 review, CD-381): on a phone, Withdraw
// from the sheet at a stale version shows the conflict banner on the document
// page, with the sheet gone.
func TestE2E_StaleWithdrawFromSheet(t *testing.T) {
	h := Start(t)
	r := refPO(h)
	h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	a := h.SignIn(admin, phone360)
	a.Open(DocPath(fixture.Order, r.ID))
	a.Until("the page", `document.querySelector(".doc-head") !== null`)
	Eval[any](a, lowerVersions)
	a.Press("Withdraw")
	a.WaitText("Someone else changed ")
	wantConflict(t, a, "doc-alert")
	if open := Eval[bool](a, `!!document.querySelector("dialog[open]")`); open {
		t.Errorf("a sheet is still open\n%s", a.page())
	}
}
