//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// UI v2 R11 (GRN review H4, H5, M2, M11; CD-387 (7)).

const (
	r11Item   = "MS Angle 50x50x5"
	r11Height = 664
	r11BadQty = "99999999999" // past MaxQty: ParseQty's error on blur
)

// The worst-case line (R11 review): a 20-character code, a 64-character
// item name and 1,00,00,000 KGS pending.
const (
	r11WorstCode = "WWWWWWWWWWWWWWWWWWWW"
	r11WorstName = "MS Angle 50x50x5 IS 2062 E250 BR hot rolled, 6 m lengths, primed"
)

// r11Worst renames WIRE-18G (stocked in KGS) to the worst-case item and returns a PO line of
// it: 1,00,00,000 KGS at ₹1.
func (h *Harness) r11Worst() map[string]string {
	h.t.Helper()
	Scalar[int64](h, `UPDATE item SET code = ?, name = ? WHERE code = 'WIRE-18G' RETURNING id`, r11WorstCode, r11WorstName)
	return map[string]string{"item_id": h.idOf("item", r11WorstCode), "uom_id": h.idOf("uom", "KGS"), "qty": "10000000", "price": "1"}
}

// r11PO is an approved PO of n lines of MS-ANGLE-50 from SHREE at METRO-SITE,
// then the extra lines.
func (h *Harness) r11PO(n int, extra ...map[string]string) {
	h.t.Helper()
	item, uom := h.idOf("item", "MS-ANGLE-50"), h.idOf("uom", "KGS")
	lines := make([]map[string]string, n)
	for i := range lines {
		lines[i] = map[string]string{"item_id": item, "uom_id": uom, "qty": "1200", "price": "62.5"}
	}
	lines = append(lines, extra...)
	r := h.Do(priya, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"vendor_id": h.idOf("party", "SHREE"), "site_id": h.idOf("site", "METRO-SITE"), "project_id": h.idOf("project", "PRJ-METRO"), "order_date": plainDate,
	}, Lines: lines})
	h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
}

// r11Items signs Suresh in at width and walks a new GRN to Quantities.
func (h *Harness) r11Items(width int64) (*Session, string) {
	h.t.Helper()
	s := h.SignIn(suresh, width)
	s.Open("/doc/" + goodsreceipt.DocType)
	id := startGRN(s)
	grnDelivery(s, grnChallan)
	return s, "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10) + "/edit/items"
}

// r11Viewport sets a phone at width × r11Height.
func r11Viewport(s *Session, width int64) {
	s.t.Helper()
	s.Do("viewport", chromedp.EmulateViewport(width, r11Height, chromedp.EmulateMobile, chromedp.EmulateTouch))
}

