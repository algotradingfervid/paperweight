//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// reducedMotion sets the tab's prefers-reduced-motion to value ("reduce" or
// "no-preference").
func reducedMotion(s *Session, value string) {
	s.t.Helper()
	s.Do("prefers-reduced-motion "+value, chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: value}}})
		return err
	}))
}

// TestE2E_SubmitSuccessBlock: A-25 (UI v2 §5.13 item 2, CD-372). After
// Submit the document page shows the success block with role="status" and
// the submit text; under prefers-reduced-motion: reduce its tick runs no
// animation, and without it the tick draws.
func TestE2E_SubmitSuccessBlock(t *testing.T) {
	for _, c := range []struct {
		media    string
		animated bool
	}{{"reduce", false}, {"no-preference", true}} {
		t.Run(c.media, func(t *testing.T) {
			h := Start(t)
			id := claimDraft(h).ID
			d := h.SignIn(deepak, desktop)
			reducedMotion(d, c.media)
			d.Open(checkStep(id))
			d.Press(sendApproval)
			d.Until("the success block", `document.querySelector('#main .success-block[role="status"]') !== null`)
			if got := Eval[string](d, `document.querySelector("#main .success-block__text").textContent.trim()`); got != claimSubmitted {
				t.Errorf("success text = %q, want %q", got, claimSubmitted)
			}
			if got := Eval[string](d, `document.querySelector("#main .success-block h2").textContent.trim()`); got != "Submitted for approval" {
				t.Errorf("success heading = %q", got)
			}
			if n := Eval[int](d, `document.querySelectorAll("#main .banner--success").length`); n != 0 {
				t.Errorf("%d success banners beside the block, want none", n)
			}
			names := Eval[string](d, `[...document.querySelectorAll("#main .success-block__tick circle, #main .success-block__tick path")].map((e) => getComputedStyle(e).animationName).join(",")`)
			if want := map[bool]string{true: "success-circle,success-check", false: "none,none"}[c.animated]; names != want {
				t.Errorf("tick animation names = %q, want %q", names, want)
			}
			if !c.animated {
				if n := Eval[int](d, `document.querySelector("#main .success-block__tick").getAnimations({subtree: true}).length`); n != 0 {
					t.Errorf("%d tick animations under reduced motion, want none", n)
				}
			}
		})
	}
}

// claimSubmitted is the submit text for Deepak's claim.
const claimSubmitted = "It went to Priya Shah or Director. You'll see it in My requests until it's decided."

// blockIs fails unless the page's success block has this heading and line.
func blockIs(s *Session, heading, line string) {
	s.t.Helper()
	s.Until("the success block "+heading, `document.querySelector('#main .success-block[role="status"] h2')?.textContent.trim() === `+strconv.Quote(heading))
	if got := Eval[string](s, `document.querySelector("#main .success-block__text").textContent.trim()`); got != line {
		s.t.Errorf("%s: line = %q, want %q", heading, got, line)
	}
	if n := Eval[int](s, `document.querySelectorAll("#main .banner--success").length`); n != 0 {
		s.t.Errorf("%s: %d success banners beside the block, want none", heading, n)
	}
	noOverflow(s, heading)
}

// TestE2E_ApproveAndSentSuccessBlock (R9, §5.13 item 2) on real pages at
// 360 px: Approve shows the block with Next request, and Mark as sent shows
// it with the vendor; a reload shows neither, because the flash is taken once.
func TestE2E_ApproveAndSentSuccessBlock(t *testing.T) {
	h := startPurchase(t)
	id := h.metroDraft(priya)
	r0 := h.Do(priya, purchase.DocType, id, doctype.ActionSubmit, doc.ActionInput{Version: Scalar[int](h, `SELECT version FROM purchase_order WHERE id = ?`, id)})
	if r0.State == doctype.StatePending {
		r := h.SignIn(ravi, phone360)
		r.Open(DocPath(purchase.DocType, id))
		r.Press("Approve")
		blockIs(r, "Approved", firstNumber+" has been numbered.")
		if got := Eval[int](r, `document.querySelectorAll('#main .success-block a').length`); got == 0 {
			t.Error("no follow-ups in the block")
		}
		r.Do("reload", chromedp.Reload())
		r.Until("the page is back", `document.querySelector(".doc-head") !== null`)
		if n := Eval[int](r, `document.querySelectorAll(".success-block").length`); n != 0 {
			t.Errorf("after a reload: %d blocks, want none", n)
		}
	}
	p := h.SignIn(priya, phone360)
	p.Open(DocPath(purchase.DocType, id))
	p.Press("Send to vendor")
	p.WaitText("Record how it was sent")
	clickOn(p, `input[name=sent_via][value=email]`)
	p.Press("Mark as sent")
	blockIs(p, "Sent to the vendor", firstNumber+" marked as sent. It went to Shree Steel Traders.")
	p.Do("reload", chromedp.Reload())
	p.Until("the page is back", `document.querySelector(".doc-head") !== null`)
	if n := Eval[int](p, `document.querySelectorAll(".success-block").length`); n != 0 {
		t.Errorf("after a reload: %d blocks, want none", n)
	}
}

// TestE2E_MoreSheetKeyboard (R9, §5.13 item 7): on a phone the footer's
// ellipsis opens the sheet, Esc closes it and focus returns to the ellipsis;
// Cancel is reachable from it.
func TestE2E_MoreSheetKeyboard(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	p := h.SignIn(ravi, phone360)
	p.Open(DocPath(fixture.Order, id))
	clickOn(p, ".actionbar > [data-open-dialog]")
	p.Until("the sheet opens", `document.querySelector("#doc-more")?.open === true`)
	p.Keys(kb.Escape)
	p.Until("the sheet closes", `document.querySelector("#doc-more")?.open === false`)
	p.Until("focus is back on the ellipsis", `document.activeElement?.matches(".actionbar > [data-open-dialog]") === true`)
	p.Press("Cancel")
	p.Until("the cancel page", `location.pathname.endsWith("/cancel")`)
}
