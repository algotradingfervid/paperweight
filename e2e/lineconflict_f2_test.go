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

// TestE2E_LinePageConflictBanner: F2 (CD-362, CD-379). A line page saved
// against a stale version, an existing line and a new one, shows the warning
// ConflictBanner holding focus; the typed value stays, and Reload lands on
// that line page with the saved value.
func TestE2E_LinePageConflictBanner(t *testing.T) {
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
	base := DocPath(fixture.Order, id)
	qty := `document.getElementById("` + components.FieldID("qty") + `")`
	first := h.SignIn(priya, phone360)
	second := first.SecondTab()

	for i, path := range []string{"/lines/1", "/lines/new"} {
		first.Front()
		first.Open(base + "/lines/2")
		first.WaitText(saveLine)
		second.Front()
		second.Open(base + path)
		second.WaitText(saveLine)

		first.Front()
		saved := strconv.Itoa(30 + i)
		first.Fill("qty", saved)
		first.Press(saveLine)
		first.Until("back on Items", `location.pathname.endsWith("/edit/items")`)

		second.Front()
		second.Fill("qty", "44")
		second.Press(saveLine)
		second.WaitText(staleText(id))
		second.Until("focus on the banner", `document.activeElement.id === "line-alert" && document.activeElement.querySelector(".banner--warning") !== null`)
		if got := Eval[int](second, `document.querySelectorAll("main .banner--danger").length`); got != 0 {
			t.Errorf("%s: %d danger banners after the 409, want 0", path, got)
		}
		if !second.Shows("Your entries stay on this page until you reload, so you can copy them.") {
			t.Errorf("%s: the conflict banner has no keep-your-entries line\n%s", path, second.page())
		}
		if got := Eval[string](second, qty+`.value`); got != "44" {
			t.Errorf("%s: qty after the conflict = %q, want the typed 44", path, got)
		}
		second.Press("Reload " + draft(id))
		second.WaitGone(staleText(id))
		second.WaitText(saveLine)
		if got := Eval[string](second, `location.pathname`); got != base+path {
			t.Errorf("Reload landed on %s, want %s", got, base+path)
		}
		if got := Eval[string](second, `document.querySelector("input[name=version]").value`); got != strconv.Itoa(orderVersion(h, id)) {
			t.Errorf("%s: reloaded version = %s, want the stored %d", path, got, orderVersion(h, id))
		}
	}
}