// r11LayoutJS lists what is wrong with the compact cards as laid out now:
// neighbours that touch, a Some rejected smaller than a 44 px target or
// over the input or the title, an input not on top at its corners, a label
// or Some rejected on two lines, a title under Some rejected.
const r11LayoutJS = `(() => {
  const out = [];
  const box = (e) => e.getBoundingClientRect();
  const meets = (a, b) => a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom;
  const cards = [...document.querySelectorAll("#wizard .qty-line")].filter((c) => c.getClientRects().length > 0);
  if (cards.length === 0) return ["no cards"];
  cards.forEach((c, i) => {
    const r = box(c);
    if (i > 0 && r.top - box(cards[i - 1]).bottom < 8) out.push("card " + (i + 1) + " is " + (r.top - box(cards[i - 1]).bottom) + " px from the last");
    const some = c.querySelector(".qty-line__reject summary"), s = box(some);
    const input = c.querySelector("input[data-line-entry]"), n = box(input);
    const title = c.querySelector(".card-title"), range = document.createRange();
    range.selectNodeContents(title);
    const t = range.getBoundingClientRect();
    if (s.height < 44 || s.width < 44) out.push("Some rejected " + s.width + " x " + s.height);
    if (s.height > 44.5) out.push("Some rejected wraps: height " + s.height);
    if (meets(s, n)) out.push("Some rejected meets the input");
    if (meets(s, t)) out.push("Some rejected meets the title");
    for (const sp of c.querySelectorAll(".qty-line__item span")) {
      const sr = document.createRange();
      sr.selectNodeContents(sp);
      for (const rr of sr.getClientRects()) if (meets(rr, s)) out.push("Some rejected over " + sp.textContent);
    }
    if (input.scrollWidth > input.clientWidth + 1) out.push("the quantity " + input.value + " is cut: " + input.scrollWidth + " > " + input.clientWidth);
    if (s.right > r.right + 0.5 || s.top < r.top - 0.5) out.push("Some rejected leaves the card");
    const label = c.querySelector("label"), l = box(label);
    if (l.height > 24.5) out.push("label wraps: height " + l.height);
    if (r.top >= 0 && r.bottom <= innerHeight) {
      for (const [x, y] of [[n.left + 2, n.top + 2], [n.right - 60, n.top + 2], [n.left + 2, n.bottom - 2], [n.left + n.width / 2, n.top + n.height / 2]]) {
        const hit = document.elementFromPoint(x, y);
        if (hit && hit !== input && !hit.closest(".actionbar, .wizard-head") && !hit.closest(".field__control")) out.push("over the input: " + hit.className);
      }
      const hit = document.elementFromPoint(s.left + s.width / 2, s.top + s.height / 2);
      if (hit && !hit.closest(".actionbar, .wizard-head") && hit !== some) out.push("over Some rejected: " + hit.tagName + "." + hit.className);
    }
  });
  return out;
})()`

// r11FitJS is how many whole cards show between the top bar and the footer
// with the first card at the top, and the first card's height.
const r11FitJS = `(() => {
  const cards = [...document.querySelectorAll("#wizard .qty-line")];
  const head = document.querySelector(".wizard-head");
  scrollTo({ top: 400, behavior: "instant" }); // the head sticks from here
  scrollBy({ top: cards[0].getBoundingClientRect().top - head.getBoundingClientRect().bottom - 8, behavior: "instant" });
  const top = head.getBoundingClientRect().bottom;
  const foot = document.querySelector("#wizard .actionbar").getBoundingClientRect().top;
  return {
    fit: cards.filter((c) => { const r = c.getBoundingClientRect(); return r.top >= top && r.bottom <= foot; }).length,
    height: cards[0].getBoundingClientRect().height, top, foot,
  };
})()`

// r11Fit is r11FitJS's answer.
type r11Fit struct {
	Fit       int
	Height    float64
	Top, Foot float64
}

