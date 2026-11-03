//go:build e2e

package gallery

import (
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	d1Active = `(() => { const id = document.querySelector("#f-vendor").getAttribute("aria-activedescendant"); const o = id && document.getElementById(id); return o ? o.dataset.value + "|" + o.getAttribute("aria-selected") : ""; })()`
	d1Values = `Array.from(document.querySelectorAll("#f-vendor-list [role=option][data-value]")).map(function (o) { return o.dataset.value; }).join(",")`
	d1Heads  = `Array.from(document.querySelectorAll("#f-vendor-list .picker__heading")).map(function (h) { return h.textContent.trim(); }).join(",")`
	d1Marks  = `Array.from(document.querySelectorAll("#f-vendor-list mark")).map(function (m) { return m.textContent; }).join(",")`
	d1Idle   = ` && document.querySelector(".htmx-request") === null`
)

// TestPickerD1_KeyboardAcrossSections (UI v2 §5.8, A-17): the open picker
// lists Recent first, then the search's rows without the recent ones; the
// arrows walk both groups in order and wrap; typing keeps only matching
// recent picks with the text marked; Enter picks. On a phone the open
// search fills the viewport.
func TestPickerD1_KeyboardAcrossSections(t *testing.T) {
	for _, w := range []int{1440, 390} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			s := httptest.NewServer(Handler())
			t.Cleanup(s.Close)
			ctx := uibrowser.New(t)
			openAt(t, ctx, w, s.URL+"/_gallery/picker-recent")

			// Both icons paint above the input (an icon under its background is
			// invisible). They ignore the pointer, so hit-testing turns that off
			// for the moment of the check.
			for _, sel := range []string{".picker__icon", ".picker__chevron"} {
				if !value[bool](t, ctx, `(() => { const i = document.querySelector("#f-vendor").closest(".picker__box").querySelector("`+sel+`"); const r = i.querySelector("svg").getBoundingClientRect(); i.style.pointerEvents = "auto"; const e = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2); i.style.pointerEvents = ""; return e !== null && i.contains(e); })()`) {
					t.Errorf("%s is covered by the input", sel)
				}
			}
			act(t, ctx, "open", chromedp.Click(chromedp.CSS("#f-vendor")))
			until(t, ctx, "the rows", `document.querySelector("#f-vendor-list .picker__group") !== null`+d1Idle)
			if got := value[string](t, ctx, d1Heads); got != "Recent,All" {
				t.Errorf("headings = %q, want Recent,All", got)
			}
			if got := value[string](t, ctx, d1Values); got != "4,1,98,99,2,3" {
				t.Errorf("options = %q, want the recent 4,1,98,99 then 2,3", got)
			}
			if w == 390 && !value[bool](t, ctx, `(() => { const r = document.querySelector("[data-picker-search][data-open]").getBoundingClientRect(); return r.top <= 0 && r.left <= 0 && r.width >= innerWidth && r.height >= innerHeight; })()`) {
				t.Error("the open search does not fill the phone's viewport")
			}

			for i, want := range []string{"4", "1", "98", "99", "2", "3", "4"} {
				act(t, ctx, "ArrowDown", chromedp.KeyEvent(kb.ArrowDown))
				if got := value[string](t, ctx, d1Active); got != want+"|true" {
					t.Errorf("ArrowDown %d: active = %q, want %s|true", i+1, got, want)
				}
			}
			act(t, ctx, "ArrowUp", chromedp.KeyEvent(kb.ArrowUp))
			if got := value[string](t, ctx, d1Active); got != "3|true" {
				t.Errorf("ArrowUp from the first: active = %q, want 3|true (wrapped)", got)
			}

			// A hostile name is text, also when marked, and the search's
			// "No matches" row hides under the matching recent pick.
			act(t, ctx, `type "img"`, chromedp.SendKeys(chromedp.CSS("#f-vendor"), "img"))
			until(t, ctx, "rows for img", `document.querySelector('#f-vendor-list [data-value="4"]') === null && document.querySelector('#f-vendor-list [data-value="99"]') !== null`+d1Idle)
			if got := value[int](t, ctx, `document.querySelectorAll(".picker img").length`); got != 0 {
				t.Errorf("img: %d img elements in the picker, want 0", got)
			}
			if got := value[string](t, ctx, `document.querySelector('#f-vendor-list [data-value="99"] .picker__name').textContent`); got != hostileName {
				t.Errorf("img: hostile name shows %q, want it literally", got)
			}
			if got := value[string](t, ctx, d1Marks); got != "img" {
				t.Errorf("img: marks = %q, want img", got)
			}
			if got := value[string](t, ctx, `Array.from(document.querySelectorAll("#f-vendor-list [role=option]")).filter(function (o) { return o.getClientRects().length > 0; }).map(function (o) { return o.dataset.value || o.textContent; }).join(",")`); got != "99" {
				t.Errorf("img: visible options = %q, want only the recent 99 (No matches hidden)", got)
			}
			act(t, ctx, "clear", chromedp.Evaluate[chromedp.Void](`(() => { const i = document.querySelector("#f-vendor"); i.value = ""; i.dispatchEvent(new Event("input", { bubbles: true })); })()`))
			until(t, ctx, "rows for the empty search", `document.querySelector('#f-vendor-list [data-value="4"]') !== null`+d1Idle)

			act(t, ctx, `type "st"`, chromedp.SendKeys(chromedp.CSS("#f-vendor"), "st"))
			until(t, ctx, "rows for st", `document.querySelector('#f-vendor-list [data-value="4"]') === null && document.querySelector("#f-vendor-list .picker__group") !== null`+d1Idle)
			if got := value[string](t, ctx, d1Heads); got != "Recent,Matches" {
				t.Errorf("st: headings = %q, want Recent,Matches", got)
			}
			if got := value[string](t, ctx, d1Values); got != "1,98,2,3" {
				t.Errorf("st: options = %q, want the recent 1,98, then 2,3", got)
			}
			// İ lower-cases to two letters, so "İstanbul" stays unmarked
			// rather than marking the wrong letters.
			if got := value[string](t, ctx, d1Marks); got != "St,ST,St,ST,st" {
				t.Errorf("st: marks = %q, want St,ST (recent name, code), none on İstanbul, St,ST (row 2), st (row 3's name)", got)
			}
			if got := value[string](t, ctx, `document.querySelector("#f-vendor-list [role=option] .picker__text").textContent.replace(/\s+/g, " ").trim()`); got != "Steel rod 12 mmST-001 Raw material · kg" {
				t.Errorf("st: marking changed the recent row's text to %q", got)
			}

			act(t, ctx, "ArrowDown, Enter", chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(kb.Enter))
			if got := value[string](t, ctx, `document.querySelector('[name="vendor_id"]').value`); got != "1" {
				t.Errorf("Enter on the first recent pick set vendor_id = %q, want 1", got)
			}
			if got := value[string](t, ctx, `document.querySelector("[data-picker-summary]").textContent`); got != "ST-001 Steel rod 12 mm" {
				t.Errorf("summary = %q", got)
			}
		})
	}
}
