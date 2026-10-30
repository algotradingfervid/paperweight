//go:build e2e

package uibrowser

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/screens"
)

// Header search in the browser (design-system §6, §10, CD-200 part 4). Every
// step goes through At, eval, do or waitFor, which carry a step deadline.

const (
	headerSearch      = `header form[action="/search"]`
	headerSearchInput = headerSearch + ` input[name="q"]`

	// searchFormState is "missing", "hidden" (no boxes, so it takes no room
	// and no focus) or "shown".
	searchFormState = `(() => {
  const f = document.querySelector('` + headerSearch + `');
  if (!f) return "missing";
  return f.getClientRects().length > 0 ? "shown" : "hidden";
})()`
)

func TestHeaderSearch_ShownOnlyFrom64rem(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, c := range []struct {
		width int64
		want  string
	}{
		{320, "hidden"}, {360, "hidden"}, {390, "hidden"}, {768, "hidden"}, {1023, "hidden"},
		{1024, "shown"}, {1440, "shown"},
	} {
		if err := At(ctx, c.width, g.URL+"/_gallery/buttons"); err != nil {
			t.Fatal(err)
		}
		if got := eval[string](t, ctx, searchFormState); got != c.want {
			t.Errorf("header search at %d px is %s, want %s", c.width, got, c.want)
		}
	}
}

func TestHeaderSearch_AbsentOnBarePageInBrowser(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	if err := At(ctx, 1440, g.URL+"/_gallery/buttons"); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, searchFormState); got != "shown" {
		t.Fatalf("control: header search on a normal page is %s, want shown", got)
	}
	if err := At(ctx, 1440, g.URL+"/_gallery/bare"); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, searchFormState); got != "missing" {
		t.Errorf("header search on a bare page is %s, want missing", got)
	}
}

// §10: no sideways scroll at any width, on a page with the header form and on
// the search page in each state, with an unbroken query at the input's limit.
func TestHeaderSearch_NoOverflowAtAnyWidth(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	longQuery := strings.Repeat("W", 100)
	for _, path := range []string{"/_gallery/buttons", "/search", "/search?q=Draft", "/search?q=" + longQuery} {
		for _, w := range screens.Widths {
			name := path + "@" + strconv.FormatInt(w, 10)
			if err := At(ctx, w, g.URL+path); err != nil {
				t.Fatal(err)
			}
			if probs, err := Overflow(ctx); err != nil || len(probs) != 0 {
				t.Errorf("%s: overflow %q, %v", name, probs, err)
			}
			want := "Buttons"
			if strings.HasPrefix(path, "/search") {
				want = "Search"
			}
			if got := eval[string](t, ctx, `(() => { const h = document.querySelector("main h1"); return h ? h.textContent.trim() : ""; })()`); got != want {
				t.Errorf("%s: h1 = %q, want %q", name, got, want)
			}
			if w >= 1024 {
				if got := eval[string](t, ctx, searchFormState); got != "shown" {
					t.Errorf("%s: header search is %s, want shown", name, got)
				}
			}
		}
	}
}

// The gallery's sample documents must show as a group, or the overflow sweep
// of /search?q=Draft covers only the empty page.
func TestHeaderSearch_GalleryShowsResultGroups(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	if err := At(ctx, 1440, g.URL+"/search?q=Draft"); err != nil {
		t.Fatal(err)
	}
	if n := eval[int](t, ctx, `document.querySelectorAll("main ul.datalist-rows > li").length`); n == 0 {
		t.Error("no result rows on /search?q=Draft")
	}
}

// §10: every interactive element stays at least 44 × 44, form included, at
// the widths the form shows at.
func TestHeaderSearch_KeepsTargetsAt44px(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	for _, w := range []int64{1024, 1440} {
		if err := At(ctx, w, g.URL+"/_gallery/buttons"); err != nil {
			t.Fatal(err)
		}
		height := eval[float64](t, ctx, `(() => {
  const e = document.querySelector('`+headerSearchInput+`');
  return e ? e.getBoundingClientRect().height : 0;
})()`)
		if height < 44 {
			t.Errorf("search input at %d px is %.1f px high, want at least 44", w, height)
		}
		small := eval[[]string](t, ctx, `(`+screens.TargetSizeJS+`).filter(function (s) { return s.indexOf("input") === 0; })`)
		if len(small) != 0 {
			t.Errorf("small inputs at %d px: %q", w, small)
		}
	}
}

func TestHeaderSearch_EnterOpensTheSearchPage(t *testing.T) {
	g := startGallery(t)
	ctx := New(t)
	if err := At(ctx, 1024, g.URL+"/_gallery/buttons"); err != nil {
		t.Fatal(err)
	}
	if got := eval[string](t, ctx, searchFormState); got != "shown" {
		t.Fatalf("header search is %s, want shown", got)
	}
	do(t, ctx, "type and press Enter", chromedp.SendKeys(chromedp.CSS(headerSearchInput), "Draft"+kb.Enter))
	do(t, ctx, "wait for the search page", chromedp.WaitVisible(chromedp.CSS(`main ul.datalist-rows > li`)))
	if got := eval[string](t, ctx, `location.search`); got != "?q=Draft" {
		t.Errorf("location.search = %q, want ?q=Draft", got)
	}
	if got := eval[string](t, ctx, `document.querySelector("main h1").textContent.trim()`); got != "Search" {
		t.Errorf("h1 = %q, want Search", got)
	}
}