// H5, M2, A-12, CD-387 (7): a 36-line order on a phone at each supported
// width, 664 px tall. Three whole cards show on one screen, the height
// budget. The footer's count fits one line, no field ends under the footer
// when focused, the cards never touch, Some rejected is a 44 px target that
// covers neither the input nor the title, nothing wraps that shouldn't, and
// no width scrolls sideways; the same with Some rejected open.
func TestE2E_GRNQuantitiesCompact(t *testing.T) {
	h := Start(t)
	h.r11PO(35, h.r11Worst())
	s, items := h.r11Items(phone360)
	for _, w := range []int64{320, phone360, phone390} {
		s.width = w
		s.Open(items)
		r11Viewport(s, w)
		onStep(s, "Step 3 of 4")
		if got := Eval[string](s, `document.querySelector("#wizard .actionbar .actionbar-summary").textContent.trim()`); got != "36 of 36 lines entered" {
			t.Errorf("%d px: footer %q, want 36 of 36 lines entered", w, got)
		}
		if got := Eval[float64](s, `document.querySelector("#wizard .actionbar .actionbar-summary").getBoundingClientRect().height`); got > 24.5 {
			t.Errorf("%d px: footer summary %v px tall, want one 24 px line", w, got)
		}
		fit := Eval[r11Fit](s, r11FitJS)
		t.Logf("%d px: card %v px, %v px between bars, %d whole cards", w, fit.Height, fit.Foot-fit.Top, fit.Fit)
		want := 3
		if w == 320 { // the title and the hint each take one more line at 320 px
			want = 2
		}
		if fit.Fit < want {
			t.Errorf("%d px: %d whole cards on the screen (card %v px, %v px between bars), want at least %d", w, fit.Fit, fit.Height, fit.Foot-fit.Top, want)
		}
		if bad := Eval[[]string](s, r11LayoutJS); len(bad) != 0 {
			t.Errorf("%d px: %q", w, bad)
		}
		noOverflow(s, "Quantities with 36 lines")
		Eval[bool](s, `([...document.querySelectorAll("#wizard .qty-line")].at(-1).scrollIntoView({ block: "center", behavior: "instant" }), true)`)
		if got := Eval[string](s, `[...document.querySelectorAll("#wizard .qty-line")].at(-1).querySelector("input[data-line-entry]").value`); got != "1,00,00,000" {
			t.Errorf("%d px: the worst-case line's Received now = %q, want 1,00,00,000", w, got)
		}
		if bad := Eval[[]string](s, r11LayoutJS); len(bad) != 0 {
			t.Errorf("%d px, worst-case line in view: %q", w, bad)
		}
		if bad := Eval[[]string](s, footerJS); len(bad) != 0 {
			t.Errorf("%d px: fields under the footer: %q", w, bad)
		}
		Eval[bool](s, `(document.querySelector("#wizard .qty-line__reject summary").click(), true)`)
		s.Until("Some rejected open", `document.querySelector("#wizard .qty-line__reject details").open`)
		s.Until("Some rejected settled", `document.getAnimations().length === 0`)
		if bad := Eval[[]string](s, r11LayoutJS); len(bad) != 0 {
			t.Errorf("%d px, open: %q", w, bad)
		}
		if bad := Eval[[]string](s, footerJS); len(bad) != 0 {
			t.Errorf("%d px, open: fields under the footer: %q", w, bad)
		}
		noOverflow(s, "Quantities with Some rejected open")
	}
}

// r11RingJS is what is wrong with the focus ring of the focused Some
// rejected: none on the summary's own box, or one still on its details.
const r11RingJS = `(() => {
  const s = document.activeElement;
  if (!s || !s.matches(".qty-line__reject summary")) return ["focus is on " + (s ? s.outerHTML.slice(0, 80) : "nothing")];
  const out = [], st = getComputedStyle(s), r = s.getBoundingClientRect();
  if (r.width < 44 || r.height < 44) out.push("summary box " + r.width + " x " + r.height);
  const ring = (st.outlineStyle !== "none" && parseFloat(st.outlineWidth) >= 2) || (st.boxShadow !== "none" && st.boxShadow !== "");
  if (!ring) out.push("no ring on the summary: outline " + st.outlineStyle + " " + st.outlineWidth + ", shadow " + st.boxShadow);
  const d = getComputedStyle(s.closest("details"));
  if (d.outlineStyle !== "none" && parseFloat(d.outlineWidth) > 0) out.push("a ring on the details: " + d.outlineStyle + " " + d.outlineWidth);
  return out;
})()`

// UI v2 §5.5 focus, GRN review H5 (R11 review): Tab from Received now to
// Some rejected rings the summary itself, at each phone width.
func TestE2E_GRNQuantitiesRejectFocusRing(t *testing.T) {
	h := Start(t)
	h.r11PO(3)
	s, items := h.r11Items(phone360)
	for _, w := range []int64{320, phone360, phone390} {
		s.width = w
		s.Open(items)
		r11Viewport(s, w)
		onStep(s, "Step 3 of 4")
		fillCard(s, r11Name(2), "75")
		s.Tab()
		if bad := Eval[[]string](s, r11RingJS); len(bad) != 0 {
			t.Errorf("%d px: %q", w, bad)
		}
	}
}

