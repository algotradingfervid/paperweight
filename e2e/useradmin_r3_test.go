//go:build e2e

package e2e

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// r3Widths are the widths the user screens are measured at (R3).
var r3Widths = []int64{320, 360, 390, 768, 1280}

const (
	r3Temp = "Temp-Godown-2026"

	// r3Unreachable lists the visible controls under 44 px tall, or whose
	// centre another element covers. A dialog covers the page behind it, so a
	// modal limits the check to itself.
	r3Unreachable = `(() => {
  const root = document.querySelector("dialog[open]") ?? document.querySelector("main");
  return [...root.querySelectorAll("a.btn, button, input:not([type=hidden]), label.field__choice")].filter((e) => {
    if (!e.checkVisibility({visibilityProperty: true}) || e.matches(".password__toggle")) return false;
    e.scrollIntoView({block: "center", behavior: "instant"});
    const r = e.getBoundingClientRect();
    const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
    return (e.matches("input[type=checkbox]") ? false : r.height < 44) || !(hit && (e.contains(hit) || hit.contains(e) || e.closest("label")?.contains(hit)));
  }).map((e) => e.tagName + ":" + e.textContent.trim().slice(0, 30));
})()`
	// r3ButtonShare is the named button's width as a share of the page column
	// (shell review L7: primaries don't stretch on desktop).
	r3ButtonShare = `((label) => {
  const b = [...document.querySelectorAll("main .btn")].find((e) => e.textContent.trim() === label);
  return b.getBoundingClientRect().width / document.querySelector("main").getBoundingClientRect().width;
})`
	// r3Outside lists panel controls that leave the viewport sideways.
	r3Outside = `((sel) => [...document.querySelectorAll(sel)].filter((e) => {
  const r = e.getBoundingClientRect();
  return r.left < 0 || r.right > innerWidth;
}).map((e) => e.textContent.trim().slice(0, 30)))`
)

var r3UserPath = regexp.MustCompile(`^/users/([0-9]+)$`)

// r3Path is the current path.
func r3Path(s *Session) string {
	s.t.Helper()
	return Eval[string](s, `location.pathname`)
}

// r3Add adds a user through the form and stays on their page.
func r3Add(s *Session, name, email string) string {
	s.t.Helper()
	s.Open("/users")
	s.Press("New user")
	s.Until("the new-user form", `location.pathname === "/users/new"`)
	s.Fill("name", name)
	s.Fill("email", email)
	s.Fill("password", r3Temp)
	s.Press("Add user")
	s.Until("the user's page", `/^\/users\/[0-9]+$/.test(location.pathname)`)
	path := r3Path(s)
	if !r3UserPath.MatchString(path) {
		s.t.Fatalf("after adding: %s", path)
	}
	return path
}

// B1: every action, from the rendered screens, as a browser posts them.
func TestE2E_UserAdmin_Actions(t *testing.T) {
	h := Start(t)
	s := h.SignIn(admin, desktop)

	path := r3Add(s, "Kiran Joshi", "kiran@vardhan.test")
	s.WaitText("Kiran Joshi was added.")

	clickOn(s, `input[name=roles][value="Stores"]`)
	s.Press("Save roles")
	s.WaitText("Roles for Kiran Joshi saved.")
	if !Eval[bool](s, `document.querySelector('input[name=roles][value="Stores"]').checked`) {
		t.Error("Stores isn't ticked after saving")
	}

	s.Press("Reset password")
	s.Until("the reset form", `location.pathname.endsWith("/password")`)
	s.Fill("password", "Reset-Ledger-2026")
	s.Press("Reset password")
	s.WaitText("Password reset for Kiran Joshi.")

	// Disable goes through its confirm, Enable undoes it, and both say so.
	s.Press("Disable user")
	s.Until("the disable confirm", `document.getElementById("disable-user").open`)
	s.Press("Keep enabled")
	s.Until("the confirm closed", `!document.getElementById("disable-user").open`)
	s.Press("Disable user")
	s.Until("the disable confirm", `document.getElementById("disable-user").open`)
	s.Press("Yes, disable")
	s.WaitText("Kiran Joshi is disabled and signed out everywhere.")
	s.Press("Enable user")
	s.WaitText("Kiran Joshi can sign in again.")

	// Keep user closes the confirm and changes nothing.
	s.Press("Delete user")
	s.Until("the confirm", `document.getElementById("delete-user").open`)
	s.Press("Keep user")
	s.Until("the confirm closed", `!document.getElementById("delete-user").open`)
	if r3Path(s) != path {
		t.Errorf("Keep user left %s for %s", path, r3Path(s))
	}

	s.Press("Delete user")
	s.Until("the confirm", `document.getElementById("delete-user").open`)
	s.Press("Yes, delete")
	s.WaitText("Kiran Joshi was deleted.")
	if r3Path(s) != "/users" {
		t.Errorf("after delete: %s", r3Path(s))
	}
	if s.Shows("kiran@vardhan.test") {
		t.Error("the list still shows the user")
	}
}

