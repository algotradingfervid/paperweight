//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"testing"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// TestE2E_Field_EchoKeepsSaveInPlace: HOOK-049 (CD-287). Pressing Save blurs
// the date, whose echo arrives before the button is released. The echo fills a
// line already reserved, so Save stays under the pointer and the click submits.
// fixtureEcho is the date field's echo of fixtureDate.
const fixtureEcho = "Saturday, 24 October 2026"

func TestE2E_Field_EchoKeepsSaveInPlace(t *testing.T) {
	h := Start(t)
	s := h.SignIn(suresh, phone360)
	s.Open("/doc/" + fixture.Receipt + "/new")
	s.Fill("receipt_date", fixtureDate)
	find := `[...document.querySelectorAll("button")].find((e) =>
    e.checkVisibility() && !e.disabled && e.textContent.replace(/\s+/g, " ").trim() === ` + strconv.Quote(saveContinue) + `)`
	s.Until("a button "+strconv.Quote(saveContinue), `(`+find+`) !== undefined`)
	msg := `document.getElementById("` + components.FieldID("receipt_date") + `-msg")`
	if text := Eval[string](s, msg+`.textContent`); text != "" {
		t.Fatalf("the date's message before the press = %q, want it empty", text)
	}
	// Save's centre, top and bottom, without the pressed button's own nudge
	// (daisyUI's .btn:active translate), which isn't a layout shift.
	save := `(() => {
  const m = ` + find + `;
  const r = m.getBoundingClientRect();
  const dy = parseFloat(getComputedStyle(m).translate.split(" ")[1]) || 0;
  return [r.x + r.width / 2, r.y + r.height / 2, r.top - dy, r.bottom - dy];
})()`
	Eval[bool](s, `(() => { (`+find+`).scrollIntoView({block: "center", behavior: "instant"}); return true; })()`)
	at := Eval[[]float64](s, save)
	echoMouse(s, chromedp.MousePressed, at[0], at[1])
	s.Until("the date's echo", msg+`?.textContent === `+strconv.Quote(fixtureEcho))
	moved := Eval[[]float64](s, save)
	if moved[2] != at[2] || moved[3] != at[3] {
		t.Errorf("Save moved while pressed: top %.1f bottom %.1f, then top %.1f bottom %.1f", at[2], at[3], moved[2], moved[3])
	}
	echoMouse(s, chromedp.MouseReleased, at[0], at[1])
	s.WaitText("Step 2 of 3")
}

// echoMouse sends one left-button mouse event at x, y.
func echoMouse(s *Session, typ input.DispatchMouseEventType, x, y float64) {
	s.t.Helper()
	s.Do(string(typ), chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: typ, X: x, Y: y, Button: input.MouseButtonLeft, ClickCount: 1})
		return err
	}))
}
