//go:build e2e

package e2e

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
)

// RR1b finding 7 (H4, UX §5.6, §5.7): on an approved GRN with no bill, at
// each phone width and on a desktop, Add bill details jumps to the bill form
// with focus on Vendor invoice no., its label on screen under the header,
// and the bar no longer offers Add bill details (the user is at the form),
// also after a blank Save redraws the form; a bar with nothing else goes
// too.
func TestE2E_GRNAddBillFocusesInvoiceNo(t *testing.T) {
	h := Start(t)
	h.r11PO(1)
	s, _ := h.r11Items(phone360)
	id := s.DocID()
	submitGRN(s, grnApproved)
	page := DocPath(goodsreceipt.DocType, id)
	add := `[...document.querySelectorAll(".doc-actions .actionbar a")].find((a) => a.textContent.trim() === "Add bill details")`
	for _, w := range []int64{320, phone360, phone390, desktop} {
		at := strconv.FormatInt(w, 10) + " px"
		s.width = w
		s.Open(page)
		Eval[bool](s, `(`+add+`.click(), true)`)
		s.Until(at+": focus on Vendor invoice no.", `document.activeElement?.id === "f-invoice_no"`)
		if m := Eval[string](s, `(() => {
  const l = document.querySelector('label[for="f-invoice_no"]').getBoundingClientRect();
  const i = document.getElementById("f-invoice_no").getBoundingClientRect();
  const head = document.querySelector("header")?.getBoundingClientRect().bottom ?? 0;
  if (l.top < head - 0.5) return "the label's top " + Math.round(l.top) + " is under the " + Math.round(head) + " px header";
  if (i.bottom > innerHeight / 2) return "the input ends at " + Math.round(i.bottom) + ", below the screen's top half";
  return "";
})()`); m != "" {
			t.Errorf("%s: %s", at, m)
		}
		if Eval[bool](s, `(() => { const a = `+add+`; return !!a && a.checkVisibility(); })()`) {
			t.Errorf("%s: the bar still offers Add bill details at the form", at)
		}
		noOverflow(s, "the GRN at the bill form")

		// A blank Save redraws the form (htmx 422), which ends the jump's
		// :target; the bar still doesn't offer Add bill details (RR1b fix 2).
		Eval[bool](s, `(document.querySelector("#bill form button[type=submit]").click(), true)`)
		s.Until(at+": the blank save's error summary", `!!document.querySelector("#bill .error-summary") && document.querySelector(".htmx-request") === null`)
		if Eval[bool](s, `(() => { const a = `+add+`; return !!a && a.checkVisibility(); })()`) {
			t.Errorf("%s: the bar offers Add bill details again after a blank save", at)
		}
	}

	// A bar holding only Add bill details goes with it, leaving no empty
	// footer (a viewer with no other action).
	s.width = phone360
	s.Open(page)
	Eval[bool](s, `(() => {
  const bar = document.querySelector(".doc-actions .actionbar");
  [...bar.children].filter((c) => c !== `+add+`).forEach((c) => c.remove());
  `+add+`.click();
  return true;
})()`)
	s.Until("focus on Vendor invoice no.", `document.activeElement?.id === "f-invoice_no"`)
	if Eval[bool](s, `document.querySelector(".doc-actions .actionbar").checkVisibility()`) {
		t.Error("an empty bar stays after the jump")
	}
}
