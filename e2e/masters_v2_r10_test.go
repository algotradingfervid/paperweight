//go:build e2e

package e2e

import (
	"strconv"
	"testing"
)

// r10Widths is every supported width.
var r10Widths = []int64{320, 360, 390, 768, 1280}

// r10Layout measures a section-card form: nothing overflows sideways, each
// card stays inside the page, the tile keeps its 36 px and its heading a
// usable width, a description never covers the first control, and every
// control is at least 44 px tall where it can be tapped.
const r10Layout = `(() => {
  const bad = [], vw = document.documentElement.clientWidth;
  for (const c of document.querySelectorAll("main form section.card")) {
    const r = c.getBoundingClientRect(), name = c.querySelector(".card-title")?.textContent;
    if (r.left < 0 || r.right > vw) bad.push("card outside the page: " + name);
    const tile = c.querySelector(".card-icon").getBoundingClientRect();
    if (Math.round(tile.width) !== 36 || Math.round(tile.height) !== 36) bad.push("tile " + tile.width + "x" + tile.height + ": " + name);
    const head = c.querySelector(".card-titles").getBoundingClientRect();
    if (head.width < 150) bad.push("heading squeezed to " + head.width + ": " + name);
    const desc = c.querySelector(".card-desc").getBoundingClientRect();
    const first = c.querySelector("label, legend")?.getBoundingClientRect();
    if (first && first.top < desc.bottom - 1) bad.push("description over the first label: " + name);
  }
  for (const i of document.querySelectorAll("main form input[id]:not([type=hidden]):not([type=checkbox]):not([type=radio]), main form select")) {
    if (i.checkVisibility() && i.getBoundingClientRect().height < 44) bad.push("short control: " + i.id);
  }
  return bad;
})()`

// TestE2E_MasterFormsLayout (UI v2 R10): each master's form, at every
// supported width.
func TestE2E_MasterFormsLayout(t *testing.T) {
	h := Start(t)
	shree := "/m/party/" + h.idOf("party", "SHREE") // its address, contact and the site delivery sub-forms
	for _, width := range r10Widths {
		s := h.SignIn(admin, width)
		for _, path := range []string{
			"/m/party/new", "/m/employee/new", "/m/item/new", "/m/branch/new", "/m/project/new", "/m/site/new",
			"/m/tax-code/new", "/m/uom/new", "/m/payment-terms/new", "/m/expense-type/new", "/m/company/1",
			shree + "/addresses/new", shree + "/contacts/new", "/m/site/" + h.idOf("site", "METRO-SITE") + "/delivery",
		} {
			s.Open(path)
			noOverflow(s, path)
			if bad := Eval[[]string](s, r10Layout); len(bad) != 0 {
				t.Errorf("%s at %d px: %q", path, width, bad)
			}
		}
	}
}

// TestE2E_BranchStateFromGSTIN (UI v2 R10, shell H5): a valid GSTIN sets the
// State Select once its check answers, over a state chosen before; an invalid
// one leaves the Select alone, and so does a stale reply for a value the box
// no longer holds (CD-355 (1)).
func TestE2E_BranchStateFromGSTIN(t *testing.T) {
	const state = `document.getElementById("f-state_code").value`
	h := Start(t)
	s := h.SignIn(admin, phone390)
	s.Open("/m/branch/new")
	Eval[bool](s, `(() => { document.getElementById("f-state_code").value = "29"; return true; })()`)
	s.Fill("gstin", "27AAFCV2449G1Z8")
	s.Tab()
	s.WaitText("is not valid")
	if got := Eval[string](s, state); got != "29" {
		t.Errorf("state after an invalid GSTIN = %q; want the chosen 29", got)
	}
	// A reply for a GSTIN typed before: the box has moved on, so neither it
	// nor the state follow.
	Eval[bool](s, `(() => { const i = document.getElementById("f-gstin"); i.value = "29AAFCV2449G1Z3";
  document.dispatchEvent(new CustomEvent("ui:field", {detail: {id: "f-gstin", value: "27AAFCV2449G1Z7", sent: "27aafcv2449g1z7", invalid: false}})); return true; })()`)
	if got := Eval[string](s, state); got != "29" {
		t.Errorf("state after a stale reply = %q; want 29", got)
	}
	s.Fill("gstin", "27AAFCV2449G1Z7")
	s.Tab()
	s.Until("the state follows the GSTIN", state+` === "27"`)
	s.WaitText("Maharashtra (27)")
}

// TestE2E_PartyWizardLeftOutRows (UI v2 R10, M14 ruling): an address row
// opened and left empty is left out on Continue, and the contacts step says so.
func TestE2E_PartyWizardLeftOutRows(t *testing.T) {
	h := Start(t)
	s := h.SignIn(admin, phone390)
	s.Open("/m/party/new")
	s.Fill("code", "MEHTA")
	s.Fill("name", "Mehta Fabricators")
	clickOn(s, `label:has(input[name="role"][value="vendor"])`)
	Eval[bool](s, `(() => { document.getElementById("f-gst_category").value = "Unregistered"; return true; })()`)
	clickOn(s, ".actionbar .btn-primary") // the step's Continue
	s.WaitText("Add address")
	s.Press("Add address")
	s.Until("a first row", `!!document.getElementById("f-lines-1-line1")`)
	settle(s)
	clickOn(s, ".actionbar .btn-primary")
	s.WaitText("Left out the empty address.")
	if !Eval[bool](s, `[...document.querySelectorAll("#wizard .banner")].some((b) => b.textContent.includes("Left out the empty address."))`) {
		t.Error("the note isn't a banner in the step")
	}
}

// TestE2E_NumberingTargets (UI v2 R10, shell M12): each row is one link at
// least 44 px tall, at every supported width.
func TestE2E_NumberingTargets(t *testing.T) {
	h := Start(t)
	for _, width := range r10Widths {
		s := h.SignIn(admin, width)
		s.Open("/settings/numbering")
		noOverflow(s, "Numbering")
		n := Eval[int](s, `[...document.querySelectorAll("main a.datalist-link")].filter((a) => a.getBoundingClientRect().height >= 44).length`)
		all := Eval[int](s, `document.querySelectorAll("main a.datalist-link").length`)
		if all == 0 || n != all {
			t.Errorf("at %s px: %d of %d links reach 44 px", strconv.FormatInt(width, 10), n, all)
		}
	}
}
