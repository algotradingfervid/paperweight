//go:build e2e

package uibrowser

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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
	if vs, err := Axe(ctx); err != nil || len(vs) == 0 {
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
