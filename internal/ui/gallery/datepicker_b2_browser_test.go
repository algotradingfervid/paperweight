//go:build e2e

package gallery

import (
	"encoding/json"
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
// box's date in every digit form, else on today (showPicker stubbed, as
// headless Chrome draws no picker; CD-351 (1)); when showPicker throws it
// focuses and clicks the date input, then gives focus back to the button; a
// picked date is written as dd-mm-yyyy and fires change, so the weekday echo
// runs.
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

		// CD-351 (1): every digit form the server accepts opens on the box's
		// date; d-Mon-yyyy, invalid and partial values open on today (an empty
		// value), over a stale one, without an exception.
		for _, c := range []struct{ typed, want string }{
			{"05-03-2026", "2026-03-05"},
			{"5-3-2026", "2026-03-05"},
			{"05/03/2026", "2026-03-05"},
			{"5/3/2026", "2026-03-05"},
			{"5.3.2026", "2026-03-05"},
			{" 27-10-2026 ", "2026-10-27"},
			{"5-Mar-2026", ""},
			{"31-02-2026", ""},
			{"05-03", ""},
			{"5-3-26", ""},
			{"05-03-20261", ""},
			{"5-3/2026", ""},
			{"5.3-2026", ""},
			{"", ""},
		} {
			got := value[[]string](t, ctx, `(() => { `+datePick+` let seen = "not called", errs = []; const onErr = function (e) { errs.push(String(e.message)); }; window.addEventListener("error", onErr); d.value = "2020-01-01"; d.showPicker = function () { seen = this.value; }; t.value = `+jsString(c.typed)+`; b.click(); window.removeEventListener("error", onErr); return [seen, errs.join("; ")]; })()`)
			if got[0] != c.want || got[1] != "" {
				t.Errorf("%d: typed %q: showPicker saw %q (errors %q), want %q", w, c.typed, got[0], got[1], c.want)
			}
		}
		got := value[string](t, ctx, `(() => { `+datePick+` let calls = []; d.addEventListener("focus", function () { calls.push("focus"); }, { once: true }); d.addEventListener("click", function () { calls.push("click"); }, { once: true }); d.showPicker = function () { throw new DOMException("no", "NotAllowedError"); }; b.click(); return calls.join(",") + (document.activeElement === b ? " back on the button" : " focus elsewhere"); })()`)
		if got != "focus,click back on the button" {
			t.Errorf("%d: when showPicker throws: %q, want the date input focused and clicked, then focus back on the button", w, got)
		}

		got = value[string](t, ctx, `(() => { `+datePick+` let changes = 0; t.addEventListener("change", function () { changes++; }); d.value = "2026-11-01"; d.dispatchEvent(new Event("change", { bubbles: true })); return t.value + " " + changes; })()`)
		if got != "01-11-2026 1" {
			t.Errorf("%d: after the pick, box and change count = %q, want \"01-11-2026 1\"", w, got)
		}
		until(t, ctx, "the weekday echo", `document.getElementById("f-date-msg").textContent.trim() === "Sunday, 1 November 2026"`)
	}
}

// jsString is s as a JavaScript string literal.
func jsString(s string) string {
	b, _ := json.Marshal(s) //nolint:errchkjson // a string always marshals
	return string(b)
}
