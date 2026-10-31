//go:build e2e

package uibrowser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/gallery"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// Loading bar, network banner, "/" shortcut and motion base (UI v2 §5.1,
// §5.2, §5.14; A-27, A-28, A-32). Every step goes through open, eval, do or
// waitFor, which carry a step deadline.

// A fault the server applies to every request for a path until cleared.
const (
	faultDrop = "drop" // close the connection without an answer
	fault503  = "503"
	faultSlow = "slow" // answer after slowFor
)

const slowFor = 1500 * time.Millisecond

type faultReq struct {
	Method, Path, Form string
}

// faultGallery is gallery.Handler behind faults set per path, logging every
// request with its posted form.
type faultGallery struct {
	*httptest.Server
	mu     sync.Mutex
	faults map[string]string
	log    []faultReq
}

func startFaultGallery(t *testing.T) *faultGallery {
	t.Helper()
	g := &faultGallery{faults: map[string]string{}}
	h := gallery.Handler()
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		g.mu.Lock()
		g.log = append(g.log, faultReq{r.Method, r.URL.Path, r.PostForm.Encode()})
		f := g.faults[r.URL.Path]
		g.mu.Unlock()
		switch f {
		case faultDrop:
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				_ = conn.Close()
			}
			return
		case fault503:
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		case faultSlow:
			time.Sleep(slowFor)
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *faultGallery) set(path, fault string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if fault == "" {
		delete(g.faults, path)
		return
	}
	g.faults[path] = fault
}

func (g *faultGallery) mark() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.log)
}

// requests are the requests for path logged after mark m.
func (g *faultGallery) requests(m int, method, path string) []faultReq {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []faultReq
	for _, r := range g.log[m:] {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return slices.Clone(out)
}

// waitRequests polls until path has had n requests since m.
func (g *faultGallery) waitRequests(t *testing.T, m, n int, method, path string) []faultReq {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := g.requests(m, method, path)
		if len(got) >= n || time.Now().After(deadline) {
			if len(got) < n {
				t.Fatalf("%d %s %s requests, want at least %d", len(got), method, path, n)
			}
			return got
		}
		time.Sleep(50 * time.Millisecond)
	}
}

const (
	netBanner     = `document.querySelector("#network .banner")`
	netText       = `(document.getElementById("network") || {textContent: ""}).textContent`
	netEmpty      = `document.getElementById("network") !== null && document.getElementById("network").children.length === 0`
	savingButton  = `Array.from(document.querySelectorAll("form[hx-post] button[type=submit]")).find(function (b) { return b.dataset.busyLabel === "Saving…"; })`
	wizardPrimary = `document.querySelector("button[type=submit].btn-primary")`
)

func clickEl(t *testing.T, ctx context.Context, what, expr string) {
	t.Helper()
	if !markEl(t, ctx, expr) {
		t.Fatalf("%s: no such element", what)
	}
	do(t, ctx, what, chromedp.Click(target, chromedp.NodeVisible))
}

