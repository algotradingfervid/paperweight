//go:build e2e

package uibrowser

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui"
)

// chromePage has the app's sticky header and action bar with the app CSS. At
// 360 × 800 the date field runs past the bottom under the action bar's
// buttons at load, with its quick-date button just below it, as on the PO
// details step in P16; the two 16 px buttons are really too small and too
// close.
const chromePage = `<!doctype html><html lang="en"><head><meta name="viewport" content="width=device-width"><title>Chrome</title>
<link rel="stylesheet" href="` + ui.CSSURL + `">
<style>main{padding-inline:16px}.gap{block-size:660px}.tall{block-size:900px}.tiny{inline-size:16px;block-size:16px;padding:0;margin:0}</style></head>
<body><header class="app-header"><button type="button" class="btn tap">Menu</button></header>
<main class="app-main"><form class="stack"><div class="gap"></div>
<div class="fieldset field"><input type="text" class="input field__input" id="f-date" aria-label="Order date"><div class="field__actions"><button type="button" class="btn btn-outline tap">Today</button> <button type="button" class="btn btn-outline tap">Yesterday</button></div></div>
<div class="tall"></div><p><button type="button" class="tiny" id="t1" aria-label="One"></button><button type="button" class="tiny" id="t2" aria-label="Two"></button></p>
<div class="actionbar"><button type="submit" class="btn tap btn-primary btn--primary"><span>Save and continue</span></button></div></form></main></body></html>`

// CD-344 (3): during Axe the sticky header and action bar are static, so a
// field under the bar at load isn't a target-size failure, while a really
// small target still is; afterwards the page is as it was.
func TestAxe_StickyChromeIsNotAnOverlap(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("/static/ui/", http.StripPrefix("/static/ui/", ui.Static()))
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(chromePage))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := New(t)
	if err := At(ctx, 360, srv.URL+"/"); err != nil {
		t.Fatal(err)
	}
	covered, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
  const f = document.getElementById("f-date").getBoundingClientRect(), b = document.querySelector(".actionbar").getBoundingClientRect();
  return getComputedStyle(document.querySelector(".actionbar")).position === "sticky" && f.top < b.bottom && f.bottom > b.top;
})()`))
	if err != nil || !covered {
		t.Fatalf("the date field is not under the sticky action bar at load (%v): the test proves nothing", err)
	}

	during, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](axeTopJS+`.then(() => [getComputedStyle(document.querySelector(".app-header")).position, getComputedStyle(document.querySelector(".actionbar")).position])`, chromedp.EvalAwaitPromise))
	if err != nil || !slices.Equal(during, []string{"static", "static"}) {
		t.Errorf("while axe runs: header and bar position = %q, %v; want [static static]", during, err)
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](axeDoneJS)); err != nil {
		t.Fatal(err)
	}

	vs, err := withDeadline(ctx, AxeTimeout, "axe", Axe)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []string
	for _, v := range vs {
		if v.ID != "target-size" {
			t.Errorf("axe %s: %s: %s", v.ID, v.Help, strings.Join(v.Nodes, ", "))
			continue
		}
		nodes = append(nodes, v.Nodes...)
	}
	slices.Sort(nodes)
	if !slices.Equal(nodes, []string{"#t1", "#t2"}) {
		t.Errorf("target-size nodes = %q, want the two tiny buttons only", nodes)
	}

	after, err := chromedp.Run(ctx, chromedp.Evaluate[[]string](`[String(document.documentElement.hasAttribute("data-axe")),
  getComputedStyle(document.querySelector(".app-header")).position, getComputedStyle(document.querySelector(".actionbar")).position]`))
	if err != nil || !slices.Equal(after, []string{"false", "sticky", "sticky"}) {
		t.Errorf("after Axe: data-axe, header and bar position = %q, %v; want [false sticky sticky]", after, err)
	}
}
