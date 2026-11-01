//go:build e2e

package gallery

import (
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// A-6 (UI v2 §8) on the sign-in page in Solo: at 1280 px the card is centred
// within 2 px, the password box fills at least 90 % of the card's content
// width and the eye toggle sits inside it; the app header is hidden; and at
// 320 px nothing scrolls sideways. Clicking the eye shows the password and
// swaps the glyph.
func TestSoloBrowser_SignInGeometry(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 1280, s.URL+"/_gallery/auth-signin"); err != nil {
		t.Fatal(err)
	}
	if got := value[float64](t, ctx, `(() => { const r = document.querySelector(".solo__card").getBoundingClientRect(); return Math.abs(r.left + r.width / 2 - document.documentElement.clientWidth / 2); })()`); got > 2 {
		t.Errorf("card centre is %v px from the viewport's centre, want ≤ 2", got)
	}
	if got := value[float64](t, ctx, `(() => { const b = document.querySelector(".solo__card .card-body"), s = getComputedStyle(b); return document.getElementById("f-signin-password").getBoundingClientRect().width / (b.clientWidth - parseFloat(s.paddingLeft) - parseFloat(s.paddingRight)); })()`); got < 0.9 {
		t.Errorf("password box is %v of the card's content width, want ≥ 0.9", got)
	}
	inside := `(() => { const i = document.getElementById("f-signin-password").getBoundingClientRect(), b = document.querySelector("[data-toggle-password]").getBoundingClientRect(), e = document.querySelector(".password__eye").getBoundingClientRect(); return b.width >= 44 && b.height >= 44 && b.left >= i.left && b.right <= i.right && b.top >= i.top && b.bottom <= i.bottom && e.left >= b.left && e.right <= b.right; })()`
	if !value[bool](t, ctx, inside) {
		t.Error("the 44 px eye toggle and its glyph are not inside the password box")
	}
	if got := value[string](t, ctx, `getComputedStyle(document.querySelector(".app-header")).display`); got != "none" {
		t.Errorf("app header display = %q, want none", got)
	}
	glyphs := `[document.getElementById("f-signin-password").type, getComputedStyle(document.querySelector(".password__show")).display, getComputedStyle(document.querySelector(".password__hide")).display].join(" ")`
	if got := value[string](t, ctx, glyphs); got != "password block none" {
		t.Errorf("before the click [type, eye, eye-off] = %q", got)
	}
	if err := chromedp.Do(ctx, chromedp.Click(chromedp.CSS("[data-toggle-password]"))); err != nil {
		t.Fatal(err)
	}
	if got := value[string](t, ctx, glyphs); got != "text none block" {
		t.Errorf("after the click [type, eye, eye-off] = %q", got)
	}
	if got := value[string](t, ctx, `document.querySelector("[data-toggle-password]").textContent`); got != "Hide password" {
		t.Errorf("toggle name after the click = %q", got)
	}

	if err := uibrowser.At(ctx, 320, s.URL+"/_gallery/auth-signin"); err != nil {
		t.Fatal(err)
	}
	probs, err := uibrowser.Overflow(ctx)
	if err != nil || len(probs) > 0 {
		t.Errorf("320 px: overflow %v, err %v", probs, err)
	}
}
