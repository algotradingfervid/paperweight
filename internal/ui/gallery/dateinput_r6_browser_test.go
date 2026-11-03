//go:build e2e

package gallery

import (
	"context"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// r6Widths are the phone widths the date and tap tests run at (UI review R6).
var r6Widths = []int64{320, 390}

// TestDateInputBrowser_DigitsFormat (CD-387 (9), shell M13, EXP HIGH): digits
// take their dashes as typed, with the caret after the same digit; a paste
// does too; a typed dash, a slash or a point is left alone, so every form the
// server reads still works; a delete beside a dash removes the digit past it,
// and the dash left at the end goes with one press. The server stays the
// parser: a date it refuses is formatted, then refused.
func TestDateInputBrowser_DigitsFormat(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, w := range r6Widths {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			name, start string
			caret       int
			keys, paste string
			want        string
		}{
			{"two digits take no dash yet", "", 0, "05", "", "05|"},
			{"the third digit brings the dash", "", 0, "051", "", "05-1|"},
			{"the fourth", "", 0, "0511", "", "05-11|"},
			{"the fifth brings the second dash", "", 0, "05112", "", "05-11-2|"},
			{"all eight", "", 0, "05112026", "", "05-11-2026|"},
			{"typed with dashes", "", 0, "05-11-2026", "", "05-11-2026|"},
			{"one digit day and month with dashes", "", 0, "2-11-2026", "", "2-11-2026|"},
			{"one digit month after a two digit day", "", 0, "05-1-2026", "", "05-1-2026|"},
			{"a year typed first stays", "", 0, "2026-10-24", "", "2026-10-24|"},
			{"digits past a month that can't be one stay", "", 0, "0520", "", "0520|"},
			{"a year that reads as a day and month", "", 0, "2010-04-01", "", "2010-04-01|"},
			{"and part way", "", 0, "2010-04-", "", "2010-04-|"},
			{"a day and month with a full year stay", "", 0, "20-10-2026", "", "20-10-2026|"},
			{"slashes", "", 0, "02/11/2026", "", "02/11/2026|"},
			{"points", "", 0, "2.11.2026", "", "2.11.2026|"},
			{"a month name", "", 0, "5-Mar-2026", "", "5-Mar-2026|"},
			{"a typed dash stays until the next digit", "", 0, "05-", "", "05-|"},
			{"past eight digits nothing moves", "", 0, "051120261", "", "05-11-20261|"},
			{"a digit typed in the middle", "05-12", 1, "1", "", "01|5-12"},
			{"paste digits", "", 0, "", "05112026", "05-11-2026|"},
			{"paste digits with spaces round them", "", 0, "", " 05112026 ", "05-11-2026|"},
			{"paste a year first kept", "", 0, "", "2026-10-24", "2026-10-24|"},
			{"paste past eight digits kept", "", 0, "", "051120261", "051120261|"},
			{"paste slashes kept", "", 0, "", "02/11/2026", "02/11/2026|"},
			{"paste dashes kept", "", 0, "", "2-11-2026", "2-11-2026|"},
			{"paste words kept", "", 0, "", "tomorrow", "tomorrow|"},
			{"backspace after a dash removes the digit before it", "05-11-2026", 3, kb.Backspace, "", "0|-11-2026"},
			{"backspace after the second dash", "05-11-2026", 6, kb.Backspace, "", "05-1|-2026"},
			{"delete before a dash removes the digit past it", "05-11-2026", 2, kb.Delete, "", "05-|1-2026"},
			{"backspace in a digit run", "05-11-2026", 8, kb.Backspace, "", "05-11-2|26"},
			{"backspace a dash left at the end", "05-", 3, kb.Backspace, "", "05|"},
			{"delete a dash at the end", "05-", 2, kb.Delete, "", "05|"},
			{"retype a day after backspacing it", "0-11-2026", 1, "6", "", "06|-11-2026"},
			{"delete all of it from the end", "05-11-2026", 10, strings.Repeat(kb.Backspace, 10), "", "|"},
		} {
			steps := []chromedp.Action[chromedp.Void]{setField("f-date", c.start, c.caret)}
			if c.paste != "" {
				steps = append(steps, paste(c.paste))
			} else {
				steps = append(steps, chromedp.KeyEvent(c.keys))
			}
			act(t, ctx, c.name, steps...)
			if got := value[string](t, ctx, caretAt("f-date")); got != c.want {
				t.Errorf("%d: %s: got %q, want %q", w, c.name, got, c.want)
			}
		}

		// A key press over a selection holding a dash regroups what's left and
		// never drops a digit; a selection of digits alone keeps its dashes,
		// so a month can be retyped in its place.
		for _, c := range []struct {
			name string
			a, b int
			keys string
			want string
		}{
			{"digits replace day end to month", 1, 6, "7", "07|-20-26"},
			{"backspace over a dash and digits", 1, 4, kb.Backspace, "0|1-20-26"},
			{"delete over the whole middle", 2, 6, kb.Delete, "05|-20-26"},
			{"a digit over one dash, nine digits kept", 2, 3, "3", "053|11-2026"},
			{"digits alone keep the dashes", 3, 5, kb.Backspace, "05-|-2026"},
			{"over everything", 0, 10, "2", "2|"},
		} {
			act(t, ctx, c.name, selectIn("f-date", "05-11-2026", c.a, c.b), chromedp.KeyEvent(c.keys))
			if got := value[string](t, ctx, caretAt("f-date")); got != c.want {
				t.Errorf("%d: %s: got %q, want %q", w, c.name, got, c.want)
			}
		}
		// A real paste fires beforeinput over the selection; it keeps its form.
		act(t, ctx, "paste over all", selectIn("f-date", "05-11-2026", 0, 10), chromedp.Evaluate[chromedp.Void](`document.activeElement.dispatchEvent(new InputEvent("beforeinput", { bubbles: true, cancelable: true, inputType: "insertFromPaste" }))`), paste("2026-10-24"))
		if got := value[string](t, ctx, caretAt("f-date")); got != "2026-10-24|" {
			t.Errorf("%d: paste over a dated box: got %q", w, got)
		}

		// Every form NUM-078 accepts, typed key by key, reaches the server as
		// typed and comes back as dd-mm-yyyy.
		for _, typed := range []string{"01042025", "01-04-2025", "1-4-2025", "01/04/2025", "01.04.2025", "2025-04-01", "01-Apr-2025", "01-apr-2025"} {
			act(t, ctx, "type "+typed, setField("f-date", "", 0), chromedp.KeyEvent(typed), chromedp.KeyEvent(kb.Tab))
			until(t, ctx, typed+" read", `document.getElementById("f-date").value === "01-04-2025" && document.getElementById("f-date-msg").textContent.trim() === "Tuesday, 1 April 2025"`)
		}

		// Digits leave a value the server reads: Tab sends it, the echo names
		// the day and the box stays valid.
		act(t, ctx, "type and Tab", setField("f-date", "", 0), chromedp.KeyEvent("05112026"), chromedp.KeyEvent(kb.Tab))
		until(t, ctx, "the echo of typed digits", `document.getElementById("f-date-msg").textContent.trim() === "Thursday, 5 November 2026"`)
		if got := value[string](t, ctx, `document.getElementById("f-date").getAttribute("aria-invalid") || "valid"`); got != "valid" {
			t.Errorf("%d: aria-invalid = %q", w, got)
		}
		act(t, ctx, "type a bad date and Tab", setField("f-date", "", 0), chromedp.KeyEvent("31022026"), chromedp.KeyEvent(kb.Tab))
		until(t, ctx, "the server's refusal", `document.getElementById("f-date-msg").textContent.includes("31-02-2026 is not a valid date.")`)
	}
}

