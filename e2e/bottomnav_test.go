//go:build e2e

package e2e

import (
	"reflect"
	"strconv"
	"testing"

	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

const (
	// bottomNavLinks is the visible bottom nav's link texts, with the text
	// screen readers get (aria-hidden parts left out), or null when the nav
	// isn't shown.
	bottomNavLinks = `(() => {
  const nav = document.querySelector("nav.bottomnav");
  if (!nav || nav.getClientRects().length === 0) return null;
  const name = (e) => Array.from(e.childNodes).map((c) =>
    c.nodeType === 3 ? c.textContent : (c.getAttribute("aria-hidden") === "true" ? "" : name(c))).join("");
  return Array.from(nav.querySelectorAll("a")).map((a) => name(a).replace(/\s+/g, " ").trim());
})()`
	// shown reports whether the selector's first match takes room.
	shownJS = `((sel) => { const e = document.querySelector(sel); return !!e && e.getClientRects().length > 0; })`
)

func shown(s *Session, sel string) bool {
	s.t.Helper()
	return Eval[bool](s, shownJS+`(`+strconv.Quote(sel)+`)`)
}

// A-3 (R3): at 390 px the list and home pages show the five-link bottom nav,
// Approvals named with its count; wizard and document pages show none. The
// phone header has no inbox or account chip (CD-349 (3)); 768 px has both
// the bottom nav and the header search (CD-352 (1)).
func TestE2E_BottomNav(t *testing.T) {
	h := Start(t)
	r := refPO(h)
	sent := h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	s := h.SignIn(ravi, phone390)
	want := []string{"Home", "Approvals, 1 waiting", "New", "Search", "Me"}
	for _, path := range []string{"/approvals", "/modules/purchase", "/doc/" + purchase.DocType, "/search", "/new"} {
		s.Open(path)
		got := Eval[[]string](s, bottomNavLinks)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: bottom nav = %q, want %q", path, got, want)
		}
	}
	if shown(s, "header .inbox-link") || shown(s, "header .user-menu") {
		t.Error("the phone header still shows the inbox or the account chip")
	}
	if got := Eval[string](s, `document.querySelector('nav.bottomnav a[aria-current="page"]')?.textContent.trim() ?? ""`); got != "New" {
		t.Errorf("current item on /new = %q, want New", got)
	}
	p := h.SignIn(priya, phone390)
	for _, path := range []string{"/doc/" + purchase.DocType + "/new", DocPath(fixture.Order, sent.ID)} {
		p.Open(path)
		if !p.Shows("Save and continue") && !p.Shows(firstNumber) && !p.Shows("Pending approval") {
			t.Errorf("%s: not the wizard or the document page\n%s", path, p.page())
		}
		if got := Eval[[]string](p, bottomNavLinks); got != nil {
			t.Errorf("%s (focus): bottom nav = %q, want none", path, got)
		}
	}

	// CD-352 (1): tablets keep the bottom nav beside the header search; the
	// sidebar takes over from 64rem, and the two never show together.
	d := h.SignIn(ravi, 768)
	if got := Eval[[]string](d, bottomNavLinks); !reflect.DeepEqual(got, want) {
		t.Errorf("768 px: bottom nav = %q, want %q", got, want)
	}
	if !shown(d, "header form[action='/search']") || !shown(d, "header .inbox-link") || !shown(d, "header .user-menu") {
		t.Error("768 px: the header lacks the search box, the inbox or the account chip")
	}
	if shown(d, "nav.app-sidebar") || shown(d, "header .header-new") {
		t.Error("768 px: the sidebar or the header's New shows below 64rem")
	}
	for _, w := range []int64{1023, 1024, desktop} {
		b := h.SignIn(ravi, w)
		nav, side := Eval[[]string](b, bottomNavLinks) != nil, shown(b, "nav.app-sidebar")
		if nav == side || nav != (w < 1024) {
			t.Errorf("%d px: bottom nav %v, sidebar %v: want exactly one, the bottom nav below 1024", w, nav, side)
		}
	}
}

// A-4 (R3): at 390 px New opens the sheet with exactly what Suresh may
// create, focus on the first tile; Escape closes it and focus returns to New.
// On desktop the header's New opens the same sheet.
func TestE2E_NewSheet(t *testing.T) {
	h := Start(t)
	s := h.SignIn(suresh, phone390)
	s.Press("New")
	s.Until("the sheet", `document.getElementById("new-sheet").open`)
	tiles := Eval[[]string](s, `Array.from(document.querySelectorAll("#new-sheet a.newtile")).map((a) => a.textContent.trim())`)
	if want := []string{"Purchase request", "Goods receipt", "Expense claim", "Advance request"}; !reflect.DeepEqual(tiles, want) {
		t.Errorf("Suresh's sheet = %q, want %q", tiles, want)
	}
	if got := Eval[string](s, `document.activeElement.textContent.trim()`); got != "Purchase request" {
		t.Errorf("focus on open = %q, want the first tile", got)
	}
	s.Keys(kb.Escape)
	s.Until("the sheet closed", `!document.getElementById("new-sheet").open`)
	if got := Eval[string](s, `document.activeElement.closest("nav.bottomnav") ? document.activeElement.textContent.trim() : ""`); got != "New" {
		t.Errorf("focus after Escape = %q, want New", got)
	}
	s.Press("New")
	s.Until("the sheet", `document.getElementById("new-sheet").open`)
	s.Press("Goods receipt")
	s.Until("the new goods receipt", `location.pathname === "/doc/goodsreceipt.Receipt/new"`)

	d := h.SignIn(admin, desktop)
	d.Press("New")
	d.Until("the sheet", `document.getElementById("new-sheet").open`)
	if n := Eval[int](d, `document.querySelectorAll("#new-sheet a.newtile").length`); n != 6 {
		t.Errorf("admin's sheet has %d tiles, want 6", n)
	}
}

// §5.2: on a phone Me opens the account sheet, and Sign out there signs out.
func TestE2E_MeSheet(t *testing.T) {
	h := Start(t)
	s := h.SignIn(priya, phone390)
	s.Press("Me")
	s.Until("the account sheet", `document.getElementById("me-sheet").open`)
	s.Press("Sign out")
	s.Until("the sign-in page", `location.pathname === "/signin"`)
}