// A-27: a request slower than 300 ms shows the bar; it goes when the
// request ends.
func TestLoadbar_ShowsDuringASlowRequest(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 1024, g.URL+"/_gallery/buttons")
	if got := eval[string](t, ctx, `getComputedStyle(document.getElementById("loadbar")).opacity`); got != "0" {
		t.Fatalf("idle #loadbar opacity = %s, want 0", got)
	}
	g.set("/_gallery/buttons", faultSlow)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the bar shown", `(() => {
  const b = document.getElementById("loadbar");
  return b.classList.contains("htmx-request") && getComputedStyle(b).opacity === "1";
})()`)
	if got := eval[string](t, ctx, `getComputedStyle(document.getElementById("loadbar")).transitionDelay`); got != "0.3s" {
		t.Errorf("#loadbar transition-delay while loading = %s, want 0.3s (shown only after 300 ms)", got)
	}
	if got := eval[float64](t, ctx, `document.getElementById("loadbar").getBoundingClientRect().height`); got != 3 {
		t.Errorf("#loadbar height = %v px, want 3", got)
	}
	if probs := eval[[]string](t, ctx, screens.OverflowJS); len(probs) != 0 {
		t.Errorf("overflow while the bar runs: %v", probs)
	}
	waitFor(t, ctx, "the bar gone", `!document.getElementById("loadbar").classList.contains("htmx-request")`)
}

// A-27: a picker search has its own indicator, so typing never shows the bar.
func TestLoadbar_PickerSearchDoesNotShowIt(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 1024, g.URL+"/_gallery/picker")
	do(t, ctx, "watch the bar", chromedp.Evaluate[chromedp.Void](`(() => {
  window.uiv2a4Bar = false;
  new MutationObserver(function () {
    if (document.getElementById("loadbar").classList.contains("htmx-request")) window.uiv2a4Bar = true;
  }).observe(document.getElementById("loadbar"), {attributes: true});
})()`))
	if !markEl(t, ctx, `document.querySelector("input[role=combobox]")`) {
		t.Fatal("picker page: no combobox input")
	}
	m := g.mark()
	do(t, ctx, "type a search", chromedp.Click(target, chromedp.NodeVisible), chromedp.SendKeys(target, "ce"))
	g.waitRequests(t, m, 1, http.MethodGet, "/_gallery/items")
	waitFor(t, ctx, "the search done", `document.querySelector(".htmx-request") === null`)
	if eval[bool](t, ctx, `window.uiv2a4Bar`) {
		t.Error("a picker search showed the loading bar")
	}
}

// A 503 on a save shows the server banner; Try again re-sends the same
// request with its submit button, and the next success clears the banner.
func TestNetworkBanner_RetryAfter503(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	if !markEl(t, ctx, savingButton) {
		t.Fatal(`buttons page: no htmx form with a "Saving…" submit button`)
	}
	do(t, ctx, "name the button", chromedp.Evaluate[chromedp.Void](`(() => {
  const b = document.querySelector("[data-test-target]");
  b.name = "uiv2a4"; b.value = "again";
})()`))
	g.set("/_gallery/buttons", fault503)
	m := g.mark()
	do(t, ctx, "press Save", chromedp.Click(target, chromedp.NodeVisible))
	waitFor(t, ctx, "the server banner", netText+`.includes("The server had a problem.")`)
	if got := eval[string](t, ctx, netText); !strings.Contains(got, "Try again in a minute.") {
		t.Errorf("banner text = %q, want the 5xx line", got)
	}
	g.set("/_gallery/buttons", "")
	clickEl(t, ctx, "press Try again", `document.querySelector("#network [data-retry]")`)
	waitFor(t, ctx, "the banner cleared", netEmpty)
	got := g.waitRequests(t, m, 2, http.MethodPost, "/_gallery/buttons")
	if len(got) != 2 {
		t.Fatalf("%d POSTs, want the failed one and one retry", len(got))
	}
	if !strings.Contains(got[1].Form, "uiv2a4=again") {
		t.Errorf("retried POST form = %q, want the submit button's uiv2a4=again", got[1].Form)
	}
}

// A-28: a dropped connection on Save shows "Couldn't reach the server." with
// Try again, keeps the typed values, never covers a field, and Try again
// saves.
func TestNetworkBanner_RetryAfterNetworkError(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	do(t, ctx, "type the step", chromedp.SetValue(chromedp.CSS("#f-reference"), "PO-77"), chromedp.SetValue(chromedp.CSS("#f-order_date"), "24-10-2026"))
	g.set("/_gallery/wizard-1", faultDrop)
	clickEl(t, ctx, "press Save and continue", wizardPrimary)
	waitFor(t, ctx, "the network banner", netText+`.includes("Couldn't reach the server.")`)
	if got := eval[string](t, ctx, netText); !strings.Contains(got, "Check your connection. Nothing you typed is lost.") {
		t.Errorf("banner text = %q, want the network line", got)
	}
	if got := eval[string](t, ctx, `document.getElementById("f-reference").value`); got != "PO-77" {
		t.Errorf("reference after the failure = %q, want the typed PO-77", got)
	}
	if n := eval[int](t, ctx, `document.querySelectorAll("#network [role]").length`); n != 0 {
		t.Errorf("the banner has %d role attributes; the polite region announces it", n)
	}
	if over := eval[[]string](t, ctx, `(() => {
  const b = `+netBanner+`.getBoundingClientRect();
  return Array.from(document.querySelectorAll("input, select, textarea")).filter(function (e) {
    const r = e.getBoundingClientRect();
    return r.width > 0 && r.left < b.right && r.right > b.left && r.top < b.bottom && r.bottom > b.top;
  }).map(function (e) { return e.id || e.name; });
})()`); len(over) != 0 {
		t.Errorf("the banner covers fields %v", over)
	}
	g.set("/_gallery/wizard-1", "")
	m := g.mark()
	clickEl(t, ctx, "press Try again", `document.querySelector("#network [data-retry]")`)
	got := g.waitRequests(t, m, 1, http.MethodPost, "/_gallery/wizard-1")
	if !strings.Contains(got[0].Form, "reference=PO-77") {
		t.Errorf("retried POST form = %q, want the typed reference", got[0].Form)
	}
	waitFor(t, ctx, "step 2", `location.pathname === "/_gallery/wizard-2"`)
}

// A failed search (no submit button) is re-issued as the same GET; an htmx
// timeout shows the network banner too.
func TestNetworkBanner_RetryGetAndTimeout(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 1024, g.URL+"/_gallery/picker")
	g.set("/_gallery/items", faultDrop)
	if !markEl(t, ctx, `document.querySelector("input[role=combobox]")`) {
		t.Fatal("picker page: no combobox input")
	}
	do(t, ctx, "type a search", chromedp.Click(target, chromedp.NodeVisible), chromedp.SendKeys(target, "ce"))
	waitFor(t, ctx, "the network banner", netText+`.includes("Couldn't reach the server.")`)
	g.set("/_gallery/items", "")
	m := g.mark()
	clickEl(t, ctx, "press Try again", `document.querySelector("#network [data-retry]")`)
	g.waitRequests(t, m, 1, http.MethodGet, "/_gallery/items")
	waitFor(t, ctx, "the banner cleared", netEmpty)

	open(t, ctx, 1024, g.URL+"/_gallery/buttons")
	do(t, ctx, "a 300 ms timeout", chromedp.Evaluate[chromedp.Void](`htmx.config.timeout = 300`))
	g.set("/_gallery/buttons", faultSlow)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the network banner on timeout", netText+`.includes("Couldn't reach the server.")`)
}

// "/" focuses the header search from the page, and types a slash in a field.
func TestShortcut_SlashFocusesSearchOutsideFields(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 1024, g.URL+"/_gallery/wizard-1")
	do(t, ctx, "blur", chromedp.Evaluate[chromedp.Void](`document.activeElement && document.activeElement.blur()`))
	pressSlash(t, ctx)
	if got := eval[string](t, ctx, `document.activeElement.id + "|" + document.activeElement.value`); got != "header-q|" {
		t.Errorf(`after "/" on the page: focused#value = %q, want the empty header search`, got)
	}
	do(t, ctx, "focus the reference field", chromedp.Evaluate[chromedp.Void](`(() => { const e = document.getElementById("f-reference"); e.value = ""; e.focus(); })()`))
	pressSlash(t, ctx)
	if got := eval[string](t, ctx, `document.activeElement.id + "|" + document.activeElement.value`); got != "f-reference|/" {
		t.Errorf(`after "/" in the reference field: focused|value = %q, want f-reference|/`, got)
	}
}

