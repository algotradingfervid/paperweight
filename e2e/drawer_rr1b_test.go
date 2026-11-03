//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
)

// drawerCloseJS reports how the drawer's Close sits: off screen, covered at
// its centre, or "" when it is in view and on top. It also says when the
// current item it measured against isn't in view (the sheet didn't scroll).
const drawerCloseJS = `(() => {
  const sheet = document.getElementById("nav-sheet");
  const box = sheet.querySelector(".modal-box");
  if (box.scrollHeight <= box.clientHeight) return "the drawer doesn't scroll: nothing to measure";
  if (box.scrollTop === 0) return "the drawer didn't scroll to the current item";
  const b = sheet.querySelector('button[aria-label="Close menu"]');
  const r = b.getBoundingClientRect();
  if (r.top < 0 || r.bottom > innerHeight) return "Close at " + Math.round(r.top) + "-" + Math.round(r.bottom) + " of " + innerHeight;
  const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
  if (!hit || !b.contains(hit)) return "Close is covered by " + (hit ? hit.outerHTML.slice(0, 80) : "nothing");
  // The focused current item isn't under the sticky head.
  const c = document.activeElement.getBoundingClientRect();
  const on = document.elementFromPoint(c.x + c.width / 2, c.y + c.height / 2);
  return on && document.activeElement.contains(on) ? "" : "the current item is covered by " + (on ? on.outerHTML.slice(0, 80) : "nothing");
})()`

// RR1b finding 5: at 320, 360 and 390 px and on a tablet (768), on a page whose drawer item is far down
// (Overdue approvals, the last admin item), opening the drawer scrolls to
// the current item, uncovered, while the head with Close stays in view and
// tappable, and Close closes it.
func TestE2E_DrawerCloseStaysInView(t *testing.T) {
	h := Start(t)
	for _, sz := range [][2]int64{{320, 568}, {360, 640}, {390, 844}, {768, 1024}} {
		s := h.SignIn(admin, sz[0])
		s.Do("open Overdue approvals", chromedp.EmulateViewport(sz[0], sz[1], chromedp.EmulateMobile, chromedp.EmulateTouch), chromedp.Navigate(h.URL+"/admin/approvals/overdue"))
		s.Until("the page", `document.readyState === "complete" && location.pathname === "/admin/approvals/overdue"`)
		clickOn(s, "button.menu-button")
		s.Until("the current item focused", `document.getElementById("nav-sheet").open && document.activeElement.getAttribute("href") === "/admin/approvals/overdue"`)
		if m := Eval[string](s, drawerCloseJS); m != "" {
			t.Errorf("%d px: %s", sz[0], m)
		}
		clickOn(s, `#nav-sheet button[aria-label="Close menu"]`)
		s.Until("the drawer closed at "+strconv.FormatInt(sz[0], 10), `!document.getElementById("nav-sheet").open`)
	}
}
