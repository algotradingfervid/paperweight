//go:build e2e

package e2e

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

// A-1 (R1): on a phone the header shows the app name once, beside the logo.
func TestE2E_HeaderBrandOnce(t *testing.T) {
	h := Start(t)
	s := h.SignIn(priya, phone390)
	name := brand.Load().AppName
	n := Eval[int](s, `(() => {
  const name = `+strconv.Quote(name)+`;
  const header = document.querySelector("header");
  const parts = header.innerText.split(/\s+/).join(" ");
  let n = 0, i = parts.indexOf(name);
  while (i >= 0) { n++; i = parts.indexOf(name, i + 1); }
  return n;
})()`)
	if n != 1 {
		t.Errorf("phone header shows %q %d times, want 1", name, n)
	}
	if !Eval[bool](s, `(() => { const r = document.querySelector("header img.brand-logo").getBoundingClientRect(); return r.width > 0 && r.height > 0; })()`) {
		t.Error("the logo isn't shown in the phone header")
	}
}

// A-2 (R2): at 1280 px the sidebar shows the six groups in order, each an h2,
// and no two items share an icon.
func TestE2E_SidebarGroups(t *testing.T) {
	h := Start(t)
	s := h.SignIn(admin, 1280)
	heads := Eval[[]string](s, `Array.from(document.querySelectorAll("nav.app-sidebar h2")).filter((h) => h.getBoundingClientRect().height > 0).map((h) => h.textContent.trim()).filter((h) => h !== "Documents")`)
	if want := []string{"Work", "Purchase", "Goods receipt", "Expenses", "Masters", "Admin"}; !reflect.DeepEqual(heads, want) {
		t.Errorf("visible sidebar headings = %q, want %q", heads, want)
	}
	dupes := Eval[[]string](s, `(() => {
  const seen = new Map(), out = [];
  document.querySelectorAll("nav.app-sidebar a").forEach((a) => {
    if (a.closest("ul").getAttribute("aria-labelledby") === "side-documents") return; // test-only DocTypes
    const svg = a.querySelector("svg");
    const key = svg ? svg.innerHTML : "";
    if (seen.has(key)) out.push(seen.get(key) + " = " + a.getAttribute("href"));
    seen.set(key, a.getAttribute("href"));
  });
  return out;
})()`)
	if len(dupes) != 0 {
		t.Errorf("nav items sharing an icon: %q", dupes)
	}
	// More masters opens from the keyboard and shows its items.
	s.Do("focus More masters", chromedp.Evaluate[chromedp.Void](`document.querySelector("nav.app-sidebar details.nav-more summary").focus()`))
	s.Keys(kb.Enter)
	s.Until("More masters open", `document.querySelector("nav.app-sidebar details.nav-more").open && document.querySelector('nav.app-sidebar a[href="/m/uom"]').getBoundingClientRect().height > 0`)
}
