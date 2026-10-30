//go:build e2e

package gallery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// rowsStep is one browser step's deadline.
const rowsStep = 5 * time.Second

// rowsServer is gallery.Handler recording the forms posted to the rows page.
type rowsServer struct {
	*httptest.Server
	mu    sync.Mutex
	posts []url.Values
}

func startRows(t *testing.T) *rowsServer {
	t.Helper()
	s := &rowsServer{}
	h := Handler()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/_gallery/rows" {
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			s.mu.Lock()
			s.posts = append(s.posts, r.PostForm)
			s.mu.Unlock()
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *rowsServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.posts)
}

// waitPost waits for the post after the first n and returns it.
func (s *rowsServer) waitPost(t *testing.T, n int) url.Values {
	t.Helper()
	for deadline := time.Now().Add(rowsStep); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if s.count() > n {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.posts[n]
		}
	}
	t.Fatal("no POST arrived")
	return nil
}

func rowsDo(t *testing.T, ctx context.Context, what string, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, rowsStep)
	defer cancel()
	if err := chromedp.Do(sctx, steps...); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func rowsEval[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, rowsStep)
	defer cancel()
	v, err := chromedp.Run(sctx, chromedp.Evaluate[T](expr))
	if err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	return v
}

// TestRowsBrowser_Submitters drives the real browser path in a plain and an
// htmx form: Add and Remove post their own name and value past an empty
// required field, and Enter in a field saves, validated, with no row action.
func TestRowsBrowser_Submitters(t *testing.T) {
	s := startRows(t)
	ctx := uibrowser.New(t)
	for _, c := range []struct{ name, form, field, remove string }{
		{"plain", "form#rows-plain", "lines[1].unit", "remove:1"},
		{"htmx", "form#rows-hx", "lines[4].unit", "remove:4"},
		{"field above the editor", "form#rows-plain", "name", "remove:1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			field := `document.querySelector(` + strconv.Quote(c.form+` input[name="`+c.field+`"]`) + `)`
			emptyRequired := chromedp.Evaluate[chromedp.Void](`(() => { const e = ` + field + `; e.required = true; e.value = ""; })()`)
			open := func() {
				t.Helper()
				if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/rows"); err != nil {
					t.Fatal(err)
				}
				rowsDo(t, ctx, "empty the required field", emptyRequired)
			}

			for value, sel := range map[string]string{
				"add":    `button[name="_rows"][value="add"]`,
				c.remove: `button[name="_rows"][value="` + c.remove + `"]`,
			} {
				open()
				n := s.count()
				rowsDo(t, ctx, "click "+value, chromedp.Click(chromedp.CSS(c.form+" "+sel)))
				got := s.waitPost(t, n)
				if v := got.Get("_rows"); v != value {
					t.Errorf("click %s posted _rows=%q", value, v)
				}
			}

			open()
			n := s.count()
			rowsDo(t, ctx, "Enter with the required field empty",
				chromedp.Evaluate[chromedp.Void](field+`.focus()`), chromedp.KeyEvent(kb.Enter), chromedp.Sleep(400*time.Millisecond))
			if got := s.count(); got != n {
				t.Fatalf("Enter posted with a required field empty: %v", s.waitPost(t, n))
			}

			rowsDo(t, ctx, "Enter with the field filled",
				chromedp.Evaluate[chromedp.Void](field+`.value = "box"`), chromedp.KeyEvent(kb.Enter))
			got := s.waitPost(t, n)
			if _, ok := got["_rows"]; ok {
				t.Errorf("Enter posted _rows=%q, want a plain save", got.Get("_rows"))
			}
			if v := got.Get(c.field); v != "box" {
				t.Errorf("Enter posted %s=%q", c.field, v)
			}
		})
	}
}

// TestRowsBrowser_LayoutAndNumbers: cards on a phone, aligned rows once the
// editor is 48rem wide, and nothing clipped or overflowing, also in the band
// where the editor has just reached 48rem, which screens.Widths steps over.
func TestRowsBrowser_LayoutAndNumbers(t *testing.T) {
	s := startRows(t)
	ctx := uibrowser.New(t)
	widths := screens.Widths[:len(screens.Widths):len(screens.Widths)]
	for w := int64(1040); w <= 1200; w += 16 {
		widths = append(widths, w)
	}
	aligned := 0
	for _, w := range widths {
		if err := uibrowser.At(ctx, w, s.URL+"/_gallery/rows"); err != nil {
			t.Fatal(err)
		}
		wide := rowsEval[bool](t, ctx, `document.querySelector(".rows-editor").clientWidth >= 48 * parseFloat(getComputedStyle(document.documentElement).fontSize)`)
		head := rowsEval[bool](t, ctx, `getComputedStyle(document.querySelector(".rows-head")).display !== "none"`)
		if head != wide {
			t.Errorf("%d: head shown = %v, editor at least 48rem wide = %v", w, head, wide)
		}
		if wide {
			aligned++
			for _, bad := range rowsEval[[]string](t, ctx, `(() => {
  const ed = document.querySelector(".rows-editor"), cols = Array.from(ed.querySelector(".rows-head").children), row = Array.from(ed.querySelector(".rows-row").children);
  return cols.filter(function (c, i) { return Math.abs(c.getBoundingClientRect().left - row[i].getBoundingClientRect().left) > 1; }).map(function (c) { return c.textContent; });
})()`) {
				t.Errorf("%d: head column %q is not over its cell", w, bad)
			}
		}
		for _, bad := range rowsEval[[]string](t, ctx, `Array.from(document.querySelectorAll(".rows-editor input.num")).filter(function (e) { return e.scrollWidth > e.clientWidth + 1; }).map(function (e) { return e.id + " " + e.scrollWidth + ">" + e.clientWidth; })`) {
			t.Errorf("%d: number clipped: %s", w, bad)
		}
		// CD-203: Item's factor at its longest, shown and whole in every row.
		if n := rowsEval[int](t, ctx, `Array.from(document.querySelectorAll(".rows-editor input.num")).filter(function (e) { return e.value === "9,99,999.999999" && e.getClientRects().length > 0 && e.scrollWidth <= e.clientWidth + 1; }).length`); n != 5 {
			t.Errorf("%d: %d of 5 rows show 9,99,999.999999 unclipped", w, n)
		}
		// The busy label is the widest Remove gets.
		rowsDo(t, ctx, "show the busy labels", chromedp.Evaluate[chromedp.Void](`document.querySelectorAll('.rows-actions button').forEach(function (b) { b.querySelector("span").textContent = b.dataset.busyLabel; })`))
		probs, err := uibrowser.Overflow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range probs {
			t.Errorf("%d: %s", w, p)
		}
		for _, bad := range rowsEval[[]string](t, ctx, `Array.from(document.querySelectorAll('.rows-actions button')).filter(function (b) { return b.getBoundingClientRect().right > b.parentElement.getBoundingClientRect().right + 0.5; }).map(function (b) { return b.value; })`) {
			t.Errorf("%d: busy %s is wider than its track", w, bad)
		}
	}
	if aligned == 0 {
		t.Error("no width showed aligned rows: the gallery no longer proves them")
	}
}
