//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const lowerVersions = `document.querySelectorAll("input[name=version]").forEach((v) => { v.value = String(Number(v.value) - 1); })`

// wantConflict fails unless the page shows no danger banner and focus is on
// the alert id, around the warning banner.
func wantConflict(t *testing.T, s *Session, id string) {
	t.Helper()
	s.Until("focus on the banner", `document.activeElement.id === "`+id+`" && document.activeElement.querySelector(".banner--warning") !== null`)
	if got := Eval[int](s, `document.querySelectorAll("main .banner--danger").length`); got != 0 {
		t.Errorf("%d danger banners after the 409, want 0", got)
	}
	if !s.Shows("Your entries stay on this page until you reload, so you can copy them.") {
		t.Errorf("the conflict banner has no keep-your-entries line\n%s", s.page())
	}
}

// TestE2E_ReasonPageConflictBanner: F3 (CD-362). A reason page posted with a
// stale version shows the warning ConflictBanner holding focus, the typed
// reason stays, and Reload lands on that page at the stored version.
func TestE2E_ReasonPageConflictBanner(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	page := DocPath(fixture.Order, id) + "/cancel"
	r := h.SignIn(ravi, phone360)
	r.Open(page)
	r.WaitText("Reason for cancelling")
	r.Fill("reason", reasonPO)
	Eval[any](r, lowerVersions)
	r.Press("Cancel purchase order")
	r.WaitText("Someone else changed " + firstNumber + " after you opened it.")
	wantConflict(t, r, "reason-alert")
	if got := Eval[string](r, `document.querySelector("textarea[name=reason]").value`); got != reasonPO {
		t.Errorf("reason after the conflict = %q, want the typed %q", got, reasonPO)
	}
	if got := Eval[string](r, `document.querySelector("input[name=version]").value`); got != strconv.Itoa(orderVersion(h, id)-1) {
		t.Errorf("version after the conflict = %s, want the posted %d (CD-94)", got, orderVersion(h, id)-1)
	}
	r.Press("Reload " + firstNumber)
	r.WaitGone("Someone else changed")
	r.WaitText("Reason for cancelling")
	if got := Eval[string](r, `location.pathname`); got != page {
		t.Errorf("Reload landed on %s, want %s", got, page)
	}
	if got := Eval[string](r, `document.querySelector("input[name=version]").value`); got != strconv.Itoa(orderVersion(h, id)) {
		t.Errorf("reloaded version = %s, want the stored %d", got, orderVersion(h, id))
	}
}

// TestE2E_DocPageConflictBanner: F3 (CD-362). An action posted from a stale
// document page shows the ConflictBanner holding focus, and Reload lands on
// the document with the banner gone.
func TestE2E_DocPageConflictBanner(t *testing.T) {
	h := Start(t)
	res := refPO(h)
	h.Do(priya, fixture.Order, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	path := DocPath(fixture.Order, res.ID)
	r := h.SignIn(ravi, phone360)
	r.Open(path)
	r.WaitText("Approve")
	Eval[any](r, lowerVersions)
	r.Press("Approve")
	r.WaitText(staleText(res.ID))
	wantConflict(t, r, "doc-alert")
	r.Press("Reload " + draft(res.ID))
	r.WaitGone(staleText(res.ID))
	r.WaitText("Approve")
	if got := Eval[string](r, `location.pathname`); got != path {
		t.Errorf("Reload landed on %s, want %s", got, path)
	}
}

// TestE2E_LinePagePastDraftRefused: F3 (CD-381). The draft is submitted while a
// line page is open: Save line is DOC's state refusal in a danger banner holding
// focus, with no Reload, and the typed value stays.
func TestE2E_LinePagePastDraftRefused(t *testing.T) {
	h := Start(t)
	res := refPO(h)
	path := DocPath(fixture.Order, res.ID)
	p := h.SignIn(priya, phone360)
	p.Open(path + "/lines/1")
	p.WaitText(saveLine)
	p.Fill("qty", "44")
	h.Do(priya, fixture.Order, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	p.Press(saveLine)
	p.Until("focus on the danger banner", `document.activeElement.id === "line-alert" && document.activeElement.querySelector(".banner--danger") !== null`)
	if got := Eval[string](p, `location.pathname`); got != path+"/lines/1" {
		t.Errorf("the refusal left the page for %s", got)
	}
	if got := Eval[string](p, `document.getElementById("`+components.FieldID("qty")+`").value`); got != "44" {
		t.Errorf("qty after the refusal = %q, want the typed 44", got)
	}
	if got := Eval[int](p, `document.querySelectorAll("main .banner--warning").length`); got != 0 {
		t.Errorf("%d warning banners after a state refusal, want 0", got)
	}
	if p.Shows("Your entries stay") || p.Shows("Reload") {
		t.Errorf("a state refusal offers Reload\n%s", p.page())
	}
}