// The user who was added signs in with the password the form set, and a
// locked account opens again from the Unlock button.
func TestE2E_UserAdmin_UnlockAndSignIn(t *testing.T) {
	h := Start(t)
	a := h.SignIn(admin, desktop)
	path := r3Add(a, "Kiran Joshi", "kiran@vardhan.test")

	k := h.Browser("kiran@vardhan.test", desktop)
	for range auth.AccountLimit {
		k.Open("/signin")
		k.Fill("email", "kiran@vardhan.test")
		k.Fill("password", "wrong-password-1")
		k.Press("Sign in")
		k.Until("the refusal", `document.querySelector(".banner--danger") !== null`)
	}

	a.Open(path)
	a.WaitText("This account is locked after too many failed sign-ins.")
	a.Press("Unlock account")
	a.WaitText("Kiran Joshi can sign in again.")
	a.WaitText("This account isn't locked.")

	k.Open("/signin")
	k.Fill("email", "kiran@vardhan.test")
	k.Fill("password", r3Temp)
	k.Press("Sign in")
	k.Until("the first sign-in's Change password", `location.pathname === "/account/password"`)
}

// Your own page offers no Delete or Disable; a stale version is the
// ConflictBanner with what was ticked kept (CD-381).
func TestE2E_UserAdmin_Refusals(t *testing.T) {
	h := Start(t)
	s := h.SignIn(admin, desktop)

	s.Open("/users/" + strconv.FormatInt(int64(h.Principal(admin).ID), 10))
	s.WaitText("Roles")
	if s.Shows("Delete user") || s.Shows("Disable user") {
		t.Error("the admin's own page offers Delete or Disable")
	}

	path := r3Add(s, "Kiran Joshi", "kiran@vardhan.test")
	clickOn(s, `input[name=roles][value="Stores"]`)
	s.Do("age the page", chromedp.Evaluate[chromedp.Void](`(() => {
  const v = document.querySelector("form[action$='/roles']").elements.version;
  v.value = String(Number(v.value) - 1);
})()`))
	s.Press("Save roles")
	s.WaitText(auth.UserChangedError("Kiran Joshi").Error())
	if !Eval[bool](s, `document.querySelector('input[name=roles][value="Stores"]').checked`) {
		t.Error("the ticked role was lost")
	}
	if n := Eval[int](s, `document.querySelectorAll("main .banner--warning").length`); n != 1 {
		t.Errorf("%d warning banners, want 1", n)
	}
	s.Press("Reload Kiran Joshi")
	s.Until("the user's page again", `location.pathname === `+strconv.Quote(path))
	s.WaitGone(auth.UserChangedError("Kiran Joshi").Error())

	s.Press("Reset password")
	s.Until("the reset form", `location.pathname.endsWith("/password")`)
	s.Fill("password", "short-1")
	s.Press("Reset password")
	s.WaitText(auth.ErrPasswordTooShort.Msg)
	if got := Eval[string](s, `document.getElementById("f-password").value`); got != "" {
		t.Errorf("the password came back: %q", got)
	}
}

// A user who isn't an Administrator gets the refusal page, no form.
func TestE2E_UserAdmin_Refused(t *testing.T) {
	h := Start(t)
	s := h.SignIn(priya, desktop)
	s.Open("/users/new")
	s.WaitText(auth.ErrAdminOnlyUsers.Msg)
	if n := Eval[int](s, `document.querySelectorAll("main form").length`); n != 0 {
		t.Errorf("%d forms on the refusal", n)
	}
}

// Layout at every width, with a 100-character name: nothing overflows, the
// confirm stays inside the screen, and every control keeps its 44 px target
// with nothing over it.
func TestE2E_UserAdmin_Layout(t *testing.T) {
	h := Start(t)
	a := h.SignIn(admin, desktop)
	path := r3Add(a, uitest.LongName100, "long@vardhan.test")
	for _, width := range r3Widths {
		s := h.SignIn(admin, width)
		pages := map[string]string{
			"list": "/users", "new user": "/users/new", "user": path, "reset": path + "/password", "sessions": "/account/sessions",
		}
		for name, page := range pages {
			s.Open(page)
			noOverflow(s, name)
			if bad := Eval[[]string](s, r3Unreachable); len(bad) != 0 {
				t.Errorf("%s at %d: can't reach %q", name, width, bad)
			}
		}
		if width >= 768 {
			for page, label := range map[string]string{"/users": "New user", "/users/new": "Add user", path: "Save roles", path + "/password": "Reset password"} {
				s.Open(page)
				if got := Eval[float64](s, r3ButtonShare+`(`+strconv.Quote(label)+`)`); got > 0.5 {
					t.Errorf("%s at %d: %s fills %.0f%% of its column", page, width, label, got*100)
				}
			}
		}
		for label, dialog := range map[string]string{"Delete user": "delete-user", "Disable user": "disable-user"} {
			s.Open(path)
			s.Press(label)
			s.Until("the confirm", `document.getElementById("`+dialog+`").open`)
			noOverflow(s, dialog)
			if bad := Eval[[]string](s, r3Outside+`("#`+dialog+` button")`); len(bad) != 0 {
				t.Errorf("%s at %d: %q leave the screen", dialog, width, bad)
			}
			if bad := Eval[[]string](s, r3Unreachable); len(bad) != 0 {
				t.Errorf("%s at %d: can't reach %q", dialog, width, bad)
			}
		}
	}
}

