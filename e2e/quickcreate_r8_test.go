//go:build e2e

package e2e

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// holdAnswers makes the page keep every htmx answer that arrives while
// window.__hold is on, unswapped, until the test releases it: the real answer
// of a slow search, landing when the test says.
const holdAnswers = `(() => {
  const send = XMLHttpRequest.prototype.send;
  window.__held = [];
  XMLHttpRequest.prototype.send = function (b) {
    if (window.__hold) {
      const x = this, load = x.onload;
      x.onload = function (e) { window.__held.push(() => load.call(x, e)); };
    }
    return send.call(this, b);
  };
})()`

// sheetWidths are the widths a quick sheet is checked at.
var sheetWidths = []int64{320, 360, 390, 768}

// TestE2E_QuickCreateLateAnswer (UI v2 §5.8, §7, PO review H1): a search answer
// landing while the sheet is open never closes it, redraws it, moves focus or
// loses typing, whether New was pressed before the answer, in the same tick,
// or after it.
func TestE2E_QuickCreateLateAnswer(t *testing.T) {
	const list = "f-vendor_id-list"
	sheet := `document.getElementById("` + list + `-sheet")`
	newRow := `document.querySelector("#` + list + ` [data-quick]")`
	for _, w := range sheetWidths {
		for _, timing := range []string{"before", "during", "after"} {
			t.Run(fmt.Sprintf("%d/%s", w, timing), func(t *testing.T) {
				h := startPurchase(t)
				p := h.SignIn(admin, w)
				p.Open("/modules/purchase")
				p.Press("New purchase order")
				onStep(p, "Step 1 of 4")
				input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
				p.Do("hold answers", chromedp.Evaluate[chromedp.Void](holdAnswers))
				p.Do("search", chromedp.Click(input), chromedp.SendKeys(input, "Sai"))
				p.Until("the first answer", newRow+`?.textContent.includes('"Sai"')`)
				p.Do("hold", chromedp.Evaluate[chromedp.Void](`window.__hold = true`))
				p.Do("search more", chromedp.SendKeys(input, " Krupa"))
				p.Until("the held answer", `window.__held.length === 1`)
				p.Do("hold no more", chromedp.Evaluate[chromedp.Void](`window.__hold = false`))

				wantName := "Sai"
				switch timing {
				case "before":
					clickOn(p, "#"+list+" [data-quick]")
				case "during":
					p.Do("press and land", chromedp.Evaluate[chromedp.Void](newRow+`.click(); window.__held.shift()();`))
				case "after":
					wantName = "Sai Krupa"
					p.Do("land", chromedp.Evaluate[chromedp.Void](`window.__held.shift()()`))
					p.Until("the late answer", newRow+`?.textContent.includes('"Sai Krupa"')`)
					settle(p)
					clickOn(p, "#"+list+" [data-quick]")
				}
				p.Until("the sheet", sheet+`?.open === true`)
				p.Fill("quick_city", "Pune")
				p.Fill("quick_name", wantName+" Traders")
				p.Do("mark the sheet", chromedp.Evaluate[chromedp.Void](`window.__sheet = `+sheet))
				if timing == "before" {
					p.Do("land", chromedp.Evaluate[chromedp.Void](`window.__held.shift()()`))
				}
				p.Until("the search's latest New row", newRow+`?.textContent.includes('"Sai Krupa"')`)
				settle(p)
				if !Eval[bool](p, sheet+` === window.__sheet && window.__sheet.open`) {
					t.Fatal("the answer closed or redrew the sheet")
				}
				if got := Eval[string](p, `document.activeElement?.id ?? ""`); got != "f-quick_name" {
					t.Errorf("focus = %q, want the Name being typed in", got)
				}
				p.Do("keep typing", chromedp.KeyEvent(" Co"))
				if got := Eval[string](p, `document.getElementById("f-quick_name")?.value ?? "gone"`); got != wantName+" Traders Co" {
					t.Errorf("Name = %q, want %q", got, wantName+" Traders Co")
				}
				if got := Eval[string](p, `document.getElementById("f-quick_city")?.value ?? "gone"`); got != "Pune" {
					t.Errorf("City = %q, want Pune", got)
				}
			})
		}
	}
}

