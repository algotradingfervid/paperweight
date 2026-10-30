//go:build e2e

package gallery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// clearStep bounds every browser step of these tests.
const clearStep = 5 * time.Second

const (
	supplierValue   = `document.querySelector('[name="supplier_id"]').value`
	supplierSummary = `document.querySelector("[data-picker-summary]").textContent`
	clearVisible    = `document.querySelector("[data-picker-clear]").getClientRects().length > 0`
	panelOpen       = `document.querySelector("[data-picker-search][data-open]") !== null`
	onPicker        = `document.activeElement === document.querySelector("[data-picker-search]").closest(".picker")`
	inertCount      = `document.querySelectorAll("[inert]").length`
	submitStep      = `(() => {
  const b = document.querySelector("#wizard form button[type=submit]:not([name=nav])");
  b.form.requestSubmit(b);
})()`
)

// postLog is the gallery behind a recorder of wizard step 1's supplier_id.
type postLog struct {
	*httptest.Server
	mu    sync.Mutex
	posts []string
}

func startPostLog(t *testing.T) *postLog {
	t.Helper()
	l := &postLog{}
	h := Handler()
	l.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/_gallery/wizard-1" {
			v, sent := "", false
			if err := r.ParseForm(); err == nil {
				var vs []string
				vs, sent = r.PostForm["supplier_id"]
				if sent {
					v = vs[0]
				}
			}
			if !sent {
				v = "<missing>"
			}
			l.mu.Lock()
			l.posts = append(l.posts, v)
			l.mu.Unlock()
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(l.Close)
	return l
}

func (l *postLog) last() (string, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.posts) == 0 {
		return "", false
	}
	return l.posts[len(l.posts)-1], true
}

// act runs one step under clearStep.
func act(t *testing.T, ctx context.Context, what string, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, clearStep)
	defer cancel()
	if err := chromedp.Do(sctx, steps...); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func value[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, clearStep)
	defer cancel()
	v, err := chromedp.Run(sctx, chromedp.Evaluate[T](expr))
	if err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	return v
}

func until(t *testing.T, ctx context.Context, what, expr string) {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, clearStep)
	defer cancel()
	if _, err := chromedp.Run(sctx, chromedp.Poll[chromedp.Void](expr, chromedp.WithPollingTimeout(clearStep))); err != nil {
		t.Fatalf("waiting for %s: %v", what, err)
	}
}

func openAt(t *testing.T, ctx context.Context, w int, url string) {
	t.Helper()
	if err := uibrowser.At(ctx, int64(w), url); err != nil {
		t.Fatal(err)
	}
}

// submit sends wizard step 1 and returns the supplier_id it posted.
func submit(t *testing.T, ctx context.Context, l *postLog) string {
	t.Helper()
	act(t, ctx, "submit step 1", chromedp.Evaluate[chromedp.Void](submitStep))
	until(t, ctx, "step 2", `location.pathname === "/_gallery/wizard-2" && document.querySelector(".htmx-request") === null`)
	got, ok := l.last()
	if !ok {
		t.Fatal("step 1 posted nothing")
	}
	return got
}

