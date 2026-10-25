//go:build e2e

package uibrowser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/css"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/gallery"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// hugeLogoPath serves a 4000 × 500 logo (TestLayout_BrandAssets, Task 6)
// that the overflow test puts in the brand-logo slot.
const hugeLogoPath = "/_test/huge-logo.svg"

const hugeLogo = `<svg xmlns="http://www.w3.org/2000/svg" width="4000" height="500" viewBox="0 0 4000 500"><rect width="4000" height="500" fill="#123456"/></svg>`

// clearTarget empties the marked input without focusing it.
var clearTarget = chromedp.Evaluate[chromedp.Void](`document.querySelector("[data-test-target]").value = ""`)

// galleryPages are the gallery's own pages (plan Task 15).
var galleryPages = []string{
	"buttons", "fields", "badges-banners", "dialogs", "datalist", "lines",
	"picker", "upload", "wizard-1", "wizard-2", "wizard-3", "wizard-4",
	"actionbar", "warnings", "filterbar", "pagination", "bare",
}

// target marks the element a test drives, so chromedp can select it.
const target = chromedp.CSS("[data-test-target]")

type reqLog struct {
	Method, Path string
	Status       int
}

// galleryServer is gallery.Handler behind a recorder: it logs every request
// with its status, counts CSP reports and serves the huge test logo.
type galleryServer struct {
	*httptest.Server
	mu      sync.Mutex
	log     []reqLog
	reports int
}

type statusWriter struct {
	http.ResponseWriter
	code int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.code == 0 {
		s.code = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.code == 0 {
		s.code = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func startGallery(t *testing.T) *galleryServer {
	t.Helper()
	g := &galleryServer{}
	h := gallery.Handler()
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == hugeLogoPath {
			w.Header().Set("Content-Type", "image/svg+xml")
			_, _ = w.Write([]byte(hugeLogo))
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == screens.CSPReportPath {
			g.mu.Lock()
			g.reports++
			g.mu.Unlock()
		}
		sw := &statusWriter{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		g.mu.Lock()
		g.log = append(g.log, reqLog{r.Method, r.URL.Path, sw.code})
		g.mu.Unlock()
	}))
	t.Cleanup(g.Close)
	return g
}

func (g *galleryServer) mark() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.log)
}

// since returns the requests logged after mark m.
func (g *galleryServer) since(m int) []reqLog {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.log[m:])
}

func (g *galleryServer) reportCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reports
}

func countPosts(rs []reqLog, path string) int {
	n := 0
	for _, r := range rs {
		if r.Method == http.MethodPost && r.Path == path {
			n++
		}
	}
	return n
}

func jsString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// stepTimeout bounds every browser step, so a missing element fails the
// test instead of waiting for it forever.
const stepTimeout = 15 * time.Second

func step(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, stepTimeout)
}

func eval[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	sctx, cancel := step(ctx)
	defer cancel()
	v, err := chromedp.Run(sctx, chromedp.Evaluate[T](expr))
	if err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	return v
}