// sheetFits reports that Add and choose in the open sheet is on screen, not
// covered; that a focused field scrolls clear of it, for every field; and that
// with the box scrolled to its end the last control ends above the bar.
func sheetFits(sheet string) string {
	return `(() => {
  const d = ` + sheet + `, box = d.querySelector(".modal-box");
  const b = [...d.querySelectorAll("button")].find((e) => e.textContent.trim() === "Add and choose");
  const seen = () => {
    const r = b.getBoundingClientRect(), f = box.getBoundingClientRect();
    return r.bottom <= Math.min(innerHeight, f.bottom) + 0.5 && r.top >= f.top && b.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2));
  };
  box.scrollTop = 0;
  if (!seen()) return false;
  for (const i of d.querySelectorAll("input:not([type=hidden], [role=combobox]), select")) {
    if (!i.checkVisibility()) continue;
    i.focus();
    const r = i.getBoundingClientRect();
    if (!i.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))) return false;
  }
  box.scrollTop = box.scrollHeight;
  const ctls = [...d.querySelectorAll(".field")].filter((e) => e.checkVisibility());
  const end = ctls[ctls.length - 1].getBoundingClientRect().bottom;
  const fits = seen() && end <= b.closest(".quick__actions").getBoundingClientRect().top + 0.5;
  box.scrollTop = 0;
  return fits;
})()`
}

// shownLabels are the sheet's visible field labels.
func shownLabels(sheet string) string {
	return `[...` + sheet + `.querySelectorAll(".field__label")].filter((e) => e.checkVisibility()).map((e) => e.textContent.replace(/\s+/g, " ").trim()).join("|")`
}

