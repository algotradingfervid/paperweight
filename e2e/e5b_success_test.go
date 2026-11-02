//go:build e2e

package e2e

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
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
const claimSubmitted = "Submitted for approval to Priya Shah or Director."