// pressSlash sends one "/" key press as a keyboard does: a keyDown that
// carries the text, so cancelling it stops the slash being typed.
func pressSlash(t *testing.T, ctx context.Context) {
	t.Helper()
	sctx, cancel := step(ctx)
	defer cancel()
	for _, p := range []input.DispatchKeyEventParams{
		{Type: input.DispatchKeyEventTypeKeyDown, Key: "/", Code: "Slash", Text: "/", WindowsVirtualKeyCode: 191},
		{Type: input.DispatchKeyEventTypeKeyUp, Key: "/", Code: "Slash", WindowsVirtualKeyCode: 191},
	} {
		if _, err := chromedp.Call(sctx, input.DispatchKeyEvent, p); err != nil {
			t.Fatalf("press /: %v", err)
		}
	}
}

func reducedMotion(t *testing.T, ctx context.Context) {
	t.Helper()
	sctx, cancel := step(ctx)
	defer cancel()
	if _, err := chromedp.Call(sctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{
		Features: []*emulation.MediaFeature{{Name: "prefers-reduced-motion", Value: "reduce"}},
	}); err != nil {
		t.Fatalf("emulate reduced motion: %v", err)
	}
}

const openSheetAndToast = `(() => {
  document.querySelector('[data-open-dialog="sample-sheet"]').click();
  document.body.dispatchEvent(new CustomEvent("notice", {bubbles: true, detail: {level: "success", text: "Saved."}}));
  return document.getAnimations().length;
})()`