// TestE2E_QuickCreateVendorSheet (UI v2 §5.8, CD-388, PO review): at every
// width, also on a short screen, the sheet asks Name, GSTIN, City, Line 1 and
// State; Add and choose stays reachable; a refusal focuses the summary, and
// answering it clears it; a valid GSTIN hides State and gives it, a wrong one
// shows it again; the save leaves the party and its primary billing address,
// and the chosen box shows the tile and line.
func TestE2E_QuickCreateVendorSheet(t *testing.T) {
	const (
		list  = "f-vendor_id-list"
		valid = "27AAFCV2449G1Z7"
	)
	sheet := `document.getElementById("` + list + `-sheet")`
	state := `document.getElementById("f-quick_state_code")`
	for _, w := range sheetWidths {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			h := startPurchase(t)
			p := h.SignIn(admin, w)
			p.Open("/modules/purchase")
			p.Press("New purchase order")
			onStep(p, "Step 1 of 4")
			input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
			p.Do("search the vendor", chromedp.Click(input), chromedp.SendKeys(input, "Sai Krupa"))
			p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.includes('"Sai Krupa"')`)
			clickOn(p, "#"+list+" [data-quick]")
			p.Until("the sheet", sheet+`?.open === true`)
			if got, want := Eval[string](p, shownLabels(sheet)), "Name * (required)|GSTIN|City * (required)|Address line 1 * (required)|State * (required)"; got != want {
				t.Errorf("labels = %q, want %q", got, want)
			}
			noOverflow(p, "the sheet")
			p.Do("a short screen", chromedp.EmulateViewport(w, 480))
			p.Until("Add and choose in reach on a short screen", sheetFits(sheet))
			p.Do("the full screen", chromedp.EmulateViewport(w, 800))

			p.Press("Add and choose")
			p.Until("focus on the summary", `document.activeElement?.matches("#`+list+`-sheet .error-summary")`)
			p.Until("the State error", `document.getElementById("f-quick_state_code-msg").textContent.includes("State is required")`)
			p.Until("Add and choose in reach after a refusal", sheetFits(sheet))
			// Each answer clears its own error; the summary goes with the last.
			summary := sheet + `.querySelectorAll(".error-summary").length`
			p.Fill("quick_city", "Pune")
			p.Until("the City error gone", `document.getElementById("f-quick_city-msg").textContent === "" && !document.getElementById("f-quick_city").hasAttribute("aria-invalid")`)
			if n := Eval[int64](p, summary); n != 1 {
				t.Errorf("%d summaries while Line 1 and State are still refused, want 1", n)
			}
			p.Fill("quick_line1", "Survey 42, Hadapsar")
			if n := Eval[int64](p, summary); n != 1 {
				t.Errorf("%d summaries while State is still refused, want 1", n)
			}
			p.Do("choose the state", chromedp.Evaluate[chromedp.Void](`(() => { const s = `+state+`; s.value = "27"; s.dispatchEvent(new Event("input", {bubbles: true})); })()`))
			p.Until("the summary gone with the last error", summary+` === 0`)

			blur := func() {
				p.Do("leave GSTIN", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-quick_line1").focus()`))
			}
			p.Fill("quick_gstin", valid)
			blur()
			p.Until("State given and hidden", `!`+state+`.checkVisibility()`)
			p.Fill("quick_gstin", "27AAFCV2449G1Z8")
			blur()
			p.Until("State back for a wrong GSTIN", state+`.checkVisibility()`)
			// A valid GSTIN that names no state (99) gives none: State stays.
			p.Fill("quick_gstin", "99AABCS1429B1ZJ")
			blur()
			p.Until("the 99 GSTIN checked", `document.getElementById("f-quick_gstin-msg").textContent.trim() === "" && !document.getElementById("f-quick_gstin").hasAttribute("aria-invalid")`)
			settle(p)
			if !Eval[bool](p, state+`.checkVisibility()`) {
				t.Error("State hid for a GSTIN that names no state")
			}
			p.Fill("quick_gstin", valid)
			blur()
			p.Until("State given again", `!`+state+`.checkVisibility()`)
			p.Press("Add and choose")

			picker := `document.querySelector("#` + list + `").closest(".picker")`
			p.Until("the vendor chosen", `!document.querySelector("dialog[open]") && `+picker+`.querySelector("[data-picker-summary]").textContent.trim() === "SAIKRUPA Sai Krupa"`)
			if got := Eval[string](p, picker+`.querySelector("[data-picker-line]").textContent.trim()`); got != valid+" · Pune" {
				t.Errorf("chosen line = %q", got)
			}
			if got := Eval[string](p, picker+`.querySelector("[data-picker-tile]").textContent.trim()`); got != "SK" {
				t.Errorf("chosen tile = %q, want SK", got)
			}
			noOverflow(p, "the chosen vendor")
			if n := Scalar[int64](h, `SELECT count(*) FROM party p JOIN address a ON a.party_id = p.id
				WHERE p.code = 'SAIKRUPA' AND p.is_vendor = 1 AND p.pan = 'AAFCV2449G' AND a.gstin = ? AND a.state_code = '27' AND a.is_primary_billing = 1 AND a.is_billing = 1`, valid); n != 1 {
				t.Errorf("saved vendor with its primary billing address and GSTIN details = %d, want 1", n)
			}
		})
	}
}

