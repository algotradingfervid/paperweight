//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// TestInfoTipBrowser_KeyboardTapAndTarget checks the fields page's info tip at
// 390 px: its "i" is a 44 × 44 target, Enter on it opens the tip and shows
// the body the input's aria-describedby names, and a tap opens it too (A-11).
func TestInfoTipBrowser_KeyboardTapAndTarget(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 390, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	const sum = `document.querySelector("#f-code-tip").previousElementSibling.querySelector("summary")`
	eval := func(what, js string) string {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		got, err := chromedp.Run(sctx, chromedp.Evaluate[string](js))
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return got
	}
	do := func(what string, a ...chromedp.Action[chromedp.Void]) {
		t.Helper()
		sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
		defer cancel()
		if err := chromedp.Do(sctx, a...); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	// state is "open|closed" and whether the body takes space.
	state := `(() => { const b = document.getElementById("f-code-tip");
		return (b.previousElementSibling.open ? "open" : "closed") + " " + (b.getBoundingClientRect().height > 0 ? "shown" : "hidden"); })()`

	if got := eval("size", `(() => { const r = `+sum+`.getBoundingClientRect(); return Math.round(r.width) + "x" + Math.round(r.height); })()`); got != "44x44" {
		t.Errorf("tip button = %s, want 44x44", got)
	}
	if got := eval("describedby", `document.getElementById("f-code").getAttribute("aria-describedby")`); got != "f-code-tip f-code-msg" {
		t.Errorf("aria-describedby = %q, want f-code-tip f-code-msg", got)
	}
	if got := eval("start", state); got != "closed hidden" {
		t.Fatalf("before: %s, want closed hidden", got)
	}

	do("Enter", chromedp.Evaluate[chromedp.Void](sum+`.focus()`), chromedp.KeyEvent(kb.Enter))
	if got := eval("after Enter", state); got != "open shown" {
		t.Errorf("after Enter: %s, want open shown", got)
	}
	do("Enter again", chromedp.KeyEvent(kb.Enter))
	if got := eval("after second Enter", state); got != "closed hidden" {
		t.Errorf("after second Enter: %s, want closed hidden", got)
	}

	do("tap", chromedp.Click(chromedp.CSS(`.field__top:has(#f-code-tip) summary`)))
	if got := eval("after tap", state); got != "open shown" {
		t.Errorf("after tap: %s, want open shown", got)
	}

	// A choice group's "i" shares the legend's row.
	if got := eval("radio tip row", `(() => {
		const f = document.getElementById("f-delivery-tip").parentElement;
		const l = f.querySelector("legend").getBoundingClientRect(), s = f.querySelector("summary").getBoundingClientRect();
		return Math.abs((l.top + l.bottom) / 2 - (s.top + s.bottom) / 2) < 12 && s.left > l.left ? "beside" : "below";
	})()`); got != "beside" {
		t.Errorf("radio group tip is %s the legend, want beside", got)
	}
}

// TestFieldErrorBrowser_Look checks the error look of §5.5 item 6: the message
// in danger-800 at weight 550 and a 2 px danger-600 border on the control.
func TestFieldErrorBrowser_Look(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 390, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	sctx, cancel := context.WithTimeout(ctx, uibrowser.StepTimeout)
	defer cancel()
	const js = `(() => {
		const m = getComputedStyle(document.getElementById("e-code-msg")), c = getComputedStyle(document.getElementById("e-code"));
		return [m.color, m.fontWeight, c.borderTopWidth, c.borderTopColor].join(" | ");
	})()`
	got, err := chromedp.Run(sctx, chromedp.Evaluate[string](js))
	if err != nil {
		t.Fatalf("read error look: %v", err)
	}
	if want := "rgb(143, 31, 24) | 550 | 2px | rgb(208, 52, 42)"; got != want {
		t.Errorf("error look = %s, want %s", got, want)
	}
}
