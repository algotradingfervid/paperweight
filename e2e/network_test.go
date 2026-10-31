//go:build e2e

package e2e

import (
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
)

// TestE2E_NetworkBanner_LostCreateAnswerOffersReload: CD-346. The answer to a
// new document's first save is lost after the server saved it: the banner
// offers Reload, never a re-send, and exactly one draft exists.
func TestE2E_NetworkBanner_LostCreateAnswerOffersReload(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	const count = `SELECT count(*) FROM fixture_order`
	before := Scalar[int](h, count)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	h.DropAnswers("/doc/" + fixture.Order + "/new")
	p.Press(saveContinue)
	p.WaitText("Your change may have been saved. Reload to check before trying again.")
	if n := Eval[int](p, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
		t.Errorf("the banner offers %d Try again buttons after a lost create, want none", n)
	}
	if got := Scalar[int](h, count) - before; got != 1 {
		t.Fatalf("%d new drafts after the lost answer, want 1", got)
	}
	h.DropAnswers("")
	p.Press("Reload")
	p.Until("the page reloaded", `document.readyState === "complete" && document.getElementById("network").children.length === 0`)
	if got := Scalar[int](h, count) - before; got != 1 {
		t.Errorf("%d new drafts after Reload, want 1", got)
	}
}