// A-32: with motion allowed, a sheet and a toast animate in; under reduced
// motion nothing is running 50 ms later.
func TestMotion_ReducedMotionStopsAnimations(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/dialogs")
	if n := eval[int](t, ctx, openSheetAndToast); n == 0 {
		t.Fatal("control: opening a sheet and a toast started no animation")
	}
	if got := eval[string](t, ctx, `getComputedStyle(document.querySelector("#sample-sheet .modal-box")).transitionDuration`); !strings.Contains(got, "0.25s") {
		t.Errorf("sheet transition-duration = %s, want 0.25s (--dur-slow)", got)
	}
	if got := eval[string](t, ctx, `getComputedStyle(document.querySelector("#toasts > .alert")).transitionDuration`); !strings.Contains(got, "0.25s") {
		t.Errorf("toast transition-duration = %s, want 0.25s (--dur-slow)", got)
	}

	reducedMotion(t, ctx)
	open(t, ctx, 360, g.URL+"/_gallery/dialogs")
	eval[int](t, ctx, openSheetAndToast)
	// tokens.css's reduce rule takes every duration to ~0 (A1, CD-341 (3)).
	if got := eval[[]string](t, ctx, `(() => {
  function long(v) { return v.split(",").some(function (d) { return parseFloat(d) >= 0.001; }); }
  const out = [];
  const box = getComputedStyle(document.querySelector("#sample-sheet .modal-box")).transitionDuration;
  if (long(box)) out.push("sheet transition " + box);
  const toast = getComputedStyle(document.querySelector("#toasts > .alert")).transitionDuration;
  if (long(toast)) out.push("toast transition " + toast);
  const bar = getComputedStyle(document.getElementById("loadbar"), "::before");
  if (long(bar.animationDuration) || bar.animationIterationCount !== "1") out.push("loadbar animation " + bar.animationDuration + " x" + bar.animationIterationCount);
  return out;
})()`); len(got) != 0 {
		t.Errorf("under reduced motion, durations are not ~0: %v", got)
	}
	do(t, ctx, "wait 50 ms", chromedp.Sleep(50*time.Millisecond))
	// daisyUI's set-page-has-scroll runs on a scroll timeline, not the clock:
	// it marks a scrollable page while a modal is open and moves nothing, so
	// only clock-driven animations count. The 0.01 ms transitions end on the
	// next frame, which a loaded headless browser may not draw within 50 ms,
	// so the check allows up to two frames after the wait (requestAnimationFrame).
	const running = `document.getAnimations().filter(function (a) { return a.timeline === document.timeline; }).map(function (a) {
  const el = a.effect && a.effect.target;
  return (a.animationName || a.transitionProperty) + " on " + (el ? el.tagName + "." + el.className : "?") + " " + String(a.effect.getComputedTiming().duration) + "ms";
})`
	do(t, ctx, "two frames", chromedp.Evaluate[chromedp.Void](`window.uiv2a4Frames = 0; requestAnimationFrame(function () { requestAnimationFrame(function () { window.uiv2a4Frames = 2; }); })`))
	waitFor(t, ctx, "two frames drawn", `window.uiv2a4Frames === 2`)
	if got := eval[[]string](t, ctx, running); len(got) != 0 {
		t.Errorf("under reduced motion, animations still run after 50 ms: %v", got)
	}
}

// Full page loads get no view transition (the page takes no taps during
// one, and the e2e flows lost their first tap after a load); the header keeps
// its view-transition name for htmx swaps; a pressed button scales to 0.98.
func TestMotion_ViewTransitionsAndPressedButton(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 1024, g.URL+"/_gallery/buttons")
	if eval[bool](t, ctx, `(() => {
  function walk(rules) {
    for (const r of rules) {
      if (r.constructor.name === "CSSViewTransitionRule" && String(r.navigation) === "auto") return true;
      if (r.cssRules && walk(r.cssRules)) return true;
    }
    return false;
  }
  return Array.from(document.styleSheets).some(function (s) { return walk(s.cssRules); });
})()`) {
		t.Error("a @view-transition { navigation: auto } rule is back: taps right after a page load are lost")
	}
	if got := eval[string](t, ctx, `getComputedStyle(document.querySelector("header")).viewTransitionName`); got != "header" {
		t.Errorf("header view-transition-name = %q, want header", got)
	}
	reducedMotion(t, ctx)
	xy := eval[[]float64](t, ctx, `(() => {
  const b = document.querySelector("main .btn");
  b.setAttribute("data-test-target", "");
  b.scrollIntoView({block: "center"});
  const r = b.getBoundingClientRect();
  return [r.left + r.width / 2, r.top + r.height / 2];
})()`)
	do(t, ctx, "press and hold", chromedp.MouseEvent(input.DispatchMouseEventTypeMousePressed, xy[0], xy[1], chromedp.ButtonLeft, chromedp.ClickCount(1)))
	if got := eval[string](t, ctx, `getComputedStyle(document.querySelector("[data-test-target]")).scale`); got != "0.98" {
		t.Errorf("pressed button scale = %q, want 0.98", got)
	}
	do(t, ctx, "release", chromedp.MouseEvent(input.DispatchMouseEventTypeMouseReleased, xy[0], xy[1], chromedp.ButtonLeft, chromedp.ClickCount(1)))
}