// r11Name is the Received now input of line n.
func r11Name(n int) string {
	return "Received now, line " + strconv.Itoa(n) + ", " + r11Item
}

// r11Count is the footer's text.
const r11Count = `document.querySelector("#wizard .actionbar .actionbar-summary").textContent.trim()`

// r11Value is line n's Received now value.
func r11Value(s *Session, n int) string {
	s.t.Helper()
	return Eval[string](s, cardInput(r11Name(n))+`.value`)
}

// CD-387 (7), CLAUDE.md (a fragment never redraws the input being typed in):
// the footer's count follows typing with no request and the inputs stay the
// same elements with the typed values. A 0 and a blank aren't entered.
func TestE2E_GRNQuantitiesCount(t *testing.T) {
	h := Start(t)
	h.r11PO(3)
	s, _ := h.r11Items(phone360)
	if got := Eval[string](s, r11Count); got != "3 of 3 lines entered" {
		t.Errorf("prefilled: footer %q", got)
	}
	Eval[bool](s, `(window.r11Same = [...document.querySelectorAll("input[data-line-entry]")], window.r11Summary = document.querySelector("[data-lines-entered]"), true)`)
	fillCard(s, r11Name(2), "0")
	s.Until("the count after a 0", r11Count+` === "2 of 3 lines entered"`)
	fillCard(s, r11Name(2), "75")
	s.Until("the count after 75", r11Count+` === "3 of 3 lines entered"`)
	fillCard(s, r11Name(1), "1")
	s.Keys(kb.Backspace)
	s.Until("the count after a blank", r11Count+` === "2 of 3 lines entered"`)
	fillCard(s, r11Name(3), "1,234")
	if got := r11Value(s, 2); got != "75" {
		t.Errorf("line 2 = %q, want the typed 75", got)
	}
	if got := r11Value(s, 3); got != "1,234" {
		t.Errorf("line 3 = %q, want the typed 1,234", got)
	}
	if !Eval[bool](s, `[...document.querySelectorAll("input[data-line-entry]")].every((e, i) => e === window.r11Same[i]) && document.querySelector("[data-lines-entered]") === window.r11Summary`) {
		t.Errorf("an input or the count was redrawn while typing")
	}
}

// CD-387 (7), CD-287: with no reserved line, the empty message takes no room;
// a blur error appears under its input and focus stays where the user went.
func TestE2E_GRNQuantitiesBlurError(t *testing.T) {
	h := Start(t)
	h.r11PO(3)
	s, _ := h.r11Items(phone360)
	in := cardInput(r11Name(1))
	msg := `document.getElementById(` + in + `.id + "-msg")`
	if got := Eval[string](s, `getComputedStyle(`+msg+`).display`); got != "none" {
		t.Errorf("an empty message takes room: display %q", got)
	}
	fillCard(s, r11Name(1), r11BadQty)
	typed := r11Value(s, 1)
	s.Tab()
	Eval[bool](s, `(window.r11Focus = document.activeElement, true)`)
	s.Until("the blur error", msg+`.classList.contains("field__msg--error")`)
	if !Eval[bool](s, `document.activeElement === window.r11Focus && document.activeElement !== document.body`) {
		t.Errorf("the error moved focus to %s", Eval[string](s, `document.activeElement.outerHTML.slice(0, 120)`))
	}
	if got := Eval[string](s, `getComputedStyle(`+msg+`).display`); got == "none" {
		t.Errorf("the error line is hidden")
	}
	if got := r11Value(s, 1); got != typed {
		t.Errorf("line 1 = %q after the blur error, want %q", got, typed)
	}
	if bad := Eval[[]string](s, r11LayoutJS); len(bad) != 0 {
		t.Errorf("with the error: %q", bad)
	}
	noOverflow(s, "Quantities with an error")
}