// TestDateInputBrowser_ChipsPressed (UI v2 §5.7, EXP LOW, GRN M10): a chip
// writes its date and shows as chosen, with aria-pressed true, for a chip
// tap, a typed date in any form and a calendar pick, and clears when the
// date stops matching. The chosen look reads at 4.5:1.
func TestDateInputBrowser_ChipsPressed(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	const today, yesterday = `document.querySelector('[data-set-date="24-10-2026"][aria-controls="f-date"]')`, `document.querySelector('[data-set-date="23-10-2026"][aria-controls="f-date"]')`
	pressed := `[` + today + `.getAttribute("aria-pressed"), ` + yesterday + `.getAttribute("aria-pressed")].join(" ")`
	for _, w := range r6Widths {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		act(t, ctx, "tap Yesterday", chromedp.Evaluate[chromedp.Void](yesterday+`.click()`))
		if got := value[string](t, ctx, `document.getElementById("f-date").value + " " + `+pressed); got != "23-10-2026 false true" {
			t.Errorf("%d: after Yesterday: %q", w, got)
		}
		until(t, ctx, "the chip's colours to settle", yesterday+`.getAnimations().length === 0`)
		look := func(chip string) string { return `getComputedStyle(` + chip + `).backgroundColor` }
		if on, off := value[string](t, ctx, look(yesterday)), value[string](t, ctx, look(today)); on == off {
			t.Errorf("%d: the chosen chip looks like the other: %s", w, on)
		}
		if got := value[float64](t, ctx, contrastOf(yesterday, yesterday)); got < 4.5 {
			t.Errorf("%d: the chosen chip's contrast = %.2f, want at least 4.5", w, got)
		}
		for _, c := range []struct{ name, typed, want string }{
			{"digits", "24102026", "true false"},
			{"a form", "24-10-2026", "true false"},
			{"one form", "24/10/2026", "true false"},
			{"yesterday", "23.10.2026", "false true"},
			{"another day", "22102026", "false false"},
		} {
			act(t, ctx, "type "+c.typed, setField("f-date", "", 0), chromedp.KeyEvent(c.typed))
			if got := value[string](t, ctx, pressed); got != c.want {
				t.Errorf("%d: %s: pressed = %q, want %q", w, c.name, got, c.want)
			}
		}
		act(t, ctx, "type a month name", setField("f-date", "", 0), chromedp.KeyEvent("24-Oct-2026"), chromedp.KeyEvent(kb.Tab))
		until(t, ctx, "the writeback to press Today", `document.getElementById("f-date").value === "24-10-2026" && `+pressed+` === "true false"`)
		act(t, ctx, "pick today", chromedp.Evaluate[chromedp.Void](`(() => { const d = document.querySelector('[data-date-picker="f-date"]'); d.value = "2026-10-24"; d.dispatchEvent(new Event("change", { bubbles: true })); })()`))
		if got := value[string](t, ctx, pressed); got != "true false" {
			t.Errorf("%d: after a pick: pressed = %q", w, got)
		}
	}
}

