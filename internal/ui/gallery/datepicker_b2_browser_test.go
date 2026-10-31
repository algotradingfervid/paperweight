//go:build e2e

package gallery

import (
	"net/http/httptest"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// datePick is the fields page's DateInput sample: its text box, calendar
// button and hidden date input.
const datePick = `const t = document.getElementById("f-date"), b = document.querySelector('[data-date-pick][aria-controls="f-date"]'), d = document.querySelector('[data-date-picker="f-date"]');`

// TestDateInputBrowser_Picker (A-15, CD-336 (1)): Tab reaches the 44 px
// "Choose date" button from the text box; the button opens the picker on the
// box's date (showPicker stubbed, as headless Chrome draws no picker) and
// falls back to focusing the date input when showPicker throws; a picked
// date is written as dd-mm-yyyy and fires change, so the weekday echo runs.
func TestDateInputBrowser_Picker(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	for _, w := range []int64{360, 1280} {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/fields"); err != nil {
			t.Fatal(err)
		}
		act(t, ctx, "focus the box and press Tab", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-date").focus()`), chromedp.KeyEvent(kb.Tab))
		if got := value[string](t, ctx, `document.activeElement.getAttribute("aria-label") || document.activeElement.id`); got != "Choose date" {
			t.Errorf("%d: Tab from the box reaches %q, want the Choose date button", w, got)
		}
		if got := value[[]float64](t, ctx, `(() => { const r = document.activeElement.getBoundingClientRect(); return [r.width, r.height]; })()`); got[0] < 44 || got[1] < 44 {
			t.Errorf("%d: button is %v px, want at least 44 x 44", w, got)
		}
		if got := value[bool](t, ctx, `(() => { const b = document.querySelector('[data-date-pick][aria-controls="f-date"]').getBoundingClientRect(), t = document.getElementById("f-date").getBoundingClientRect(); return b.left >= t.left && b.right <= t.right + 0.5 && b.top >= t.top - 0.5 && b.bottom <= t.bottom + 0.5; })()`); !got {
			t.Errorf("%d: the button isn't inside the text box", w)
		}

		got := value[string](t, ctx, `(() => { `+datePick+` let seen = "none"; d.showPicker = function () { seen = this.value; }; t.value = "05-03-2026"; b.click(); return seen; })()`)
		if got != "2026-03-05" {
			t.Errorf("%d: showPicker saw %q, want 2026-03-05 (the box's date)", w, got)
		}
		got = value[string](t, ctx, `(() => { `+datePick+` d.showPicker = function () { throw new DOMException("no", "NotAllowedError"); }; b.click(); return document.activeElement === d ? "focused" : "not focused"; })()`)
		if got != "focused" {
			t.Errorf("%d: when showPicker throws, the date input is %s", w, got)
		}

		act(t, ctx, "pick 1 November 2026", chromedp.Evaluate[chromedp.Void](`(() => { `+datePick+` window.__b2changes = 0; t.addEventListener("change", function () { window.__b2changes++; }); d.value = "2026-11-01"; d.dispatchEvent(new Event("change", { bubbles: true })); })()`))
		if got := value[string](t, ctx, `document.getElementById("f-date").value`); got != "01-11-2026" {
			t.Errorf("%d: box = %q, want 01-11-2026", w, got)
		}
		if got := value[int](t, ctx, `window.__b2changes`); got != 1 {
			t.Errorf("%d: %d change events on the box, want 1", w, got)
		}
		until(t, ctx, "the weekday echo", `document.getElementById("f-date-msg").textContent.trim() === "Sunday, 1 November 2026"`)
	}
}