// A 422 and a 409 redraw keep every typed value and count them.
func TestE2E_GRNQuantitiesRedrawsKeepValues(t *testing.T) {
	h := Start(t)
	h.r11PO(3)
	s, items := h.r11Items(phone360)
	fillCard(s, r11Name(1), "2,500") // over twice ordered: refused on save
	fillCard(s, r11Name(2), "75")
	s.Keys(kb.Tab)
	fillCard(s, r11Name(3), "0")
	s.Press(saveContinue)
	s.WaitText("Line 1: Accepted 2,500 KGS is more than twice the 1,200 KGS ordered. Check the quantity.")
	for n, want := range map[int]string{1: "2,500", 2: "75", 3: "0"} {
		if got := r11Value(s, n); got != want {
			t.Errorf("after the 422: line %d = %q, want %q", n, got, want)
		}
	}
	if got := Eval[string](s, r11Count); got != "2 of 3 lines entered" {
		t.Errorf("after the 422: footer %q", got)
	}
	fillCard(s, r11Name(1), "900")

	other := s.SecondTab()
	other.Open(items)
	fillCard(other, r11Name(2), "60")
	other.Press(saveContinue)
	onStep(other, "Step 4 of 4")

	s.Front()
	fillCard(s, r11Name(2), "80")
	s.Press(saveContinue)
	s.WaitText("Reload")
	for n, want := range map[int]string{1: "900", 2: "80", 3: "0"} {
		if got := r11Value(s, n); got != want {
			t.Errorf("after the 409: line %d = %q, want %q", n, got, want)
		}
	}
	if got := Eval[string](s, r11Count); got != "2 of 3 lines entered" {
		t.Errorf("after the 409: footer %q", got)
	}
	noOverflow(s, "Quantities after a 409")
}

// H4, UX §5.6, §5.7: on an approved GRN with no bill, Add bill details jumps
// to the Bill details card, under the top bar and on screen, at each phone
// width and on a desktop. Saving there removes the button at once, with no
// empty footer left. An Actions card with nothing else goes with it.
func TestE2E_GRNAddBillDetails(t *testing.T) {
	h := Start(t)
	h.r11PO(5)
	s, _ := h.r11Items(phone360)
	id := s.DocID()
	submitGRN(s, grnApproved)
	page := DocPath(goodsreceipt.DocType, id)
	add := `[...document.querySelectorAll(".doc-actions .actionbar a")].find((a) => a.textContent.trim() === "Add bill details")`
	for _, w := range []int64{320, phone360, phone390, desktop} {
		s.width = w
		s.Open(page)
		if !Eval[bool](s, `(() => { const r = `+add+`?.getBoundingClientRect(); return !!r && r.top >= 0 && r.bottom <= innerHeight + 0.5 && r.height >= 44; })()`) {
			t.Errorf("%d px: Add bill details isn't a 44 px target on the first screen\n%s", w, s.page())
		}
		Eval[bool](s, `(`+add+`.click(), true)`)
		s.Until("the jump", `location.hash === "#bill" && Math.abs(document.getElementById("bill").getBoundingClientRect().top) < innerHeight`)
		top := Eval[float64](s, `document.getElementById("bill").getBoundingClientRect().top`)
		head := Eval[float64](s, `(document.querySelector("header")?.getBoundingClientRect().bottom ?? 0)`)
		if top < head-0.5 || top > float64(r11Height)/2 {
			t.Errorf("%d px: the card's top is at %v under a %v px header", w, top, head)
		}
		noOverflow(s, "the GRN at #bill")
	}
	s.width = phone360
	s.Open(page)
	s.Fill("invoice_no", "INV-889")
	s.Fill("invoice_date", plainDate)
	s.Fill("invoice_amount", "100")
	Eval[bool](s, `(document.querySelector("#bill form button[type=submit]").click(), true)`)
	s.Until("the saved card", `!!document.querySelector("#bill [role=status]")`)
	if !Eval[bool](s, `!document.querySelector(".doc-actions .actionbar") && !!document.querySelector(".doc-actions .more-actions")`) {
		t.Errorf("after the save: the bar stays, or the other actions went\n%s", s.page())
	}
	s.Open(page)
	if s.Shows("Add bill details") {
		t.Errorf("Add bill details shows after a reload with bill details saved")
	}
}