func do(t *testing.T, ctx context.Context, what string, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	sctx, cancel := step(ctx)
	defer cancel()
	if err := chromedp.Do(sctx, steps...); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// waitFor polls the JavaScript predicate expr until it is true.
func waitFor(t *testing.T, ctx context.Context, what, expr string) {
	t.Helper()
	if _, err := chromedp.Run(ctx, chromedp.Poll[chromedp.Void](expr, chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("waiting for %s: %v", what, err)
	}
}

// markEl puts data-test-target on the element expr returns, and reports
// whether there was one.
func markEl(t *testing.T, ctx context.Context, expr string) bool {
	t.Helper()
	return eval[bool](t, ctx, `(() => {
  document.querySelectorAll("[data-test-target]").forEach(function (e) { e.removeAttribute("data-test-target"); });
  const el = (`+expr+`);
  if (!el) return false;
  el.setAttribute("data-test-target", "");
  return true;
})()`)
}

func open(t *testing.T, ctx context.Context, w int64, u string) {
	t.Helper()
	if err := At(ctx, w, u); err != nil {
		t.Fatalf("open %s@%d: %v", u, w, err)
	}
}

// visit opens s at width w and runs its Setup; false means the error is
// already reported.
func visit(t *testing.T, ctx context.Context, base string, s screens.Screen, w int64, query string) bool {
	t.Helper()
	u := base + s.URL
	if query != "" {
		if strings.Contains(u, "?") {
			u += "&" + query
		} else {
			u += "?" + query
		}
	}
	if err := At(ctx, w, u); err != nil {
		t.Errorf("%s@%d: open: %v", s.Name, w, err)
		return false
	}
	if !eval[bool](t, ctx, `document.querySelector("main#main") !== null`) {
		t.Errorf("%s@%d: %s has no main#main", s.Name, w, u)
		return false
	}
	if s.Setup != nil {
		if err := s.Setup(ctx); err != nil {
			t.Errorf("%s@%d: setup: %v", s.Name, w, err)
			return false
		}
	}
	return true
}

func galleryScreens(t *testing.T) []screens.Screen {
	t.Helper()
	all := gallery.Screens("")
	if len(all) == 0 {
		t.Fatal("gallery.Screens is empty")
	}
	for _, s := range all {
		if s.As != "" {
			t.Errorf("%s: As = %q; uibrowser.Check refuses screens that sign in", s.Name, s.As)
		}
	}
	for _, p := range galleryPages {
		if !slices.ContainsFunc(all, func(s screens.Screen) bool { return strings.SplitN(s.URL, "?", 2)[0] == "/_gallery/"+p }) {
			t.Errorf("gallery.Screens has no /_gallery/%s", p)
		}
	}
	return all
}

func expectNone(t *testing.T, name string, w int64, what string, probs []string, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("%s@%d: %s: %v", name, w, what, err)
	}
	for _, p := range probs {
		t.Errorf("%s@%d: %s: %s", name, w, what, p)
	}
}

func TestGalleryBrowser_NoHorizontalOverflow(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, s := range galleryScreens(t) {
		for _, w := range screens.Widths {
			if !visit(t, ctx, g.URL, s, w, "") {
				continue
			}
			probs, err := Overflow(ctx)
			expectNone(t, s.Name, w, "overflow", probs, err)
		}
	}

	// The logo slot holds a 4000 × 500 logo in the header and on the bare
	// (sign-in shaped) page.
	for _, p := range []string{"/_gallery", "/_gallery/bare"} {
		for _, w := range screens.Widths {
			open(t, ctx, w, g.URL+p)
			n := eval[int](t, ctx, `(() => {
  const imgs = document.querySelectorAll("img.brand-logo");
  imgs.forEach(function (i) { i.setAttribute("width", "4000"); i.setAttribute("height", "500"); i.src = `+jsString(hugeLogoPath)+`; });
  return imgs.length;
})()`)
			if n == 0 {
				t.Errorf("%s@%d: no img.brand-logo", p, w)
				continue
			}
			waitFor(t, ctx, "the huge logo", `Array.from(document.querySelectorAll("img.brand-logo")).every(function (i) { return i.complete && i.naturalWidth === 4000; })`)
			probs, err := Overflow(ctx)
			expectNone(t, p+" with a 4000x500 logo", w, "overflow", probs, err)
		}
	}
}

func TestGalleryBrowser_Axe(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, s := range galleryScreens(t) {
		for _, w := range []int64{360, 1440} {
			if !visit(t, ctx, g.URL, s, w, "") {
				continue
			}
			vs, err := Axe(ctx)
			if err != nil {
				t.Errorf("%s@%d: axe: %v", s.Name, w, err)
			}
			for _, v := range vs {
				t.Errorf("%s@%d: axe %s: %s: %s", s.Name, w, v.ID, v.Help, strings.Join(v.Nodes, ", "))
			}
		}
	}
}

func TestGalleryBrowser_TargetSize(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, s := range galleryScreens(t) {
		if !visit(t, ctx, g.URL, s, 360, "") {
			continue
		}
		small, err := SmallTargets(ctx)
		expectNone(t, s.Name, 360, "target smaller than 44px", small, err)
	}
}

func TestGalleryBrowser_NoCSPViolations(t *testing.T) {
	g := startGallery(t)

	res, err := http.Get(g.URL + "/_gallery/buttons?csp=strict")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	policy := res.Header.Get("Content-Security-Policy")
	if !strings.Contains(policy, "style-src 'self'") || strings.Contains(policy, "unsafe-inline") ||
		!strings.Contains(policy, "report-uri "+screens.CSPReportPath) {
		t.Fatalf("?csp=strict policy = %q; want style-src 'self' without 'unsafe-inline' and report-uri %s", policy, screens.CSPReportPath)
	}

	ctx := New(t)
	csp := CSPProblems(ctx)
	for _, s := range galleryScreens(t) {
		for _, w := range []int64{360, 1440} {
			before := g.reportCount()
			if !visit(t, ctx, g.URL, s, w, "csp=strict") {
				continue
			}
			time.Sleep(150 * time.Millisecond) // reports are sent after the page runs
			if n := g.reportCount() - before; n != 0 {
				t.Errorf("%s@%d: %d reports at %s", s.Name, w, n, screens.CSPReportPath)
			}
			for _, p := range csp() {
				t.Errorf("%s@%d: CSP: %s", s.Name, w, p)
			}
		}
	}

	// The check above must be able to see a violation: an inline style on a
	// strict page is reported.
	before := g.reportCount()
	open(t, ctx, 360, g.URL+"/_gallery/buttons?csp=strict")
	do(t, ctx, "add an inline style", chromedp.Evaluate[chromedp.Void](`document.body.setAttribute("style", "outline: 1px solid red")`))
	deadline := time.Now().Add(3 * time.Second)
	for g.reportCount() == before && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if g.reportCount() == before {
		t.Errorf("an inline style on a ?csp=strict page sent no report to %s", screens.CSPReportPath)
	}
	_ = csp()
}

func TestGalleryBrowser_MoneyBlur(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/fields")
	if !markEl(t, ctx, `document.querySelector('input[hx-post="/ui/validate/money"]:not([aria-invalid])')`) {
		t.Fatal("fields page: no money input checked by /ui/validate/money")
	}
	id := eval[string](t, ctx, `document.querySelector("[data-test-target]").id`)
	el := `document.getElementById(` + jsString(id) + `)`
	msg := `document.getElementById(` + jsString(id+"-msg") + `)`

	do(t, ctx, "type 1411200 and Tab", clearTarget, chromedp.SendKeys(target, "1411200"), chromedp.KeyEvent(kb.Tab))
	waitFor(t, ctx, "the formatted money value", el+`.value === "14,11,200.00"`)
	if eval[bool](t, ctx, el+`.hasAttribute("aria-invalid")`) {
		t.Error(`a valid amount left aria-invalid on the input`)
	}

	_, perr := types.ParseMoney("1.234,56")
	if perr == nil {
		t.Fatal(`types.ParseMoney("1.234,56") accepted the value`)
	}
	want := perr.Error()
	do(t, ctx, "type 1.234,56 and Tab", clearTarget, chromedp.SendKeys(target, "1.234,56"), chromedp.KeyEvent(kb.Tab))
	waitFor(t, ctx, "NUM's error under the field", msg+` !== null && `+msg+`.textContent.trim() === `+jsString(want))
	if v := eval[string](t, ctx, el+`.value`); v != "1.234,56" {
		t.Errorf("input value = %q, want the typed 1.234,56 kept", v)
	}
	if a := eval[string](t, ctx, el+`.getAttribute("aria-invalid") || ""`); a != "true" {
		t.Errorf(`aria-invalid = %q, want "true"`, a)
	}
}

func TestGalleryBrowser_StepperAndFocus(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	submit := chromedp.CSS(`#wizard form button[type=submit]:not([name=nav])`)

	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	do(t, ctx, "mark the page", chromedp.Evaluate[chromedp.Void](`window.__samePage = true`))
	do(t, ctx, "submit step 1", chromedp.Click(submit))
	waitFor(t, ctx, "step 2", `location.pathname === "/_gallery/wizard-2"`)
	waitFor(t, ctx, "focus on the step heading", `document.activeElement !== null && document.activeElement.id === "step-title"`)
	if got := eval[string](t, ctx, `(document.getElementById("step-count") || {textContent: ""}).textContent.trim()`); got != "Step 2 of 4" {
		t.Errorf("#step-count = %q, want %q", got, "Step 2 of 4")
	}
	if !eval[bool](t, ctx, `window.__samePage === true`) {
		t.Error("step 1 was submitted by a full page load, want htmx with HX-Push-Url")
	}

	// The 422 variant: step 1 sent with its fields empty.
	open(t, ctx, 360, g.URL+"/_gallery/wizard-1")
	do(t, ctx, "mark the page", chromedp.Evaluate[chromedp.Void](`window.__samePage = true`))
	n := eval[int](t, ctx, `(() => {
  const els = document.querySelectorAll("#wizard form input:not([type=hidden]), #wizard form textarea");
  els.forEach(function (e) { e.value = ""; });
  return els.length;
})()`)
	if n == 0 {
		t.Fatal("wizard-1 has no field to leave empty")
	}
	m := g.mark()
	do(t, ctx, "submit step 1 empty", chromedp.Click(submit))
	waitFor(t, ctx, "the error summary", `document.querySelector("#wizard .error-summary") !== null`)
	waitFor(t, ctx, "focus on the error summary", `document.activeElement !== null && document.activeElement.closest(".error-summary") !== null`)
	if p := eval[string](t, ctx, `location.pathname`); p != "/_gallery/wizard-1" {
		t.Errorf("after a 422 the URL is %s, want /_gallery/wizard-1", p)
	}
	if !eval[bool](t, ctx, `window.__samePage === true`) {
		t.Error("the 422 came back as a full page load, want an htmx swap")
	}
	got422 := false
	for _, r := range g.since(m) {
		if r.Method == http.MethodPost && r.Status == http.StatusUnprocessableEntity {
			got422 = true
		}
	}
	if !got422 {
		t.Errorf("submitting step 1 empty got no 422; requests: %+v", g.since(m))
	}
}

func TestGalleryBrowser_DialogFocusRestored(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, w := range []int64{360, 1440} {
		open(t, ctx, w, g.URL+"/_gallery/dialogs")
		n := eval[int](t, ctx, `Array.from(document.querySelectorAll("main [data-open-dialog]")).filter(function (e) { return e.getClientRects().length > 0; }).length`)
		if n < 2 {
			t.Fatalf("dialogs@%d: %d visible openers in main, want the dialog's and the sheet's", w, n)
		}
		for i := range n {
			open(t, ctx, w, g.URL+"/_gallery/dialogs")
			markEl(t, ctx, `Array.from(document.querySelectorAll("main [data-open-dialog]")).filter(function (e) { return e.getClientRects().length > 0; })[`+strconv.Itoa(i)+`]`)
			id := eval[string](t, ctx, `document.querySelector("[data-test-target]").getAttribute("data-open-dialog")`)
			dlg := `document.getElementById(` + jsString(id) + `)`
			do(t, ctx, "open "+id, chromedp.Click(target))
			waitFor(t, ctx, id+" open", dlg+` !== null && `+dlg+`.open`)
			if !eval[bool](t, ctx, dlg+`.matches(":modal")`) {
				t.Errorf("%s@%d: dialog is not modal", id, w)
			}
			if leaked := eval[string](t, ctx, `(() => {
  const dlg = `+dlg+`;
  const out = Array.from(document.querySelectorAll("a[href], button, input:not([type=hidden]), select, textarea, summary")).filter(function (e) { return !dlg.contains(e) && e.getClientRects().length > 0; });
  for (const e of out) { e.focus(); if (document.activeElement === e) return e.outerHTML.slice(0, 120); }
  return "";
})()`); leaked != "" {
				t.Errorf("%s@%d: background is not inert: focused %s", id, w, leaked)
			}
			if !eval[bool](t, ctx, `(() => { const e = document.elementFromPoint(2, 2); return e !== null && e.closest("dialog") === `+dlg+`; })()`) {
				t.Errorf("%s@%d: the page behind the dialog takes pointer input", id, w)
			}
			do(t, ctx, "Esc", chromedp.KeyEvent(kb.Escape))
			waitFor(t, ctx, id+" closed", `!`+dlg+`.open`)
			waitFor(t, ctx, "focus back on the opener", `document.activeElement === document.querySelector("[data-test-target]")`)
		}
	}
}

func TestGalleryBrowser_ToastTextOnly(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	const payload = `<img src=x onerror=alert(1)>`
	do(t, ctx, "send an error notice", chromedp.Evaluate[chromedp.Void](
		`document.body.dispatchEvent(new CustomEvent("notice", {bubbles: true, detail: {level: "error", text: `+jsString(payload)+`}}))`))
	waitFor(t, ctx, "the notice in #alerts", `document.getElementById("alerts") !== null && document.getElementById("alerts").textContent.includes(`+jsString(payload)+`)`)
	if n := eval[int](t, ctx, `document.querySelectorAll("img[src=x]").length + document.querySelectorAll("#alerts img").length`); n != 0 {
		t.Errorf("the notice text became markup: %d img elements", n)
	}
	if eval[bool](t, ctx, `document.getElementById("toasts").textContent.includes(`+jsString(payload)+`)`) {
		t.Error("an error notice was shown as a toast (CD-75)")
	}

	const ok = "Saved the gallery sample."
	do(t, ctx, "send a success notice", chromedp.Evaluate[chromedp.Void](
		`document.body.dispatchEvent(new CustomEvent("notice", {bubbles: true, detail: {level: "success", text: `+jsString(ok)+`}}))`))
	waitFor(t, ctx, "the success toast", `document.getElementById("toasts").textContent.includes(`+jsString(ok)+`)`)
	do(t, ctx, "wait 5.5 s", chromedp.Sleep(5500*time.Millisecond))
	if eval[bool](t, ctx, `document.getElementById("toasts").textContent.includes(`+jsString(ok)+`)`) {
		t.Error("the success toast is still shown after 5.5 s")
	}
	if !eval[bool](t, ctx, `document.getElementById("alerts").textContent.includes(`+jsString(payload)+`)`) {
		t.Error("the error notice left #alerts on its own; errors stay until closed")
	}
}

func TestGalleryBrowser_DoubleSubmitSendsOnce(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 360, g.URL+"/_gallery/buttons")
	if !markEl(t, ctx, `Array.from(document.querySelectorAll("form[hx-post] button[type=submit]")).find(function (b) { return b.dataset.busyLabel === "Saving…"; })`) {
		t.Fatal(`buttons page: no htmx form with a "Saving…" submit button`)
	}
	path := eval[string](t, ctx, `new URL(document.querySelector("[data-test-target]").form.getAttribute("hx-post"), location.href).pathname`)
	m := g.mark()
	do(t, ctx, "double click", chromedp.DoubleClick(target))
	waitFor(t, ctx, "the busy button", `(() => { const b = document.querySelector("[data-test-target]"); return b !== null && b.disabled && b.textContent.trim() === "Saving…"; })()`)
	if n := countPosts(g.since(m), path); n != 0 {
		t.Errorf("the POST to %s had finished before the busy state was seen (handler must take 500 ms)", path)
	}
	deadline := time.Now().Add(5 * time.Second)
	for countPosts(g.since(m), path) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	time.Sleep(1200 * time.Millisecond) // a second, queued request would finish by now
	if n := countPosts(g.since(m), path); n != 1 {
		t.Errorf("double click sent %d POSTs to %s, want 1", n, path)
	}
}