// TestE2E_QuickCreateItemLateAnswer: the item sheet at each width. HSN/SAC
// carries the star, and a search answer landing while the sheet's own picker
// is in use changes nothing in it.
func TestE2E_QuickCreateItemLateAnswer(t *testing.T) {
	const list = "f-item_id-list"
	sheet := `document.getElementById("` + list + `-sheet")`
	for _, w := range sheetWidths {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			h := startPurchase(t)
			po := h.metroDraft(admin)
			p := h.SignIn(admin, w)
			p.Open(DocPath(purchase.DocType, po) + "/lines/new")
			p.WaitText(saveLine)
			input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
			p.Do("hold answers", chromedp.Evaluate[chromedp.Void](holdAnswers))
			p.Do("search", chromedp.Click(input), chromedp.SendKeys(input, "TMT"))
			p.Until("the first answer", `document.querySelector("#`+list+` [data-quick]")?.textContent.includes('"TMT"')`)
			p.Do("hold", chromedp.Evaluate[chromedp.Void](`window.__hold = true`))
			p.Do("search more", chromedp.SendKeys(input, " bar 12"))
			p.Until("the held answer", `window.__held.length === 1`)
			p.Do("hold no more", chromedp.Evaluate[chromedp.Void](`window.__hold = false`))
			clickOn(p, "#"+list+" [data-quick]")
			p.Until("the sheet", sheet+`?.open === true`)
			if got, want := Eval[string](p, shownLabels(sheet)), "Name * (required)|UoM * (required)|HSN/SAC * (required)|Tax Code"; got != want {
				t.Errorf("labels = %q, want %q", got, want)
			}
			p.Fill("quick_hsn_sac", "7214")
			p.Pick("quick_stock_uom_id", "KGS")
			p.Do("mark the sheet", chromedp.Evaluate[chromedp.Void](`window.__sheet = `+sheet))
			nested := `document.querySelector('` + `#` + list + `-sheet [aria-controls="f-quick_default_tax_code_id-list"]')`
			p.Do("open the sheet's own picker", chromedp.Evaluate[chromedp.Void](nested+`.focus()`))
			p.Do("land", chromedp.Evaluate[chromedp.Void](`window.__held.shift()()`))
			p.Until("the late answer", `document.querySelector("#`+list+` [data-quick]")?.textContent.includes('"TMT bar 12"')`)
			settle(p)
			if !Eval[bool](p, sheet+` === window.__sheet && window.__sheet.open && document.activeElement === `+nested) {
				t.Fatal("the answer closed the sheet or took focus from its own picker")
			}
			if got := Eval[string](p, `document.getElementById("f-quick_hsn_sac").value`); got != "7214" {
				t.Errorf("HSN = %q, want 7214", got)
			}
			if got := Eval[string](p, `document.querySelector('`+`#`+list+`-sheet input[name="quick_stock_uom_id"]').value`); got == "" {
				t.Error("the chosen UoM was lost")
			}
			p.Do("close the sheet's own picker", chromedp.Evaluate[chromedp.Void](nested+`.blur()`))
			p.Until("Add and choose in reach", sheetFits(sheet))
			p.Press("Add and choose")
			p.Until("the item chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-summary]").textContent.trim() === "TMT TMT"`)
		})
	}
}

// TestE2E_QuickCreateCustomer: Party's other variant, from a project's
// customer picker. It saves a customer, not a vendor.
func TestE2E_QuickCreateCustomer(t *testing.T) {
	const list = "f-customer_id-list"
	h := startPurchase(t)
	p := h.SignIn(admin, 390)
	p.Open("/m/project/new")
	input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
	p.Do("search the customer", chromedp.Click(input), chromedp.SendKeys(input, "Lotus Homes"))
	p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.replace(/\s+/g, " ").trim() === 'New customer "Lotus Homes"'`)
	clickOn(p, "#"+list+" [data-quick]")
	p.Until("the sheet", `document.getElementById("`+list+`-sheet")?.open === true`)
	p.Fill("quick_city", "Pune")
	p.Fill("quick_line1", "Baner Road")
	p.Do("choose the state", chromedp.Evaluate[chromedp.Void](`(() => { const s = document.getElementById("f-quick_state_code"); s.value = "27"; s.dispatchEvent(new Event("input", {bubbles: true})); })()`))
	p.Press("Add and choose")
	p.Until("the customer chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-summary]").textContent.trim() === "LOTUSHOMES Lotus Homes"`)
	if n := Scalar[int64](h, `SELECT count(*) FROM party WHERE code = 'LOTUSHOMES' AND is_customer = 1 AND is_vendor = 0`); n != 1 {
		t.Errorf("saved customers = %d, want 1", n)
	}
}

// TestE2E_PickerRowsAndChosen (UI v2 §5.8, PO review): a vendor row shows its
// tile, name, code, GSTIN and city, and the chosen value the box with the
// same, at every width.
func TestE2E_PickerRowsAndChosen(t *testing.T) {
	const list = "f-vendor_id-list"
	for _, w := range sheetWidths {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			h := startPurchase(t)
			h.metroDraft(admin)
			p := h.SignIn(admin, w)
			p.Open("/modules/purchase")
			p.Press("New purchase order")
			onStep(p, "Step 1 of 4")
			input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
			if got := Eval[string](p, `document.querySelector('input[aria-controls="`+list+`"]').placeholder`); got != "Name, code or GSTIN" {
				t.Errorf("placeholder = %q, want the field's own", got)
			}
			// The draft's vendor comes back as a recent pick, with its line,
			// the typed letter marked in its text and never in its tile.
			row := `document.querySelector("#` + list + ` [role=option][data-value]")`
			p.Do("search", chromedp.Click(input), chromedp.SendKeys(input, "S"))
			p.Until("the marked row", row+`?.querySelector(".picker__name mark") != null`)
			if n := Eval[int64](p, `document.querySelectorAll("#`+list+` .picker__tile mark").length`); n != 0 {
				t.Errorf("%d marks in the tiles, want none", n)
			}
			p.Do("search more", chromedp.SendKeys(input, "hree"))
			p.Until("the count", `document.getElementById("`+list+`-count")?.textContent === '1 vendor matches "Shree".'`)
			p.Until("the row", row+`?.querySelector(".picker__code")?.textContent === "SHREE"`)
			if got := Eval[string](p, row+`.querySelector(".picker__tile").textContent`); got != "SS" {
				t.Errorf("tile = %q, want SS", got)
			}
			line := Eval[string](p, row+`.querySelector(".picker__line").textContent`)
			if !strings.HasPrefix(line, shreeGSTIN+" · ") {
				t.Errorf("row line = %q, want the GSTIN and city", line)
			}
			if h := Eval[float64](p, row+`.getBoundingClientRect().height`); h < 60 {
				t.Errorf("row height = %v, want at least 60", h)
			}
			noOverflow(p, "the rows")
			p.Do("choose", chromedp.Evaluate[chromedp.Void](row+`.click()`))
			picker := `document.querySelector("#` + list + `").closest(".picker")`
			p.Until("the chosen box", picker+`.querySelector("[data-picker-line]").textContent === `+strconv.Quote(line))
			if got := Eval[string](p, picker+`.querySelector("[data-picker-tile]").textContent`); got != "SS" {
				t.Errorf("chosen tile = %q, want SS", got)
			}
			if !Eval[bool](p, `getComputedStyle(`+picker+`.querySelector(".picker__chosen")).borderTopStyle === "solid" && (() => { const b = `+picker+`.querySelector("[data-picker-change]").getBoundingClientRect(), s = `+picker+`.querySelector("[data-picker-summary]").getBoundingClientRect(); return b.left >= s.right - 0.5 || b.top >= s.bottom - 0.5; })()`) {
				t.Error("the chosen value isn't a bordered box with Change clear of the text")
			}
			noOverflow(p, "the chosen box")
		})
	}
}

// TestE2E_QuickCreateReopen: a sheet closed and opened again from the same New
// keeps what was typed; a different search opens a fresh one; and after a save
// empties the slot the same New opens one again.
func TestE2E_QuickCreateReopen(t *testing.T) {
	const list = "f-vendor_id-list"
	sheet := `document.getElementById("` + list + `-sheet")`
	h := startPurchase(t)
	p := h.SignIn(admin, 390)
	p.Open("/modules/purchase")
	p.Press("New purchase order")
	onStep(p, "Step 1 of 4")
	input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
	asks := func(q string) {
		t.Helper()
		p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.includes('"`+q+`"')`)
		settle(p)
		clickOn(p, "#"+list+" [data-quick]")
		p.Until("the sheet", sheet+`?.open === true`)
	}
	p.Do("search", chromedp.Click(input), chromedp.SendKeys(input, "Sai"))
	asks("Sai")
	p.Fill("quick_city", "Pune")
	p.Press("Cancel")
	p.Until("the sheet shut", sheet+`?.open === false`)
	asks("Sai")
	if got := Eval[string](p, `document.getElementById("f-quick_city").value`); got != "Pune" {
		t.Errorf("City after reopening = %q, want kept", got)
	}
	p.Press("Cancel")
	p.Until("the sheet shut", sheet+`?.open === false`)
	p.Do("search more", chromedp.SendKeys(input, " Traders"))
	asks("Sai Traders")
	if got := Eval[string](p, `document.getElementById("f-quick_name").value + "|" + document.getElementById("f-quick_city").value`); got != "Sai Traders|" {
		t.Errorf("Name|City = %q, want a fresh sheet", got)
	}
	p.Fill("quick_city", "Pune")
	p.Fill("quick_line1", "Hadapsar")
	p.Do("choose the state", chromedp.Evaluate[chromedp.Void](`(() => { const s = document.getElementById("f-quick_state_code"); s.value = "27"; s.dispatchEvent(new Event("input", {bubbles: true})); })()`))
	p.Press("Add and choose")
	p.Until("the vendor chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-value]").value !== ""`)
	settle(p)
	clickOn(p, "[data-picker-change]")
	p.Until("the search focused", `document.activeElement?.matches("input[aria-controls='`+list+`']")`)
	p.Do("type the same", chromedp.KeyEvent("Sai Traders"))
	asks("Sai Traders")
}

// TestE2E_GRNSelectEcho (GRN review M5): with 7 orders step 1 is a Select
// with a Choose an order prompt; choosing one echoes its vendor, site and
// pending lines under it, at the phone widths, without moving focus.
func TestE2E_GRNSelectEcho(t *testing.T) {
	h := Start(t)
	code := func(table, c string) string {
		return strconv.FormatInt(Scalar[int64](h, `SELECT id FROM `+table+` WHERE code = ?`, c), 10) //nolint:gosec // G202: fixed table names
	}
	var last int64
	for range 7 {
		r := h.Do(admin, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
			"vendor_id": code("party", "SHREE"), "site_id": code("site", "METRO-SITE"), "project_id": code("project", "PRJ-METRO"), "order_date": plainDate,
		}, Lines: []map[string]string{{"item_id": code("item", "WIRE-18G"), "uom_id": code("uom", "KGS"), "qty": "1", "price": "1"}}})
		r = h.Do(admin, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
		last = r.ID
	}
	site := Scalar[string](h, `SELECT name FROM site WHERE code = 'METRO-SITE'`)
	want := "Shree Steel Traders · " + site + " · 1 of 1 line pending"
	sel := `document.querySelector("select[name=purchase_order_id]")`
	for _, w := range []int64{320, 360} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			s := h.SignIn(suresh, w)
			s.Open("/doc/" + goodsreceipt.DocType + "/new")
			onStep(s, "Step 1 of 4")
			if got := Eval[string](s, sel+`.selectedOptions[0].textContent.trim()`); got != "Choose an order" {
				t.Errorf("unopened Select shows %q, want the prompt", got)
			}
			if got := Eval[string](s, `document.getElementById("f-purchase_order_id-msg").textContent.trim()`); got != "" {
				t.Errorf("echo before a choice = %q", got)
			}
			s.Do("choose an order", chromedp.Evaluate[chromedp.Void](`(() => { const s = `+sel+`; s.focus(); s.value = "`+strconv.FormatInt(last, 10)+`"; s.dispatchEvent(new Event("change", {bubbles: true})); })()`))
			s.Until("the echo", `document.getElementById("f-purchase_order_id-msg")?.textContent.trim() === `+strconv.Quote(want))
			settle(s)
			if !Eval[bool](s, `document.activeElement === `+sel) {
				t.Error("the echo took focus from the Select")
			}
			if got := Eval[string](s, sel+`.value`); got != strconv.FormatInt(last, 10) {
				t.Errorf("Select value after the echo = %q", got)
			}
			noOverflow(s, "the Select with its echo")
		})
	}
}