// TestNumberInputBrowser_PrefixReadable (shell L2): "₹" is at least the 13 px
// minimum and reads at 4.5:1 on the input.
func TestNumberInputBrowser_PrefixReadable(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, w := range r6Widths {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{"f-amount", "f-rate"} {
			pre, box := `document.getElementById("`+id+`").parentElement.querySelector(".field__pre")`, `document.getElementById("`+id+`")`
			if got := value[float64](t, ctx, `parseFloat(getComputedStyle(`+pre+`).fontSize)`); got < 13 {
				t.Errorf("%d: %s: prefix is %v px, want at least 13", w, id, got)
			}
			if got := value[float64](t, ctx, contrastOf(pre, box)); got < 4.5 {
				t.Errorf("%d: %s: prefix contrast = %.2f, want at least 4.5", w, id, got)
			}
		}
	}
}

// contrastOf is the script giving the WCAG contrast of fg's text colour on
// bg's background colour, both read through a canvas so any colour space
// comes back as sRGB bytes.
func contrastOf(fg, bg string) string {
	return `(() => {
  const c = document.createElement("canvas").getContext("2d", { willReadFrequently: true });
  const lum = (css) => {
    c.fillStyle = css; c.fillRect(0, 0, 1, 1);
    const [r, g, b] = c.getImageData(0, 0, 1, 1).data;
    const f = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
    return 0.2126 * f(r) + 0.7152 * f(g) + 0.0722 * f(b);
  };
  const a = lum(getComputedStyle(` + fg + `).color), b = lum(getComputedStyle(` + bg + `).backgroundColor);
  return (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
})()`
}

