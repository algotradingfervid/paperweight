//go:build e2e

package e2e

import (
	"testing"

	"github.com/chromedp/chromedp/kb"
)

// homeWidths are the supported widths of the Home check (plan R4).
var homeWidths = []int64{320, 360, 390, 768, 1280}

// homeLayoutProblems measures what Home could break beyond sideways scroll:
// tiles below the tap target, a card or row wider than main, a card's text
// clipped, the greeting not directly above the h1 or wrapping, the page end
// under the bottom nav.
const homeLayoutProblems = `(() => {
  const out = [];
  const main = document.querySelector("main").getBoundingClientRect();
  for (const t of document.querySelectorAll("main a.newtile")) {
    const r = t.getBoundingClientRect();
    if (r.height < 44 || r.width < 44) out.push("tile " + t.textContent.trim() + " is " + r.width + " x " + r.height);
  }
  for (const e of document.querySelectorAll("main .statcard, main .datalist-link, main a.newtile, main .empty-state p")) {
    const r = e.getBoundingClientRect();
    if (r.left < main.left - 1 || r.right > main.right + 1) out.push(e.className + " leaves main: " + r.left + ".." + r.right);
    if (e.scrollWidth > e.clientWidth + 1) out.push(e.className + " clips its text");
  }
  const eyebrow = document.querySelector("main p.eyebrow"), h1 = document.querySelector("main h1");
  if (eyebrow) {
    const a = eyebrow.getBoundingClientRect(), b = h1.getBoundingClientRect();
    if (a.bottom > b.top + 1) out.push("the greeting isn't above the h1");
    if (a.height > 2 * parseFloat(getComputedStyle(eyebrow).lineHeight) - 1) out.push("the greeting wraps");
  } else out.push("no greeting");
  const tiles = [...document.querySelectorAll("main a.newtile")].map((t) => t.getBoundingClientRect());
  tiles.forEach((a, i) => tiles.slice(i + 1).forEach((b) => {
    if (a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom) out.push("two tiles overlap");
  }));
  const nav = document.querySelector("nav.bottomnav");
  if (nav && nav.getClientRects().length > 0) {
    const last = [...document.querySelectorAll("main a")].pop();
    if (last) {
      last.scrollIntoView({block: "end"});
      if (last.getBoundingClientRect().top >= nav.getBoundingClientRect().top) out.push("the page's last link stays under the bottom nav");
      window.scrollTo(0, 0);
    }
  }
  return out;
})()`

func homeLayoutOK(s *Session, who string) {
	s.t.Helper()
	noOverflow(s, who+"'s Home")
	if bad := Eval[[]string](s, homeLayoutProblems); len(bad) != 0 {
		s.t.Errorf("%s's Home at %d px: %q\n%s", who, s.width, bad, s.page())
	}
}

// Plan R4: Home at every supported width for the three kinds of viewer: Priya
// with counts, 5 Recent rows and tiles; Ravi with 3 Waiting rows, the hot
// card and See all; Deepak caught up with the second line.
func TestE2E_HomeEveryWidth(t *testing.T) {
	h := startPurchase(t)
	for range 4 {
		h.e3BigOrder()
	}
	for range 3 {
		h.metroDraft(priya)
	}
	for _, width := range homeWidths {
		p := h.SignIn(priya, width)
		p.WaitText("Recent")
		if n := e3Count(p, "main .homesect .datalist-link"); n != 5 {
			t.Errorf("%d px: Priya's rows = %d, want 5 Recent rows", width, n)
		}
		homeLayoutOK(p, "Priya")

		r := h.SignIn(ravi, width)
		r.WaitText("See all 4")
		if n := e3Count(r, "main a.statcard--hot"); n != 1 {
			t.Errorf("%d px: Ravi's hot cards = %d", width, n)
		}
		homeLayoutOK(r, "Ravi")

		d := h.SignIn(deepak, width)
		d.WaitText("You're all caught up.")
		d.WaitText("New documents and approvals will show here.")
		homeLayoutOK(d, "Deepak")
	}
}

// H4, M10 on a phone: Home and Approvals are two tabs, each current on its own
// page; Search lands in the box; the drawer focuses the current item and Esc
// gives focus back.
func TestE2E_HomeNavAndFocus(t *testing.T) {
	h := Start(t)
	for _, width := range []int64{320, 360, 390} {
		s := h.SignIn(priya, width)
		current := `document.querySelector('nav.bottomnav a[aria-current="page"]').getAttribute("href")`
		if got := Eval[string](s, current); got != "/" {
			t.Errorf("%d px: current tab on / = %q", width, got)
		}
		clickOn(s, `nav.bottomnav a[href="/approvals"]`)
		s.Until("Approvals", `location.pathname === "/approvals"`)
		if got := Eval[string](s, current); got != "/approvals" {
			t.Errorf("%d px: current tab on /approvals = %q", width, got)
		}
		clickOn(s, `nav.bottomnav a[href="/search"]`)
		s.Until("the search box focused", `location.pathname === "/search" && document.activeElement.id === "filter-q"`)

		s.Open("/approvals")
		clickOn(s, "button.menu-button")
		s.Until("the current item focused", `document.getElementById("nav-sheet").open && document.activeElement.getAttribute("aria-current") === "page" && document.activeElement.getAttribute("href") === "/approvals"`)
		s.Keys(kb.Escape)
		s.Until("focus back on Menu", `document.activeElement.classList.contains("menu-button")`)

		// A page with no nav item: the sheet still opens with focus inside it.
		s.Open("/search")
		clickOn(s, "button.menu-button")
		s.Until("focus in the sheet", `document.getElementById("nav-sheet").open && document.getElementById("nav-sheet").contains(document.activeElement) && document.activeElement.tagName === "BUTTON"`)
	}
}

// A-2 at 1280 px: Home leads Work and is told from Approvals.
func TestE2E_SidebarHomeCurrent(t *testing.T) {
	h := Start(t)
	s := h.SignIn(priya, 1280)
	first := `document.querySelector('nav.app-sidebar ul[aria-labelledby="side-work"] a')`
	if got := Eval[string](s, first+`.getAttribute("aria-current")`); got != "page" {
		t.Errorf("Home's aria-current on / = %q", got)
	}
	s.Open("/approvals")
	if got := Eval[string](s, first+`.getAttribute("href")`); got != "/" {
		t.Errorf("first Work item = %q", got)
	}
	if got := Eval[string](s, `document.querySelector('nav.app-sidebar a[aria-current="page"]').getAttribute("href")`); got != "/approvals" {
		t.Errorf("sidebar current on /approvals = %q", got)
	}
}