// H4: a viewer with no other action loses the Actions card with the button.
func TestE2E_GRNAddBillLastAction(t *testing.T) {
	h := Start(t)
	h.r11PO(1)
	s, _ := h.r11Items(phone360)
	id := s.DocID()
	submitGRN(s, grnApproved)
	s.Open(DocPath(goodsreceipt.DocType, id))
	Eval[bool](s, `(document.querySelector(".more-actions").remove(), true)`)
	s.Fill("invoice_no", "INV-889")
	s.Fill("invoice_date", plainDate)
	s.Fill("invoice_amount", "100")
	Eval[bool](s, `(document.querySelector("#bill form button[type=submit]").click(), true)`)
	s.Until("the saved card", `!!document.querySelector("#bill [role=status]")`)
	if Eval[bool](s, `!!document.querySelector(".doc-actions")`) {
		t.Errorf("an empty Actions card stays")
	}
}

// r11CardJS lists what is wrong with the line cards on the page: a quantity
// part on two lines, or a part that doesn't start at the card's text edge,
// the Rejected note's edge.
const r11CardJS = `(() => {
  const out = [];
  const cards = [...document.querySelectorAll(".lines-card")];
  if (cards.length === 0) return ["no line cards"];
  for (const c of cards) {
    const meta = c.querySelectorAll(".lines-meta");
    const parts = [...meta[0].querySelectorAll("span")].filter((s) => s.textContent);
    if (parts.length !== 2) { out.push("parts: " + parts.length); continue; }
    const edge = c.querySelector(".lines-top").getBoundingClientRect().left;
    const [a, b] = parts.map((p) => p.getBoundingClientRect());
    if (a.height > 21.5 || b.height > 21.5) out.push("a part wraps: " + a.height + ", " + b.height);
    for (const p of parts) if (!getComputedStyle(p).fontVariantNumeric.includes("tabular-nums")) out.push(p.textContent + " has no tabular figures");
    if (Math.abs(a.left - edge) > 0.5) out.push("Received starts at " + a.left + ", edge " + edge);
    if (b.top >= a.bottom && Math.abs(b.left - edge) > 0.5) out.push("Accepted on its own line starts at " + b.left + ", edge " + edge);
    if (meta[1] && Math.abs(meta[1].getBoundingClientRect().left - edge) > 0.5) out.push("the note starts off the edge");
  }
  return out;
})()`

// M11: the check and document line cards at each phone width read cleanly,
// also with a long quantity, a reject and the worst-case line; both
// quantity parts have tabular figures.
func TestE2E_GRNLineCardsRead(t *testing.T) {
	h := Start(t)
	h.r11PO(2, h.r11Worst())
	s, items := h.r11Items(phone360)
	id := s.DocID()
	fillCard(s, r11Name(1), "2,399.555")
	rejectOn(s, 2, r11Item, "10", grnRejectReason)
	s.Press(saveContinue)
	onStep(s, "Step 4 of 4")
	check := items[:len(items)-len("items")] + "check"
	check1 := func(where string) {
		for _, w := range []int64{320, phone360, phone390} {
			s.width = w
			s.Open(where)
			if bad := Eval[[]string](s, r11CardJS); len(bad) != 0 {
				t.Errorf("%s at %d px: %q", where, w, bad)
			}
			noOverflow(s, where)
		}
	}
	check1(check)
	s.width = phone360
	s.Open(check)
	s.Press("Submit")
	s.WaitText(grnApproved)
	check1(DocPath(goodsreceipt.DocType, id))
}