// TestPickerClear_PickClearSubmit: inside a wizard step, on a desktop and
// on a phone, a chosen row can be cleared and the save posts it blank.
func TestPickerClear_PickClearSubmit(t *testing.T) {
	for _, w := range []int{1440, 320} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			l := startPostLog(t)
			ctx := uibrowser.New(t)
			openAt(t, ctx, w, l.URL+"/_gallery/wizard-1")
			if value[bool](t, ctx, clearVisible) {
				t.Error("Clear is visible with nothing chosen")
			}
			act(t, ctx, "count change events", chromedp.Evaluate[chromedp.Void](`window.__changes = 0; document.addEventListener("change", function (e) { if (e.target.matches("[data-picker-value]")) window.__changes++; })`))

			act(t, ctx, `type "st"`, chromedp.Click(chromedp.CSS("#f-supplier_id")), chromedp.SendKeys(chromedp.CSS("#f-supplier_id"), "st"))
			until(t, ctx, "rows for st", `document.querySelector("#f-supplier_id-list [data-value]") !== null && document.querySelector('#f-supplier_id-list [data-value="4"]') === null && document.querySelector(".htmx-request") === null`)
			act(t, ctx, "choose the first row", chromedp.Click(chromedp.CSS("#f-supplier_id-list [data-value]")))
			until(t, ctx, "the summary", clearVisible)
			if value[string](t, ctx, supplierValue) == "" {
				t.Fatal("choosing a row set no value")
			}

			act(t, ctx, "Clear", chromedp.Click(chromedp.CSS("[data-picker-clear]")))
			if got := value[string](t, ctx, supplierValue); got != "" {
				t.Errorf("hidden value = %q after Clear", got)
			}
			if got := value[string](t, ctx, supplierSummary); got != "" {
				t.Errorf("summary = %q after Clear", got)
			}
			if value[bool](t, ctx, clearVisible) {
				t.Error("Clear is still visible after Clear")
			}
			if !value[bool](t, ctx, `document.querySelector("[data-picker-chosen]").getClientRects().length === 0 && document.querySelector("#f-supplier_id").getClientRects().length > 0`) {
				t.Error("Clear did not return the picker to its search")
			}
			if !value[bool](t, ctx, onPicker) {
				t.Errorf("focus is on %s after Clear, want the picker", value[string](t, ctx, `document.activeElement.outerHTML.slice(0, 80)`))
			}
			if value[bool](t, ctx, panelOpen) {
				t.Error("Clear left the search open, want it closed")
			}
			if n := value[int](t, ctx, inertCount); n != 0 {
				t.Errorf("%d elements inert after Clear", n)
			}
			if got := value[int](t, ctx, `window.__changes`); got != 2 {
				t.Errorf("hidden input fired %d change events for a choice and a Clear, want 2", got)
			}
			if got := value[string](t, ctx, `document.querySelector("#f-supplier_id").value`); got != "" {
				t.Errorf("search text = %q after Clear, want it empty", got)
			}
			until(t, ctx, "rows for the empty search", `document.querySelector('#f-supplier_id-list [data-value="4"]') !== null`)

			if w == 320 {
				act(t, ctx, "open the panel", chromedp.Click(chromedp.CSS("#f-supplier_id")))
				until(t, ctx, "the open panel", panelOpen)
				act(t, ctx, "Close", chromedp.Click(chromedp.CSS("[data-picker-search][data-open] [data-picker-close]")))
				until(t, ctx, "the panel closed", `!(`+panelOpen+`)`)
				if n := value[int](t, ctx, inertCount); n != 0 {
					t.Errorf("%d elements still inert after Close", n)
				}
				if !value[bool](t, ctx, onPicker) {
					t.Errorf("focus is on %s after Close with no value, want the picker", value[string](t, ctx, `document.activeElement.outerHTML.slice(0, 80)`))
				}
				if value[string](t, ctx, supplierValue) != "" || value[bool](t, ctx, clearVisible) {
					t.Error("Close after Clear brought the old choice back")
				}
			}
			if got := submit(t, ctx, l); got != "" {
				t.Errorf("posted supplier_id = %q, want blank", got)
			}
		})
	}
}

// TestPickerClear_ChangeThenCloseRestores: with a saved choice, "Change"
// followed by close keeps it, so it still posts.
func TestPickerClear_ChangeThenCloseRestores(t *testing.T) {
	for _, w := range []int{1440, 320} {
		t.Run(strconv.Itoa(w), func(t *testing.T) {
			l := startPostLog(t)
			ctx := uibrowser.New(t)
			openAt(t, ctx, w, l.URL+"/_gallery/wizard-1?supplier=3")
			if !value[bool](t, ctx, clearVisible) {
				t.Fatal("Clear is not visible with a saved choice")
			}
			// Two buttons beside a phone's summary leave it too narrow to read.
			stacked := value[bool](t, ctx, `document.querySelector("[data-picker-clear]").getBoundingClientRect().top >= document.querySelector("[data-picker-summary]").getBoundingClientRect().bottom`)
			if stacked != (w == 320) {
				t.Errorf("buttons under the summary = %v at %d, want them there only on a phone", stacked, w)
			}
			want := value[string](t, ctx, supplierSummary)
			act(t, ctx, "Change", chromedp.Click(chromedp.CSS("[data-picker-change]")))
			until(t, ctx, "the search", `document.activeElement === document.querySelector("#f-supplier_id")`)
			if w == 320 {
				act(t, ctx, "Close", chromedp.Click(chromedp.CSS("[data-picker-search][data-open] [data-picker-close]")))
			} else {
				act(t, ctx, "Esc", chromedp.KeyEvent(kb.Escape))
			}
			until(t, ctx, "the summary back", clearVisible)
			if got := value[string](t, ctx, supplierSummary); got != want {
				t.Errorf("summary = %q, want %q", got, want)
			}
			if got := submit(t, ctx, l); got != "3" {
				t.Errorf("posted supplier_id = %q, want 3", got)
			}
		})
	}
}
