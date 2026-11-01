//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// TestE2E_LineGridConflictBanner: CD-362. A grid row saved against a stale
// version is DOC's 409, swapped in place by htmx: the warning ConflictBanner
// in #grid-alerts with "Reload Draft {n}", and the typed value kept.
func TestE2E_LineGridConflictBanner(t *testing.T) {
	h := Start(t)
	id := h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
			"order_date": plainDate, "amount": "1000.00",
		},
		Lines: []map[string]string{
			{"item_id": strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10), "qty": "10.000", "rate": "62.5000", "uom": "kg"},
			{"item_id": strconv.FormatInt(h.M.Item["WIRE-18G"], 10), "qty": "20.000", "rate": "96.0000", "uom": "kg"},
		},
	}).ID
	items := DocPath(fixture.Order, id) + "/edit/items"
	const grid = `document.getElementById("grid-2-qty")?.checkVisibility() === true`
	first := h.SignIn(priya, desktop)
	first.Open(items)
	first.Until("the grid", grid)
	second := first.SecondTab()
	second.Open(items)
	second.Until("the grid", grid)

	first.Front()
	version := orderVersion(h, id)
	focusSelected(first, "grid-1-qty")
	first.Keys("11")
	first.Tab()
	first.Until("the first tab's save", `document.getElementById("doc-version").value === "`+strconv.Itoa(version+1)+`"`)

	second.Front()
	Eval[bool](second, `window.e6cSamePage = true`) // gone if the 409 loads a new page
	focusSelected(second, "grid-2-qty")
	second.Keys("22")
	second.Tab()
	second.WaitText(staleText(id))
	if !Eval[bool](second, `window.e6cSamePage === true`) {
		t.Error("the 409 loaded a new page; htmx must swap it in place")
	}
	if got := Eval[int](second, `document.querySelectorAll("#grid-alerts .banner--warning").length`); got != 1 {
		t.Errorf("%d warning banners in #grid-alerts, want 1", got)
	}
	if got := Eval[int](second, `document.querySelectorAll("#grid-alerts .banner--danger").length`); got != 0 {
		t.Errorf("%d danger banners in #grid-alerts, want 0", got)
	}
	if !second.Shows("Your entries stay on this page until you reload, so you can copy them.") {
		t.Errorf("the conflict banner has no keep-your-entries line\n%s", second.page())
	}
	if got := Eval[string](second, `document.getElementById("grid-2-qty").value`); got != "22" {
		t.Errorf("line 2 qty after the conflict = %q, want the typed 22", got)
	}
	if got := gridQty(h, id, 2); got != "20000" {
		t.Errorf("stored line 2 qty = %s, want 20000 (unchanged)", got)
	}
	second.Press("Reload " + draft(id))
	second.WaitGone(staleText(id))
	second.Until("the reloaded grid", `Number(document.getElementById("grid-1-qty")?.value) === 11`)
}
