//go:build e2e

package e2e

import (
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// TestE2E_QuickCreateVendor (UI v2 A-19, CD-375): on the PO wizard at 360 px a
// search with no match offers New vendor "Sai Krupa"; a refusal shows in the
// sheet; then the sheet saves Name, Line 1, City and State, closes, and the
// vendor is chosen, while the typed Order date stays as typed.
func TestE2E_QuickCreateVendor(t *testing.T) {
	const (
		list    = "f-vendor_id-list"
		orderOn = "15-10-2026"
	)
	h := startPurchase(t)
	p := h.SignIn(admin, phone360)
	p.Open("/modules/purchase")
	p.Press("New purchase order")
	onStep(p, "Step 1 of 4")
	p.Fill("order_date", orderOn)

	input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
	p.Do("search the vendor", chromedp.Click(input), chromedp.SendKeys(input, "Sai Krupa"))
	p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.replace(/\s+/g, " ").trim() === 'New vendor "Sai Krupa"'`)
	p.WaitText(`No vendor matches "Sai Krupa"`)
	noOverflow(p, "the vendor search with no match")
	clickOn(p, "#"+list+" [data-quick]")
	p.Until("the sheet", `document.getElementById("`+list+`-sheet")?.open === true`)
	if got := Eval[string](p, `document.getElementById("f-quick_name").value`); got != "Sai Krupa" {
		t.Errorf("Name = %q, want the search Sai Krupa", got)
	}
	if got := Eval[string](p, `document.getElementById("f-quick_code").value`); got != "SAIKRUPA" {
		t.Errorf("Code = %q, want SAIKRUPA", got)
	}
	noOverflow(p, "the quick-create sheet")
	p.Fill("quick_line1", "Survey 42, Hadapsar")
	// A refusal (422) redraws the sheet's form in place, with the field error.
	p.Press("Add and choose")
	p.Until("the City error in the sheet", `document.getElementById("`+list+`-sheet")?.open === true && document.getElementById("f-quick_city-msg")?.textContent.includes("City is required.")`)
	if got := Eval[string](p, `document.getElementById("f-quick_line1").value`); got != "Survey 42, Hadapsar" {
		t.Errorf("Line 1 after the refusal = %q, want kept", got)
	}
	p.Fill("quick_city", "Pune")
	p.Do("choose the state", chromedp.Evaluate[chromedp.Void](`(() => { const s = document.getElementById("f-quick_state_code"); s.value = "27"; s.dispatchEvent(new Event("change", {bubbles: true})); })()`))
	p.Press("Add and choose")

	p.Until("the vendor chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-summary]").textContent.trim() === "SAIKRUPA Sai Krupa"`)
	id := Scalar[int64](h, `SELECT p.id FROM party p JOIN address a ON a.party_id = p.id
		WHERE p.code = 'SAIKRUPA' AND p.is_vendor = 1 AND a.is_primary_billing = 1 AND a.line1 = 'Survey 42, Hadapsar' AND a.city = 'Pune' AND a.state_code = '27'`)
	if got := Eval[string](p, `document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-value]").value`); got != strconv.FormatInt(id, 10) {
		t.Errorf("vendor_id = %q, want the new party %d", got, id)
	}
	if got := Eval[string](p, `document.getElementById("f-order_date").value`); got != orderOn {
		t.Errorf("Order date = %q, want the typed %s", got, orderOn)
	}
	p.Until("focus on Change", `document.activeElement?.matches("[data-picker-change]") && document.activeElement.closest(".picker").contains(document.querySelector("#`+list+`"))`)
	noOverflow(p, "the chosen new vendor")
}

// TestE2E_QuickCreateItemNestedPickers (D2 review): the item sheet's own
// pickers (UoM, Tax code) never reach the line's item picker. Choosing and
// clearing the Tax code inside the sheet keeps it open with the typed Code,
// Name and HSN; then the item is saved without a tax code and chosen.
func TestE2E_QuickCreateItemNestedPickers(t *testing.T) {
	const list = "f-item_id-list"
	h := startPurchase(t)
	po := h.metroDraft(admin)
	p := h.SignIn(admin, phone360)
	p.Open(DocPath(purchase.DocType, po) + "/lines/new")
	p.WaitText(saveLine)

	input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
	p.Do("search the item", chromedp.Click(input), chromedp.SendKeys(input, "TMT bar 12"))
	p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.replace(/\s+/g, " ").trim() === 'New item "TMT bar 12"'`)
	clickOn(p, "#"+list+" [data-quick]")
	p.Until("the sheet", `document.getElementById("`+list+`-sheet")?.open === true`)
	p.Fill("quick_code", "TMT-12")
	p.Fill("quick_hsn_sac", "7214")
	p.Pick("quick_stock_uom_id", "KGS")
	p.Pick("quick_default_tax_code_id", "GST18")
	clickOn(p, `#`+list+`-sheet [data-picker-clear][aria-label="Clear Tax Code"]`)
	p.Until("the tax code cleared", `document.querySelector('#`+list+`-sheet input[name="quick_default_tax_code_id"]').value === ""`)
	settle(p)
	p.Do("wait past a search's debounce", chromedp.Sleep(600*time.Millisecond))
	for id, want := range map[string]string{"f-quick_code": "TMT-12", "f-quick_name": "TMT bar 12", "f-quick_hsn_sac": "7214"} {
		if got := Eval[string](p, `document.getElementById("`+id+`")?.value ?? "gone"`); got != want {
			t.Errorf("%s = %q after the inner pickers, want %q", id, got, want)
		}
	}
	if !Eval[bool](p, `document.getElementById("`+list+`-sheet")?.open === true`) {
		t.Fatal("the sheet closed while its own pickers were used")
	}
	p.Press("Add and choose")
	p.Until("the item chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-summary]").textContent.trim() === "TMT-12 TMT bar 12"`)
	if n := Scalar[int64](h, `SELECT count(*) FROM item WHERE code = 'TMT-12' AND hsn_sac = '7214' AND default_tax_code_id IS NULL AND stock_uom_id = (SELECT id FROM uom WHERE code = 'KGS')`); n != 1 {
		t.Errorf("saved items TMT-12 with KGS, HSN 7214 and no tax code = %d, want 1", n)
	}
}