// H7: from 48rem the account menu reaches the same pages as Me. It closes on
// Escape, giving focus back, and on a click outside; the keyboard reaches and
// follows its links; every link keeps its target and stays on screen.
func TestE2E_AccountMenu(t *testing.T) {
	h := Start(t)
	for _, width := range []int64{768, 1280} {
		s := h.SignIn(priya, width)
		menuOpen := `document.querySelector("details.user-menu").open`

		clickOn(s, "details.user-menu summary")
		s.Until("the menu", menuOpen)
		bad := Eval[[]string](s, r3Outside+`(".user-menu-panel a, .user-menu-panel button")`)
		if len(bad) != 0 {
			t.Errorf("%d: %q leave the screen", width, bad)
		}
		short := Eval[[]string](s, `[...document.querySelectorAll(".user-menu-panel a, .user-menu-panel button")].filter((e) => {
  const r = e.getBoundingClientRect();
  const hit = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2);
  return r.height < 44 || !e.contains(hit);
}).map((e) => e.textContent.trim())`)
		if len(short) != 0 {
			t.Errorf("%d: %q are short or covered", width, short)
		}

		s.Keys(kb.Escape)
		s.Until("Escape closes it", `!`+menuOpen)
		if !Eval[bool](s, `document.activeElement === document.querySelector("details.user-menu summary")`) {
			t.Errorf("%d: focus after Escape is on %s", width, Eval[string](s, `document.activeElement.tagName`))
		}

		s.Keys(kb.Enter)
		s.Until("Enter opens it", menuOpen)
		clickOn(s, "main")
		s.Until("a click outside closes it", `!`+menuOpen)

		clickOn(s, "details.user-menu summary")
		s.Until("the menu", menuOpen)
		s.Do("inside click", chromedp.Evaluate[chromedp.Void](`document.querySelector(".user-menu-panel").click()`))
		if !Eval[bool](s, menuOpen) {
			t.Errorf("%d: a click inside closed it", width)
		}

		// Keyboard: from the chip, Tab reaches each link in order; Enter follows the second.
		s.Do("focus the chip", chromedp.Evaluate[chromedp.Void](`document.querySelector("details.user-menu summary").focus()`))
		var order []string
		for range 3 {
			s.Tab()
			order = append(order, strings.TrimSpace(Eval[string](s, `document.activeElement.textContent`)))
		}
		if strings.Join(order, ",") != "My requests,My sessions,Change password" {
			t.Errorf("%d: Tab order %q", width, order)
		}
		s.Tab()
		if got := strings.TrimSpace(Eval[string](s, `document.activeElement.textContent`)); got != "Sign out" {
			t.Errorf("%d: last stop = %q", width, got)
		}
		s.Do("back to My sessions", chromedp.Evaluate[chromedp.Void](`document.querySelector('.user-menu-panel a[href="/account/sessions"]').focus()`))
		s.Keys(kb.Enter)
		s.Until("My sessions", `location.pathname === "/account/sessions"`)
		s.WaitText("Chrome on")
		s.WaitText("This session")
		if s.Shows("Mozilla/5.0") {
			t.Errorf("%d: the page shows the raw user agent", width)
		}

		clickOn(s, "details.user-menu summary")
		s.Press("Change password")
		s.Until("the password page", `location.pathname === "/account/password"`)
	}
}

// RR1b finding 4: Add user with every field blank posts the form (it is
// novalidate), and the server's error summary names the first blank
// required field, not the browser's own bubble.
func TestE2E_UserAdmin_BlankShowsErrorSummary(t *testing.T) {
	h := Start(t)
	s := h.SignIn(admin, desktop)
	s.Open("/users/new")
	s.Press("Add user")
	s.Until("the error summary", `document.querySelector(".error-summary")?.checkVisibility() ?? false`)
	if got := Eval[string](s, `document.querySelector(".error-summary").innerText`); !strings.Contains(got, auth.ErrNameRequired.Msg) {
		t.Errorf("error summary %q, want %q", got, auth.ErrNameRequired.Msg)
	}
}
