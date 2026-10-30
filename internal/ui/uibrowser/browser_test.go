//go:build e2e

package uibrowser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// Pages from research frontend-tech.md §6.4.
const (
	code60   = "27AAPFU0939F1ZV27AAPFU0939F1ZV27AAPFU0939F1ZV27AAPFU0939F1ZV"
	badPage  = `<!doctype html><html><head><meta name="viewport" content="width=device-width"><style>.box{width:400px}</style></head><body><div class="box">box</div><p>` + code60 + `</p></body></html>`
	goodPage = `<!doctype html><html><head><meta name="viewport" content="width=device-width"><style>body{overflow-wrap:anywhere}.box{max-width:100%}button{width:48px;height:48px}</style></head><body><div class="box">box</div><p>` + code60 + `</p><button>Go</button></body></html>`
	cspPage  = `<!doctype html><html><body><script>document.title = "x"</script></body></html>`
	tinyPage = `<!doctype html><html><head><meta name="viewport" content="width=device-width"><style>button{width:20px;height:20px;padding:0}</style></head><body><button id="tiny">x</button></body></html>`
)

func TestBrowserHelpers_CatchBadPage(t *testing.T) {
	pages := map[string]string{"/bad": badPage, "/good": goodPage, "/tiny": tinyPage, "/csp": cspPage}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/csp" {
			w.Header().Set("Content-Security-Policy", "script-src 'self'")
		}
		_, _ = w.Write([]byte(pages[r.URL.Path]))
	}))
	defer srv.Close()
	ctx := New(t)
	csp := CSPProblems(ctx)

	if err := At(ctx, 320, srv.URL+"/bad"); err != nil {
		t.Fatal(err)
	}
	if probs, err := Overflow(ctx); err != nil || len(probs) == 0 {
		t.Errorf("Overflow(/bad@320) = %q, %v; want problems", probs, err)
	}

	for _, w := range []int64{320, 1440} {
		if err := At(ctx, w, srv.URL+"/good"); err != nil {
			t.Fatal(err)
		}
		if probs, err := Overflow(ctx); err != nil || len(probs) != 0 {
			t.Errorf("Overflow(/good@%d) = %q, %v; want none", w, probs, err)
		}
		if small, err := SmallTargets(ctx); err != nil || len(small) != 0 {
			t.Errorf("SmallTargets(/good@%d) = %q, %v; want none", w, small, err)
		}
	}
	if vs, err := withDeadline(ctx, AxeTimeout, "axe /good", Axe); err != nil || len(vs) == 0 {
		t.Errorf("Axe(/good) = %+v, %v; want violations (no lang, no title)", vs, err)
	}
	if p := csp(); len(p) != 0 {
		t.Errorf("CSP problems before /csp: %q", p)
	}

	if err := At(ctx, 360, srv.URL+"/tiny"); err != nil {
		t.Fatal(err)
	}
	if small, err := SmallTargets(ctx); err != nil || len(small) != 1 || small[0] != "button#tiny 20x20" {
		t.Errorf("SmallTargets(/tiny) = %q, %v; want [button#tiny 20x20]", small, err)
	}

	if err := At(ctx, 360, srv.URL+"/csp"); err != nil {
		t.Fatal(err)
	}
	var p []string
	for range 50 {
		if p = csp(); len(p) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(p) == 0 {
		t.Error("CSPProblems saw no report for a blocked inline script")
	}
}

// A screen for a user can't be checked without a way to sign in, and a screen
// is opened only after SignIn has given the browser its user's session.
func TestCheck_SignsInBeforeOpening(t *testing.T) {
	var seen atomic.Pointer[string]
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("who"); err == nil {
			seen.Store(&c.Value)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(goodPage))
	}))
	defer srv.Close()
	ctx := New(t)
	s := screens.Screen{Name: "page", URL: "/page", As: "priya@vardhan.test"}

	opt := Options{Widths: []int64{360}, Overflow: true}
	if got := Problems(ctx, s, srv.URL, opt); len(got) != 1 || !strings.Contains(got[0], "no SignIn") {
		t.Fatalf("Problems with no SignIn = %q, want one saying so", got)
	}

	opt.SignIn = func(ctx context.Context, email string) error {
		if err := chromedp.Do(ctx, chromedp.Navigate(srv.URL+"/signin")); err != nil {
			return err
		}
		return chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`document.cookie = "who=`+strings.Split(email, "@")[0]+`"`))
	}
	if got := Problems(ctx, s, srv.URL, opt); len(got) != 0 {
		t.Fatalf("Problems = %q, want none", got)
	}
	if p := seen.Load(); p == nil || *p != "priya" {
		t.Errorf("the page saw cookie %v, want priya's", p)
	}

	opt.SignIn = func(context.Context, string) error { return errors.New("wrong password") }
	if got := Problems(ctx, s, srv.URL, opt); len(got) != 1 || !strings.Contains(got[0], "wrong password") {
		t.Errorf("Problems with a failing SignIn = %q, want the error", got)
	}
}

// Each check reports under its own name, and an option left off is not run.
func TestCheck_RunsWhatOptionsAsk(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		page := badPage
		if r.URL.Path == "/tiny" {
			page = tinyPage
		}
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()
	ctx := New(t)
	bad, tiny := screens.Screen{Name: "bad", URL: "/bad"}, screens.Screen{Name: "tiny", URL: "/tiny"}

	has := func(got []string, what string) bool {
		return slices.ContainsFunc(got, func(p string) bool { return strings.Contains(p, what) })
	}
	if got := Problems(ctx, bad, srv.URL, Options{Widths: []int64{320}, Overflow: true}); !has(got, "bad@320: overflow") {
		t.Errorf("overflow on: %q", got)
	}
	if got := Problems(ctx, bad, srv.URL, Options{Widths: []int64{320}}); len(got) != 0 {
		t.Errorf("nothing on: %q", got)
	}
	if got := Problems(ctx, tiny, srv.URL, Options{Widths: []int64{360}, Targets: true}); !has(got, "tiny@360: target") {
		t.Errorf("targets on: %q", got)
	}
	if got := Problems(ctx, tiny, srv.URL, Options{Widths: []int64{360}, Axe: true}); !has(got, "tiny@360: axe") {
		t.Errorf("axe on: %q", got)
	}
	csp := func() []string { return []string{"a report"} }
	if got := Problems(ctx, tiny, srv.URL, Options{Widths: []int64{360}, CSP: csp}); !has(got, "tiny@360: CSP: a report") {
		t.Errorf("CSP on: %q", got)
	}
}