// tap is a finger's tap on input id, a fraction of the way across it, after
// scrolling it clear of the sticky bars.
func tap(t *testing.T, ctx context.Context, id string, across float64) {
	t.Helper()
	at := value[[]float64](t, ctx, `(() => { const e = document.getElementById("`+id+`"); e.scrollIntoView({block: "center", behavior: "instant"}); const r = e.getBoundingClientRect(); return [r.left + r.width * `+strconv.FormatFloat(across, 'f', 2, 64)+`, r.top + r.height / 2]; })()`)
	act(t, ctx, "tap "+id, chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
		_, err := cdp.Call(ctx, tg, input.SynthesizeTapGesture, input.SynthesizeTapGestureParams{X: at[0], Y: at[1]})
		return err
	}))
}

// TestNumberInputBrowser_TapSelects (CD-387 (8), GRN H1): on a touch screen,
// the first tap on a prefilled Qty, Rate, Money or date selects it wherever the
// finger lands, so the first digit replaces it (10 never becomes 100); a second
// tap, anywhere, places the caret, so the next digit goes into the value.
// Percent keeps the browser's caret.
func TestNumberInputBrowser_TapSelects(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	selected := func(id string) string {
		return `(() => { const e = document.getElementById("` + id + `"); return document.activeElement === e && e.selectionStart === 0 && e.selectionEnd === e.value.length && e.value !== ""; })()`
	}
	for _, w := range r6Widths {
		for _, c := range []struct {
			id, over, want string
		}{
			{"f-qty", "10", "10"},
			{"f-received", "10", "10"},
			{"f-amount", "10", "10"},
			{"f-rate", "10", "10"},
			{"f-date", "05112026", "05-11-2026"},
		} {
			for _, across := range []float64{0.15, 0.45, 0.75} {
				if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
					t.Fatal(err)
				}
				at := value[string](t, ctx, `document.getElementById("`+c.id+`").value`)
				tap(t, ctx, c.id, across)
				until(t, ctx, c.id+" selected", selected(c.id))
				time.Sleep(time.Second) // past the double-tap window, which selects a word
				tap(t, ctx, c.id, across)
				until(t, ctx, c.id+" caret", `document.getElementById("`+c.id+`").selectionStart === document.getElementById("`+c.id+`").selectionEnd`)
				act(t, ctx, "type into "+c.id, chromedp.KeyEvent("1"))
				if got := value[string](t, ctx, `document.getElementById("`+c.id+`").value`); got == "1" {
					t.Errorf("%d: %s at %.2f: typing after a second tap gave %q from %q, want it kept", w, c.id, across, got, at)
				}
			}
			if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
				t.Fatal(err)
			}
			tap(t, ctx, c.id, 0.3)
			until(t, ctx, c.id+" selected again", selected(c.id))
			act(t, ctx, "type over "+c.id, chromedp.KeyEvent(c.over))
			if got := value[string](t, ctx, `document.getElementById("`+c.id+`").value`); got != c.want {
				t.Errorf("%d: %s: typing %s over the value gave %q", w, c.id, c.over, got)
			}
		}
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		act(t, ctx, "prefill the percent", setField("f-discount", "12.5", 0), chromedp.Evaluate[chromedp.Void](`document.activeElement.blur()`))
		tap(t, ctx, "f-discount", 0.3)
		until(t, ctx, "the percent focused", `document.activeElement.id === "f-discount"`)
		if value[bool](t, ctx, selected("f-discount")) {
			t.Errorf("%d: a tap selected the percent", w)
		}
		// Nor does a tap on a selected Percent take the selection away.
		act(t, ctx, "select the percent", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-discount").select()`))
		time.Sleep(time.Second)
		tap(t, ctx, "f-discount", 0.3)
		time.Sleep(300 * time.Millisecond)
		if !value[bool](t, ctx, selected("f-discount")) {
			t.Errorf("%d: a tap collapsed the selected percent", w)
		}
	}
}