func TestGalleryBrowser_RupeeUsesInter(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 1440, g.URL+"/_gallery/datalist")
	do(t, ctx, "add a rupee probe", chromedp.Evaluate[chromedp.Void](`(() => {
  const s = document.createElement("span");
  s.id = "rupee-probe";
  s.textContent = "₹";
  document.getElementById("main").append(s);
})()`))
	fctx, fcancel := step(ctx)
	defer fcancel()
	ok, err := chromedp.Run(fctx, chromedp.Evaluate[bool](`document.fonts.ready.then(function () { return document.fonts.check("16px Inter", "₹"); })`, chromedp.EvalAwaitPromise))
	if err != nil || !ok {
		t.Errorf(`document.fonts.check("16px Inter", "₹") = %v, %v; want true`, ok, err)
	}
	if !eval[bool](t, ctx, `Array.from(document.fonts).some(function (f) { return f.family.replace(/"/g, "") === "Inter" && f.status === "loaded"; })`) {
		t.Error("no loaded Inter font face")
	}

	sctx, cancel := step(ctx)
	defer cancel()
	nodes, err := chromedp.Run(sctx, chromedp.Nodes(chromedp.CSS("#rupee-probe")))
	if err != nil || len(nodes) == 0 {
		t.Fatalf("find #rupee-probe: %v", err)
	}
	if _, err := chromedp.Call(sctx, css.Enable, cdp.Empty{}); err != nil {
		t.Fatal(err)
	}
	res, err := chromedp.Call(sctx, css.GetPlatformFontsForNode, css.GetPlatformFontsForNodeParams{NodeID: nodes[0].NodeID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Fonts) == 0 {
		t.Fatal("no font rendered the rupee probe")
	}
	for _, f := range res.Fonts {
		if !f.IsCustomFont || !strings.Contains(f.FamilyName, "Inter") {
			t.Errorf("₹ rendered with %q (custom %v), want the bundled Inter", f.FamilyName, f.IsCustomFont)
		}
	}
}

