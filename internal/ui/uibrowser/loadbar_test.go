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
	fault403  = "403"
	fault404  = "404"
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
		case fault403:
			http.Error(w, "refused", http.StatusForbidden)
			return
		case fault404:
			http.NotFound(w, r)
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

// A 503 on a save of a saved record (its form carries a version) shows the
// server banner; Try again re-sends the same request with its submit button,
// and the next success clears the banner (CD-346).
func TestNetworkBanner_RetryAfter503(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	if !markEl(t, ctx, savingButton) {
		t.Fatal(`buttons page: no htmx form with a "Saving…" submit button`)
	}
	do(t, ctx, "name the button, version the form", chromedp.Evaluate[chromedp.Void](`(() => {
  const b = document.querySelector("[data-test-target]");
  b.name = "uiv2a4"; b.value = "again";
  const v = document.createElement("input");
  v.type = "hidden"; v.name = "version"; v.value = "3";
  b.form.append(v);
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

// A 503 on a POST without a saved version (it may have created something)
// offers Reload, never a re-send, and Reload reloads the page (CD-346).
func TestNetworkBanner_UnversionedPostOffersReload(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	g.set("/_gallery/buttons", fault503)
	m := g.mark()
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved. Reload to check before trying again.")`)
	if n := eval[int](t, ctx, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
		t.Errorf("the banner offers %d Try again buttons for an unversioned POST, want none", n)
	}
	g.set("/_gallery/buttons", "")
	do(t, ctx, "mark this page", chromedp.Evaluate[chromedp.Void](`window.uiv2a4Page = 1`))
	clickEl(t, ctx, "press Reload", `document.querySelector("#network [data-reload]")`)
	waitReload(t, ctx, "/_gallery/buttons")
	if n := len(g.requests(m, http.MethodPost, "/_gallery/buttons")); n != 1 {
		t.Errorf("%d POSTs, want only the failed one", n)
	}
}

// waitReload waits until the page marked with window.uiv2a4Page is replaced
// by a fresh load of path; evaluations during the reload may fail.
func waitReload(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	deadline := time.Now().Add(stepTimeout)
	for time.Now().Before(deadline) {
		sctx, cancel := context.WithTimeout(ctx, time.Second)
		ok, err := chromedp.Run(sctx, chromedp.Evaluate[bool](`window.uiv2a4Page === undefined && document.readyState === "complete" && location.pathname === `+jsString(path)))
		cancel()
		if err == nil && ok {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("the page did not reload to %s", path)
}

// CD-387 (5) on a POST with no saved version. Online, a lost answer can't be
// told from an abort, so a drop offers Reload as a 503 does (CD-346). While the
// browser reports offline nothing was sent: the connection text with Try again,
// which saves.
func TestNetworkBanner_UnversionedDrop(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	g.set("/_gallery/buttons", faultDrop)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved. Reload to check before trying again.")`)
	if n := eval[int](t, ctx, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
		t.Errorf("an online drop offers %d Try again buttons, want none", n)
	}

	// That banner stays until its form is answered: a fresh page for the rest.
	offline := func() {
		t.Helper()
		g.set("/_gallery/buttons", "")
		open(t, ctx, 360, g.URL+"/_gallery/buttons")
		do(t, ctx, "report offline", chromedp.Evaluate[chromedp.Void](`Object.defineProperty(navigator, "onLine", {get: () => false})`))
	}
	offline()
	g.set("/_gallery/buttons", faultDrop)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the offline banner", netText+`.includes("Couldn't reach the server. Check your connection. Nothing you typed is lost.")`)
	if n := eval[int](t, ctx, `document.querySelectorAll("#network [data-reload]").length`); n != 0 {
		t.Errorf("an offline drop offers %d Reload buttons, want none", n)
	}

	// Offline covers only what got no answer: a 503 was received.
	offline()
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner for a 503", netText+`.includes("Your change may have been saved.")`)

	offline()
	g.set("/_gallery/buttons", faultDrop)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the offline banner again", netText+`.includes("Couldn't reach the server.")`)
	g.set("/_gallery/buttons", "")
	m := g.mark()
	clickEl(t, ctx, "press Try again", `document.querySelector("#network [data-retry]")`)
	waitFor(t, ctx, "the banner cleared", netEmpty)
	if n := len(g.requests(m, http.MethodPost, "/_gallery/buttons")); n != 1 {
		t.Errorf("%d POSTs after Try again, want 1", n)
	}
}

// CD-387 (5): which failure gets which banner, by a form's version and by what
// marks a request as writing nothing.
func TestNetworkBanner_Matrix(t *testing.T) {
	const (
		offline = "Couldn't reach the server. Check your connection. Nothing you typed is lost."
		server  = "The server had a problem. Try again in a minute."
		unsure  = "Your change may have been saved. Reload to check before trying again."
	)
	const path = "/_gallery/buttons"
	for _, c := range []struct {
		name, setup   string
		drop, fail503 string
	}{
		{"a saved version", `v.value = "4";`, offline, server},
		{"version 0", `v.value = "0";`, unsure, unsure},
		{"a form writing nothing", `b.form.setAttribute("data-no-write", "");`, offline, server},
		{"a button writing nothing", `b.setAttribute("data-no-write", "");`, offline, server},
		{"neither", ``, unsure, unsure},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := startFaultGallery(t)
			ctx := New(t)
			open(t, ctx, 360, g.URL+path)
			do(t, ctx, "set up the form", chromedp.Evaluate[chromedp.Void](`(() => {
  const b = `+savingButton+`;
  const v = document.createElement("input");
  v.type = "hidden"; v.name = "version"; v.value = "";
  b.form.append(v);
  `+c.setup+`
})()`))
			for _, f := range []struct{ fault, want string }{{faultDrop, c.drop}, {fault503, c.fail503}} {
				g.set(path, f.fault)
				clickEl(t, ctx, "press Save", savingButton)
				waitFor(t, ctx, f.fault+": "+f.want, netText+`.includes(`+jsString(f.want)+`)`)
				retry := f.want != unsure
				if got := eval[bool](t, ctx, `!!document.querySelector("#network [data-retry]")`); got != retry {
					t.Errorf("%s: Try again shown = %v, want %v", f.fault, got, retry)
				}
				waitFor(t, ctx, "the button back", `!(`+savingButton+`).disabled`)
			}
		})
	}
}

// Offline counts when the request was sent. Going offline after a request left
// says nothing about what the server got: that's a lost answer.
func TestNetworkBanner_OfflineIsReadAtSend(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	g.set("/_gallery/buttons", faultDrop)
	do(t, ctx, "go offline just after sending", chromedp.Evaluate[chromedp.Void](`document.addEventListener("htmx:beforeSend", () => Object.defineProperty(navigator, "onLine", {get: () => false, configurable: true}))`))
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved.")`)
}

// Reload's banner goes only with an answer to the path it failed on: another
// request says nothing about the lost save. Any other banner goes with any
// answer but a field's check.
func TestNetworkBanner_WhatClearsIt(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	send := func(path string) {
		t.Helper()
		m := g.mark()
		do(t, ctx, "send "+path, chromedp.Evaluate[chromedp.Void](`htmx.ajax("post", `+jsString(path)+`, {source: document.body, swap: "none"})`))
		g.waitRequests(t, m, 1, http.MethodPost, path)
		waitFor(t, ctx, "the request done", `document.querySelector(".htmx-request") === null`)
	}
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved.")`)
	g.set("/_gallery/buttons", "")
	send("/ui/validate/qty")
	send("/_gallery/other")
	if !eval[bool](t, ctx, netBanner+` !== null`) {
		t.Fatal("another request cleared the unsure banner")
	}
	clickEl(t, ctx, "press Reload's form again", savingButton)
	waitFor(t, ctx, "the banner cleared by its own path", netEmpty)

	do(t, ctx, "version the form", chromedp.Evaluate[chromedp.Void](`(() => {
  const v = document.createElement("input");
  v.type = "hidden"; v.name = "version"; v.value = "3";
  (`+savingButton+`).form.append(v);
})()`))
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the server banner", netText+`.includes("The server had a problem.")`)
	send("/ui/validate/qty")
	if !eval[bool](t, ctx, netBanner+` !== null`) {
		t.Fatal("a field's check cleared the server banner")
	}
	send("/_gallery/other")
	waitFor(t, ctx, "the banner cleared by another answer", netEmpty)
}

// The banner takes focus from a failed form post, on any page position, but a
// request the user didn't press for leaves them typing.
func TestNetworkBanner_FocusFollowsTheUsersRequest(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	g.set("/_gallery/wizard-1", faultDrop)
	clickEl(t, ctx, "press Save and continue", wizardPrimary)
	waitFor(t, ctx, "the banner holds focus", netBanner+` !== null && document.activeElement === `+netBanner)

	// Enter in a field sends the form: that is a press too, with focus in a field.
	g.set("/_gallery/wizard-1", "")
	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	g.set("/_gallery/wizard-1", faultDrop)
	do(t, ctx, "press Enter in a field", chromedp.Focus(chromedp.CSS("#f-reference")), chromedp.SendKeys(chromedp.CSS("#f-reference"), "PO-77\r"))
	waitFor(t, ctx, "the banner holds focus after Enter", netBanner+` !== null && document.activeElement === `+netBanner)

	open(t, ctx, 1024, g.URL+"/_gallery/picker")
	if !markEl(t, ctx, `document.querySelector("input[role=combobox]")`) {
		t.Fatal("picker page: no combobox input")
	}
	g.set("/_gallery/items", faultDrop)
	do(t, ctx, "type a search", chromedp.Click(target, chromedp.NodeVisible), chromedp.SendKeys(target, "ce"))
	waitFor(t, ctx, "the banner", netBanner+` !== null`)
	if !eval[bool](t, ctx, `document.activeElement.matches("input[role=combobox]")`) {
		t.Errorf("a failed search took focus to %s", eval[string](t, ctx, `document.activeElement.outerHTML.slice(0, 80)`))
	}
}

// A field's own check failing is no banner: it would take the banner from the
// Save the same tap sent, and it saves nothing.
func TestNetworkBanner_FieldCheckStaysQuiet(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	g.set("/ui/validate/check", faultDrop)
	m := g.mark()
	do(t, ctx, "a field checks itself", chromedp.Evaluate[chromedp.Void](`htmx.ajax("post", "/ui/validate/check", {source: document.body, swap: "none"})`))
	g.waitRequests(t, m, 1, http.MethodPost, "/ui/validate/check")
	waitFor(t, ctx, "the request done", `document.querySelector(".htmx-request") === null`)
	if got := eval[string](t, ctx, netText); got != "" {
		t.Errorf("#network = %q after a field check dropped, want empty", got)
	}
}

// A 4xx is the page's own refusal: it never shows the network banner (CD-346).
// Nor does it clear Reload's banner: a lost first save is still unresolved, and
// a second Save would make a second draft (CD-387 (5) review).
func TestNetworkBanner_4xxLeavesItEmpty(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	for _, f := range []string{fault404, fault403} {
		g.set("/_gallery/buttons", f)
		m := g.mark()
		clickEl(t, ctx, "press Save", savingButton)
		g.waitRequests(t, m, 1, http.MethodPost, "/_gallery/buttons")
		waitFor(t, ctx, "the "+f+" handled", `document.querySelector(".htmx-request") === null`)
		if got := eval[string](t, ctx, netText); got != "" {
			t.Errorf("after a %s, #network = %q, want empty", f, got)
		}
	}
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved.")`)
	for _, f := range []string{fault404, fault403} {
		g.set("/_gallery/buttons", f)
		m := g.mark()
		clickEl(t, ctx, "press Save", savingButton)
		g.waitRequests(t, m, 1, http.MethodPost, "/_gallery/buttons")
		waitFor(t, ctx, "the "+f+" handled", `document.querySelector(".htmx-request") === null`)
		if got := eval[string](t, ctx, netText); !strings.Contains(got, "Your change may have been saved.") {
			t.Errorf("after a %s on the lost save's form, #network = %q, want the unsure banner kept", f, got)
		}
	}
}

// A background request failing after a lost save never puts a safe banner in
// its place: that one would clear on the next good answer, and the warning with
// it.
func TestNetworkBanner_UnsureOutlastsABackgroundFailure(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes("Your change may have been saved.")`)
	for _, f := range []string{faultDrop, fault503, ""} {
		g.set("/_gallery/other", f)
		m := g.mark()
		do(t, ctx, "a background GET", chromedp.Evaluate[chromedp.Void](`htmx.ajax("get", "/_gallery/other", {source: document.body, swap: "none"})`))
		g.waitRequests(t, m, 1, http.MethodGet, "/_gallery/other")
		waitFor(t, ctx, "the request done", `document.querySelector(".htmx-request") === null`)
		if got := eval[string](t, ctx, netText); !strings.Contains(got, "Your change may have been saved.") {
			t.Errorf("after a background GET (%q), #network = %q, want the unsure banner", f, got)
		}
	}
	g.set("/_gallery/buttons", "")
	clickEl(t, ctx, "press Save on its own form", savingButton)
	waitFor(t, ctx, "a 2xx from its own path clears it", netEmpty)
}

// Every request that may have saved needs its own 2xx before the warning goes:
// a second lost POST elsewhere (a quick-create after a lost first save) may
// show its own warning, but its success says nothing about the first. A 2xx
// GET of the first path doesn't either: it's another method.
func TestNetworkBanner_EveryLostSaveNeedsItsAnswer(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	send := func(verb, path, fault string) {
		t.Helper()
		g.set(path, fault)
		m := g.mark()
		do(t, ctx, verb+" "+path, chromedp.Evaluate[chromedp.Void](`htmx.ajax(`+jsString(verb)+`, `+jsString(path)+`, {source: document.body, swap: "none"})`))
		g.waitRequests(t, m, 1, strings.ToUpper(verb), path)
		waitFor(t, ctx, "the request done", `document.querySelector(".htmx-request") === null`)
	}
	const unsure = "Your change may have been saved."
	kept := func(after string) {
		t.Helper()
		if got := eval[string](t, ctx, netText); !strings.Contains(got, unsure) {
			t.Errorf("after %s, #network = %q, want the unsure banner", after, got)
		}
	}
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save", savingButton)
	waitFor(t, ctx, "the unsure banner", netText+`.includes(`+jsString(unsure)+`)`)
	send("post", "/_gallery/cards", fault503)
	kept("a second lost POST")
	send("post", "/_gallery/cards", "")
	kept("the second POST's 2xx")
	send("get", "/_gallery/buttons", "")
	kept("a 2xx GET of the first path")
	g.set("/_gallery/buttons", "")
	clickEl(t, ctx, "press Save again", savingButton)
	waitFor(t, ctx, "the banner cleared by the last 2xx", netEmpty)
}

// A dialog holding the warning can be swapped away without a close event: the
// warning comes home with the next answer.
func TestNetworkBanner_RemovedDialogHandsItBack(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	do(t, ctx, "move the form into a dialog", chromedp.Evaluate[chromedp.Void](`(() => {
  const d = document.createElement("dialog");
  d.id = "uiv2r1-dialog";
  const box = document.createElement("div");
  box.className = "modal-box";
  box.append((`+savingButton+`).form);
  d.append(box);
  document.body.append(d);
  d.showModal();
})()`))
	g.set("/_gallery/buttons", fault503)
	clickEl(t, ctx, "press Save in the dialog", savingButton)
	waitFor(t, ctx, "the banner in the dialog", `!!document.querySelector("#uiv2r1-dialog .banner")`)
	do(t, ctx, "swap the dialog away", chromedp.Evaluate[chromedp.Void](`document.getElementById("uiv2r1-dialog").remove()`))
	m := g.mark()
	do(t, ctx, "another request", chromedp.Evaluate[chromedp.Void](`htmx.ajax("get", "/_gallery/buttons", {source: document.body, swap: "none"})`))
	g.waitRequests(t, m, 1, http.MethodGet, "/_gallery/buttons")
	waitFor(t, ctx, "the warning back on the page", netText+`.includes("Your change may have been saved.")`)
}

// Reload's banner, after a lost save: every failure or answer that can follow
// it, and only a 2xx on the same method and path clears it. Kept, it never
// turns into a Try again that would send the save twice.
func TestNetworkBanner_WhatClearsReload(t *testing.T) {
	const unsure = "Your change may have been saved."
	ajax := func(verb, path string) string {
		return `htmx.ajax(` + jsString(verb) + `, ` + jsString(path) + `, {source: document.body, swap: "none"})`
	}
	for _, c := range []struct {
		name, method, path, fault, js string
		press, cleared            bool
	}{
		{name: "unsafe on another key", method: http.MethodPost, path: "/_gallery/cards", fault: fault503, js: ajax("post", "/_gallery/cards")},
		{name: "safe in the background", method: http.MethodGet, path: "/_gallery/other", fault: faultDrop, js: ajax("get", "/_gallery/other")},
		{name: "offline-safe on the same key", method: http.MethodPost, path: "/_gallery/buttons", fault: faultDrop, press: true,
			js: `Object.defineProperty(navigator, "onLine", {get: () => false, configurable: true})`},
		{name: "a 4xx on the same key", method: http.MethodPost, path: "/_gallery/buttons", fault: fault404, press: true},
		{name: "a field check answered", method: http.MethodPost, path: "/ui/validate/check", js: ajax("post", "/ui/validate/check")},
		{name: "a field check dropped", method: http.MethodPost, path: "/ui/validate/check", fault: faultDrop, js: ajax("post", "/ui/validate/check")},
		{name: "a 2xx on the same method and path", method: http.MethodPost, path: "/_gallery/buttons", press: true, cleared: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := startFaultGallery(t)
			ctx := New(t)
			open(t, ctx, 360, g.URL+"/_gallery/buttons")
			g.set("/_gallery/buttons", fault503)
			clickEl(t, ctx, "press Save", savingButton)
			waitFor(t, ctx, "the unsure banner", netText+`.includes(`+jsString(unsure)+`)`)
			waitFor(t, ctx, "the button back", `!(`+savingButton+`).disabled`)
			g.set("/_gallery/buttons", "")
			g.set(c.path, c.fault)
			m := g.mark()
			if c.js != "" {
				do(t, ctx, c.name, chromedp.Evaluate[chromedp.Void](c.js))
			}
			if c.press {
				clickEl(t, ctx, "press Save", savingButton)
			}
			g.waitRequests(t, m, 1, c.method, c.path)
			waitFor(t, ctx, "the request done", `document.querySelector(".htmx-request") === null`)
			if c.cleared {
				waitFor(t, ctx, "the banner cleared", netEmpty)
				return
			}
			if got := eval[string](t, ctx, netText); !strings.Contains(got, unsure) {
				t.Errorf("#network = %q, want the unsure banner kept", got)
			}
			if n := eval[int](t, ctx, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
				t.Errorf("the banner offers %d Try again buttons, want Reload only", n)
			}
		})
	}
}

// A request the user didn't press for leaves them typing: focus and the caret
// stay where they were.
func TestNetworkBanner_TypingKeepsFocus(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	do(t, ctx, "type in a field", chromedp.Focus(chromedp.CSS("#f-reference")), chromedp.SendKeys(chromedp.CSS("#f-reference"), "PO-77"))
	do(t, ctx, "move the caret", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-reference").setSelectionRange(2, 2)`))
	g.set("/_gallery/other", faultDrop)
	m := g.mark()
	do(t, ctx, "a background request", chromedp.Evaluate[chromedp.Void](`htmx.ajax("get", "/_gallery/other", {source: document.body, swap: "none"})`))
	g.waitRequests(t, m, 1, http.MethodGet, "/_gallery/other")
	waitFor(t, ctx, "the banner", netBanner+` !== null`)
	if got := eval[string](t, ctx, `document.activeElement.id + ":" + document.activeElement.selectionStart`); got != "f-reference:2" {
		t.Errorf("focus and caret = %q, want f-reference:2", got)
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
	if n := eval[int](t, ctx, `document.querySelectorAll("#network [data-reload]").length`); n != 0 {
		t.Errorf("a versioned step save offers %d Reload buttons, want Try again", n)
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
// timeout shows a banner too (here the unversioned POST's Reload one).
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
	waitFor(t, ctx, "the unsure banner on timeout", netText+`.includes("Your change may have been saved.")`)
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
	do(t, ctx, "focus an editable region", chromedp.Evaluate[chromedp.Void](`(() => {
  const e = document.createElement("div");
  e.id = "uiv2a4-edit"; e.contentEditable = "true"; e.textContent = "x";
  document.querySelector("main").append(e);
  e.focus();
})()`))
	pressSlash(t, ctx)
	if got := eval[string](t, ctx, `document.activeElement.id + "|" + document.activeElement.textContent`); !strings.HasPrefix(got, "uiv2a4-edit|") || !strings.Contains(got, "/") {
		t.Errorf(`after "/" in an editable region: focused|text = %q, want the region with a slash typed`, got)
	}
}

// Below 64rem the header search is hidden, so "/" is left alone.
func TestShortcut_SlashLeftAloneWhenSearchHidden(t *testing.T) {
	g := startFaultGallery(t)
	ctx := New(t)
	for _, c := range []struct {
		width int64
		want  bool
	}{{1024, true}, {360, false}} {
		open(t, ctx, c.width, g.URL+"/_gallery/buttons")
		do(t, ctx, "watch the key", chromedp.Evaluate[chromedp.Void](`(() => {
  document.activeElement && document.activeElement.blur();
  window.uiv2a4Prevented = null;
  window.addEventListener("keydown", function (e) { window.uiv2a4Prevented = e.defaultPrevented; });
})()`))
		pressSlash(t, ctx)
		if got := eval[bool](t, ctx, `window.uiv2a4Prevented === true`); got != c.want {
			t.Errorf(`at %d px "/" default prevented = %v, want %v`, c.width, got, c.want)
		}
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
