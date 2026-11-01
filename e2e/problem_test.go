//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// phone320 is the narrowest phone the layout must fit without sideways scroll.
const phone320 = 320

// wantProblemPage checks the 403 or 404 page on s (UI v2 §5.14, A-30): the
// HTTP status, the heading, the kernel text exactly, the next step, Go to
// Home and Search, and no sideways scroll.
func wantProblemPage(t *testing.T, s *Session, status int, heading, msg string) {
	t.Helper()
	s.Until("the problem page", `document.querySelector("main#main section.problem") !== null`)
	if got := Eval[int](s, `performance.getEntriesByType("navigation")[0].responseStatus`); got != status {
		t.Errorf("status = %d, want %d", got, status)
	}
	if got := Eval[string](s, `[...document.querySelectorAll("main#main h1")].map((h) => h.innerText).join("|")`); got != heading {
		t.Errorf("headings = %q, want only %q", got, heading)
	}
	if got := Eval[string](s, `document.querySelector(".problem-message").innerText`); got != msg {
		t.Errorf("message = %q, want the kernel text %q", got, msg)
	}
	if !s.Shows("If someone sent you this link, ask them to check it, or ask your administrator for access.") {
		t.Errorf("no next-step line\n%s", s.page())
	}
	links := Eval[[]string](s, `[...document.querySelectorAll(".problem-actions a")].map((a) => a.innerText.trim() + " " + a.getAttribute("href"))`)
	if want := []string{"Go to Home /", "Search /search"}; len(links) != 2 || links[0] != want[0] || links[1] != want[1] {
		t.Errorf("ways back = %q, want %q", links, want)
	}
	if Eval[bool](s, `document.documentElement.scrollWidth > document.documentElement.clientWidth`) {
		t.Errorf("the problem page scrolls sideways at %d px", s.width)
	}
}

// TestE2E_ProblemPage404: a missing document reads as the kernel 404 with
// the problem page, and Search leaves it (A-30).
func TestE2E_ProblemPage404(t *testing.T) {
	h := Start(t)
	s := h.SignIn(priya, phone320)
	s.Open(DocPath(fixture.Order, missingDoc))
	wantProblemPage(t, s, 404, "We can't open this page", doctype.ErrNotFound.Error())

	s.Press("Search")
	s.Until("the search page", `location.pathname === "/search"`)
}

// TestE2E_ProblemPage403: a user without the create right opening /new gets
// AUTH's refusal on the 403 problem page, and Go to Home leaves it.
func TestE2E_ProblemPage403(t *testing.T) {
	h := Start(t)
	dt, ok := doctype.Lookup(fixture.Order)
	if !ok {
		t.Fatalf("%s isn't registered", fixture.Order)
	}
	msg := (&auth.DeniedError{Right: auth.RightCreate, Label: dt.Label, Plural: dt.PluralLabel}).Error()
	s := h.SignIn(ravi, phone320) // Purchase Manager: read and approve, no create
	s.Open("/doc/" + fixture.Order + "/new")
	wantProblemPage(t, s, 403, "You don't have access to this", msg)

	s.Press("Go to Home")
	s.Until("a page that isn't the problem page", `document.querySelector("section.problem") === null && location.pathname !== `+strconv.Quote("/doc/"+fixture.Order+"/new"))
}