func TestGalleryBrowser_PickerKeyboard(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 1440, g.URL+"/_gallery/picker")
	if !markEl(t, ctx, `Array.from(document.querySelectorAll("[role=combobox][hx-get]")).find(function (e) { return e.getClientRects().length > 0; })`) {
		t.Fatal("picker page: no visible combobox")
	}
	list := `document.getElementById(document.querySelector("[data-test-target]").getAttribute("aria-controls"))`
	do(t, ctx, `type "st"`, chromedp.Click(target), chromedp.SendKeys(target, "st"), chromedp.Sleep(700*time.Millisecond))
	waitFor(t, ctx, "two options", list+` !== null && `+list+`.querySelectorAll("[role=option][data-value]").length >= 2 && document.querySelector(".htmx-request") === null`)
	second := eval[map[string]string](t, ctx, `(() => { const o = `+list+`.querySelectorAll("[role=option][data-value]")[1]; return {id: o.id, value: o.dataset.value, label: o.dataset.label}; })()`)

	do(t, ctx, "↓ ↓", chromedp.KeyEvent(kb.ArrowDown), chromedp.KeyEvent(kb.ArrowDown))
	if got := eval[string](t, ctx, `document.querySelector("[data-test-target]").getAttribute("aria-activedescendant") || ""`); got != second["id"] {
		t.Errorf("aria-activedescendant = %q, want the second row %q", got, second["id"])
	}
	do(t, ctx, "Enter", chromedp.KeyEvent(kb.Enter))
	picker := `document.querySelector("[data-test-target]").closest(".picker")`
	if got := eval[string](t, ctx, picker+`.querySelector("[data-picker-value]").value`); got != second["value"] {
		t.Errorf("hidden input = %q, want the second row's id %q", got, second["value"])
	}
	if got := eval[string](t, ctx, picker+`.querySelector("[data-picker-summary]").textContent.trim()`); got != second["label"] {
		t.Errorf("summary = %q, want %q", got, second["label"])
	}
}