// TestNumberInputBrowser_ScrollKeepsSelection (CD-387 (8), GRN H1): a finger
// that scrolls the page from a selected field, up or down, leaves it selected,
// so the first digit still replaces the value (never 51,00,000).
func TestNumberInputBrowser_ScrollKeepsSelection(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, id := range []string{"f-qty", "f-amount", "f-date"} {
		for _, dist := range []float64{-150, 150} {
			if err := uibrowser.At(ctx, 390, s.URL+"/_gallery/fields"); err != nil {
				t.Fatal(err)
			}
			tap(t, ctx, id, 0.3)
			until(t, ctx, id+" selected", `(() => { const e = document.getElementById("`+id+`"); return e.value !== "" && e.selectionEnd - e.selectionStart === e.value.length; })()`)
			time.Sleep(500 * time.Millisecond) // past the double-tap window
			at := value[[]float64](t, ctx, `(() => { const r = document.getElementById("`+id+`").getBoundingClientRect(); return [r.left + r.width * 0.3, r.top + r.height / 2, scrollY]; })()`)
			act(t, ctx, "scroll from "+id, chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
				_, err := cdp.Call(ctx, tg, input.SynthesizeScrollGesture, input.SynthesizeScrollGestureParams{X: at[0], Y: at[1], YDistance: dist, GestureSourceType: input.GestureSourceTypeTouch})
				return err
			}))
			if got := value[float64](t, ctx, `scrollY`); got == at[2] {
				t.Fatalf("%s %v: the page didn't scroll, so this proves nothing", id, dist)
			}
			if got := value[string](t, ctx, caretAt(id)); got != "|" {
				t.Errorf("%s %v: after the scroll the field shows %q, want all of it selected", id, dist, got)
			}
			act(t, ctx, "type 5", chromedp.KeyEvent("5"))
			if got := value[string](t, ctx, `document.getElementById("`+id+`").value`); got != "5" {
				t.Errorf("%s %v: typing 5 gave %q", id, dist, got)
			}
		}
	}
}

// TestNumberInputBrowser_DragKeepsPart: a mouse drag over part of an unfocused
// quantity keeps that part selected; the click that ends it isn't a tap.
func TestNumberInputBrowser_DragKeepsPart(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 1280, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	at := value[[]float64](t, ctx, `(() => { const e = document.getElementById("f-qty"); e.scrollIntoView({block: "center", behavior: "instant"}); const r = e.getBoundingClientRect(); return [r.right - 16, r.right - 42, r.top + r.height / 2]; })()`)
	mouse := func(typ input.DispatchMouseEventType, x float64, buttons int64) chromedp.Action[chromedp.Void] {
		return chromedp.Func(func(ctx context.Context, tg *chromedp.Target) error {
			_, err := cdp.Call(ctx, tg, input.DispatchMouseEvent, input.DispatchMouseEventParams{Type: typ, X: x, Y: at[2], Button: input.MouseButtonLeft, Buttons: buttons, ClickCount: 1})
			return err
		})
	}
	act(t, ctx, "drag", mouse(chromedp.MousePressed, at[0], 1), mouse(chromedp.MouseMoved, at[1], 1), mouse(chromedp.MouseReleased, at[1], 0))
	got := value[[]int](t, ctx, `(() => { const e = document.getElementById("f-qty"); return [e.selectionStart, e.selectionEnd, e.value.length]; })()`)
	if got[0] == got[1] || got[1]-got[0] == got[2] {
		t.Errorf("after a drag: selection %d to %d of %d, want part of the value", got[0], got[1], got[2])
	}
}