// TestGalleryBrowser_PickerPhoneSheet covers CD-133 (a): at 320 the open
// picker search is a full-screen panel that causes no horizontal overflow,
// no ancestor traps its fixed box, the page behind is inert and focus stays
// inside. It runs on every screen with a picker, which must include the
// picker page and a wizard step (and a line page once one is a screen).
func TestGalleryBrowser_PickerPhoneSheet(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	var checked []screens.Screen
	hasLinePage := false
	for _, s := range galleryScreens(t) {
		if strings.Contains(s.Route, "/lines/") {
			hasLinePage = true
		}
		if !visit(t, ctx, g.URL, s, 320, "") {
			continue
		}
		kind := eval[string](t, ctx, `(() => {
  document.querySelectorAll("[data-test-target]").forEach(function (e) { e.removeAttribute("data-test-target"); });
  const vis = function (e) { return e.getClientRects().length > 0; };
  const c = Array.from(document.querySelectorAll("[role=combobox]")).find(vis);
  if (c) { c.setAttribute("data-test-target", ""); return "combobox"; }
  const ch = Array.from(document.querySelectorAll("[data-picker-change]")).find(vis);
  if (ch) { ch.setAttribute("data-test-target", ""); return "change"; }
  return "";
})()`)
		if kind == "" {
			continue
		}
		checked = append(checked, s)
		do(t, ctx, s.Name+": open the picker", chromedp.Click(target))
		waitFor(t, ctx, s.Name+": the open search panel", `document.querySelector("[data-picker-search][data-open]") !== null`)
		markEl(t, ctx, `document.querySelector("[data-picker-search][data-open] [role=combobox]")`)
		if strings.HasPrefix(s.URL, "/_gallery/picker") {
			do(t, ctx, `type "st"`, chromedp.SendKeys(target, "st"))
			waitFor(t, ctx, "options in the panel", `document.querySelector("[data-picker-search][data-open] [role=option]") !== null`)
		}

		probs, err := Overflow(ctx)
		expectNone(t, s.Name+" (picker open)", 320, "overflow", probs, err)

		box := eval[[]float64](t, ctx, `(() => {
  const r = document.querySelector("[data-picker-search][data-open]").getBoundingClientRect();
  return [r.left, r.top, r.width, r.height, document.documentElement.clientWidth, window.innerHeight];
})()`)
		if abs(box[0]) > 1 || abs(box[1]) > 1 || abs(box[2]-box[4]) > 1 || abs(box[3]-box[5]) > 1 {
			t.Errorf("%s@320: picker panel box [left %v top %v %vx%v], want the full %vx%v viewport (an ancestor traps the fixed panel?)",
				s.Name, box[0], box[1], box[2], box[3], box[4], box[5])
		}

		const panelOpen = `document.querySelector("[data-picker-search][data-open]") !== null`
		if !eval[bool](t, ctx, `(() => { const p = document.querySelector("[data-picker-search][data-open]"); const e = document.elementFromPoint(2, window.innerHeight - 2); return p !== null && p.contains(e); })()`) {
			t.Errorf("%s@320: the page behind the picker panel takes pointer input at its bottom-left corner: %s", s.Name,
				eval[string](t, ctx, `(() => { const e = document.elementFromPoint(2, window.innerHeight - 2); return e ? e.outerHTML.slice(0, 120) : "nothing"; })()`))
		}
		trapped := true
		for i := range 8 {
			do(t, ctx, "Tab", chromedp.KeyEvent(kb.Tab))
			if !eval[bool](t, ctx, panelOpen+` && document.querySelector("[data-picker-search][data-open]").contains(document.activeElement)`) {
				t.Errorf("%s@320: Tab %d moved focus out of the open picker panel to %s", s.Name, i+1,
					eval[string](t, ctx, `document.activeElement ? document.activeElement.outerHTML.slice(0, 120) : "nothing"`))
				trapped = false
				break
			}
		}
		if !trapped {
			continue // the panel may have closed; the inert check needs it open
		}
		if leaked := eval[string](t, ctx, `(() => {
  const p = document.querySelector("[data-picker-search][data-open]");
  if (!p) return "nothing: the panel closed";
  const out = Array.from(document.querySelectorAll("a[href], button, input:not([type=hidden]), select, textarea, summary")).filter(function (e) { return !p.contains(e) && e.getClientRects().length > 0; });
  for (const e of out) { e.focus(); if (document.activeElement === e) return e.outerHTML.slice(0, 120); }
  return "";
})()`); leaked != "" {
			t.Errorf("%s@320: the page behind the picker panel is not inert: focused %s", s.Name, leaked)
		}
	}

	names := func(f func(screens.Screen) bool) bool { return slices.ContainsFunc(checked, f) }
	if !names(func(s screens.Screen) bool { return strings.HasPrefix(s.URL, "/_gallery/picker") }) {
		t.Error("the picker page was not checked with an open picker")
	}
	if !names(func(s screens.Screen) bool {
		return strings.HasPrefix(s.URL, "/_gallery/wizard-") || strings.Contains(s.Route, "/edit/{step}")
	}) {
		t.Error("no wizard step has a picker; CD-133 checks the panel inside a wizard step")
	}
	if hasLinePage && !names(func(s screens.Screen) bool { return strings.Contains(s.Route, "/lines/") }) {
		t.Error("no line page was checked with an open picker (CD-133)")
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// gridQty marks the first grid row's qty input on the lines page at 1440
// and returns its form's id and the row's save path.
func gridQty(t *testing.T, ctx context.Context, base string) (formID, post string) {
	t.Helper()
	open(t, ctx, 1440, base+"/_gallery/lines")
	if !markEl(t, ctx, `document.querySelector('#line-editor form[id^="grid-row-"] input[id$="-qty"]')`) {
		t.Fatal("lines page: no grid qty input (id grid-<no>-qty)")
	}
	if !eval[bool](t, ctx, `document.querySelector("[data-test-target]").getClientRects().length > 0`) {
		t.Fatal("lines page@1440: the line grid is not shown")
	}
	formID = eval[string](t, ctx, `document.querySelector("[data-test-target]").form.id`)
	post = eval[string](t, ctx, `new URL(document.querySelector("[data-test-target]").form.getAttribute("hx-post"), location.href).pathname`)
	return formID, post
}

func TestGalleryBrowser_LineGridCtrlEnterKeepsRefusedEdit(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	formID, post := gridQty(t, ctx, g.URL)
	if !eval[bool](t, ctx, `document.querySelector("[data-add-line]") !== null`) {
		t.Fatal("lines page: no [data-add-line] control")
	}
	before := eval[string](t, ctx, `location.href`)
	do(t, ctx, "mark the page", chromedp.Evaluate[chromedp.Void](`window.__samePage = true`))
	m := g.mark()

	do(t, ctx, `type qty "bad"`, clearTarget, chromedp.SendKeys(target, "bad"))
	do(t, ctx, "Ctrl+Enter", chromedp.KeyEvent(kb.Enter, chromedp.KeyModifiers(kb.ModifierCtrl)))
	form := `document.getElementById(` + jsString(formID) + `)`
	waitFor(t, ctx, "focus on the refused input", `(() => {
  const f = `+form+`, a = document.activeElement;
  return f !== null && a !== null && f.contains(a) && a.getAttribute("aria-invalid") === "true";
})()`)
	do(t, ctx, "wait for a late navigation", chromedp.Sleep(800*time.Millisecond))

	if !eval[bool](t, ctx, `window.__samePage === true`) {
		t.Fatal("Ctrl+Enter after a refused edit left the page")
	}
	if got := eval[string](t, ctx, `location.href`); got != before {
		t.Errorf("Ctrl+Enter after a refused edit moved to %s, want %s", got, before)
	}
	if !eval[bool](t, ctx, `(() => { const a = document.activeElement; return a !== null && a.getAttribute("aria-invalid") === "true" && `+form+`.contains(a); })()`) {
		t.Error("focus left the refused input")
	}
	refused := false
	for _, r := range g.since(m) {
		if r.Method == http.MethodPost && r.Path == post && r.Status == http.StatusUnprocessableEntity {
			refused = true
		}
	}
	if !refused {
		t.Errorf("the row save to %s was not refused with 422; requests: %+v", post, g.since(m))
	}
}

func TestGalleryBrowser_LineGridEnterSkipsCombobox(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	open(t, ctx, 1440, g.URL+"/_gallery/lines")
	kind := eval[string](t, ctx, `(() => {
  const vis = function (e) { return e.getClientRects().length > 0; };
  const c = Array.from(document.querySelectorAll('#line-editor form[id^="grid-row-"] [role=combobox]')).find(vis);
  if (c) { c.setAttribute("data-test-target", ""); return "combobox"; }
  const ch = Array.from(document.querySelectorAll('#line-editor form[id^="grid-row-"] [data-picker-change]')).find(vis);
  if (ch) { ch.setAttribute("data-test-target", ""); return "change"; }
  return "";
})()`)
	if kind == "" {
		t.Skip("the line grid has no picker cell")
	}
	if kind == "change" {
		do(t, ctx, "Change the item", chromedp.Click(target))
		if !markEl(t, ctx, `document.querySelector("[data-test-target]").closest(".picker").querySelector("[role=combobox]")`) {
			t.Fatal("the grid picker has no combobox after Change")
		}
	}
	formID := eval[string](t, ctx, `document.querySelector("[data-test-target]").closest("form").id`)
	before := eval[string](t, ctx, `location.href`)
	list := `document.getElementById(document.querySelector("[data-test-target]").getAttribute("aria-controls"))`
	do(t, ctx, `type "st"`, chromedp.Click(target), chromedp.SendKeys(target, "st"), chromedp.Sleep(700*time.Millisecond))
	waitFor(t, ctx, "options", list+` !== null && `+list+`.querySelector("[role=option][data-value]") !== null && document.querySelector(".htmx-request") === null`)
	first := eval[string](t, ctx, list+`.querySelector("[role=option][data-value]").dataset.value`)
	do(t, ctx, "↓", chromedp.KeyEvent(kb.ArrowDown))
	do(t, ctx, "record focus and choice", chromedp.Evaluate[chromedp.Void](`(() => {
  window.__focusForms = [];
  window.__chosen = null;
  document.addEventListener("focusin", function (e) { const f = e.target.closest("form"); window.__focusForms.push(f ? f.id : ""); }, true);
  document.addEventListener("change", function (e) { if (e.target.matches("[data-picker-value]")) window.__chosen = e.target.value; }, true);
})()`))
	do(t, ctx, "Enter", chromedp.KeyEvent(kb.Enter), chromedp.Sleep(400*time.Millisecond))

	if got := eval[string](t, ctx, `window.__chosen === null ? "" : window.__chosen`); got != first {
		t.Errorf("Enter in the grid picker chose %q, want the active row %q", got, first)
	}
	for _, f := range eval[[]string](t, ctx, `window.__focusForms`) {
		if f != formID && f != "" {
			t.Errorf("Enter in the grid picker moved focus to row form %q, want it kept in %q", f, formID)
		}
	}
	if got := eval[string](t, ctx, `location.href`); got != before {
		t.Errorf("Enter in the grid picker moved to %s", got)
	}
}
