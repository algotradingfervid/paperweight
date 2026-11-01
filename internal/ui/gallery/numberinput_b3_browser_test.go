//go:build e2e

package gallery

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// caretAt is the field's value with "|" at the caret.
func caretAt(id string) string {
	return `(() => { const t = document.getElementById("` + id + `"); return t.value.slice(0, t.selectionStart) + "|" + t.value.slice(t.selectionEnd); })()`
}

// setField gives the field id focus with value v and the caret at c.
func setField(id, v string, c int) chromedp.Action[chromedp.Void] {
	return chromedp.Evaluate[chromedp.Void](`(() => { const t = document.getElementById("` + id + `"); t.value = ` + jsString(v) + `; t.focus(); t.setSelectionRange(` + strconv.Itoa(c) + `, ` + strconv.Itoa(c) + `); })()`)
}

// paste puts s over the focused field's selection as a paste does: the text,
// the caret after it, then one "insertFromPaste" input event (headless Chrome
// has no clipboard to paste from). It fires no beforeinput, so a filter on
// "insertFromPaste" there would go untested: paste cleaning lives in the input
// handler, which this does reach.
func paste(s string) chromedp.Action[chromedp.Void] {
	return chromedp.Evaluate[chromedp.Void](`(() => { const t = document.activeElement; t.setRangeText(` + jsString(s) + `, t.selectionStart, t.selectionEnd, "end"); t.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertFromPaste" })); })()`)
}

// selectIn gives the field id focus with value v and the characters a to b
// selected.
func selectIn(id, v string, a, b int) chromedp.Action[chromedp.Void] {
	return chromedp.Evaluate[chromedp.Void](`(() => { const t = document.getElementById("` + id + `"); t.value = ` + jsString(v) + `; t.focus(); t.setSelectionRange(` + strconv.Itoa(a) + `, ` + strconv.Itoa(b) + `); })()`)
}

// TestNumberInputBrowser_Grouping (A-16, CD-336 (2)): money, quantity and
// rate inputs group digits Indian style as the user types, keeping the caret
// on the same digit after an insert or a delete in the middle (a delete
// beside a comma removes the digit past it); only digits, one point and,
// where the field is signed, a leading minus can be typed; decimals stop at
// the field's scale; leading zeros go (CD-355 (3)). A paste is cleaned of
// grouping commas and spaces only: anything else NUM refuses stays as pasted
// (CD-355 (2)).
func TestNumberInputBrowser_Grouping(t *testing.T) {
	s := httptest.NewServer(Handler())
	t.Cleanup(s.Close)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	// UI v2 §5.7: in a full-width box "₹" shows inside the input's start,
	// clear of the typed text; the unit joins the input's end with no gap.
	for _, id := range []string{"f-amount", "f-rate"} {
		if got := value[string](t, ctx, `(() => { const t = document.getElementById("`+id+`"), p = t.parentElement.querySelector(".field__pre"), r = p.getBoundingClientRect(), b = t.getBoundingClientRect(); return [p.textContent, r.width > 0, r.left >= b.left, r.right <= b.left + parseFloat(getComputedStyle(t).paddingInlineStart)].join(" "); })()`); got != "₹ true true true" {
			t.Errorf("%s: prefix [text, shown, inside, clear of the text] = %q", id, got)
		}
	}
	if got := value[float64](t, ctx, `(() => { const t = document.getElementById("f-qty"); return t.parentElement.querySelector(".field__suffix").getBoundingClientRect().left - t.getBoundingClientRect().right; })()`); got > 0.5 || got < -1.5 {
		t.Errorf("qty suffix starts %v px from the input's end, want joined", got)
	}

	for _, c := range []struct {
		name, id, start string
		caret           int
		keys            string // typed after the start value; "" when paste is used
		paste           string
		want            string
	}{
		{"A-16 typed from empty", "f-amount", "", 0, "150000.5", "", "1,50,000.5|"},
		{"insert in the middle", "f-amount", "1,50,000", 3, "7", "", "15,7|0,000"},
		{"insert before the first digit", "f-amount", "50,000", 0, "1", "", "1|,50,000"},
		{"backspace in the middle", "f-amount", "15,70,000", 4, kb.Backspace, "", "1,5|0,000"},
		{"backspace after a comma", "f-amount", "1,50,000", 2, kb.Backspace, "", "|50,000"},
		{"delete before a comma", "f-amount", "1,50,000", 4, kb.Delete, "", "15,0|00"},
		{"delete in the middle", "f-amount", "1,50,000", 3, kb.Delete, "", "15|,000"},
		{"paste with commas and spaces", "f-amount", "", 0, "", " 1,50, 000.50 ", "1,50,000.50|"},
		{"paste in the middle", "f-qty", "1,000", 1, "", "2 3", "1,23|,000"},
		{"paste with leading zeros", "f-amount", "", 0, "", "0150", "150|"},
		{"paste with a comma after the point kept", "f-amount", "", 0, "", "1.234,56", "1.234,56|"},
		{"paste with a letter kept", "f-amount", "", 0, "", "1e5", "1e5|"},
		{"paste in parentheses kept", "f-amount", "", 0, "", "(1,500)", "(1,500)|"},
		{"paste with a second point kept", "f-amount", "", 0, "", "1.2.3", "1.2.3|"},
		{"paste past the scale kept", "f-amount", "", 0, "", "1,500.505", "1,500.505|"},
		{"paste with a rupee sign kept", "f-amount", "", 0, "", "₹1,500", "₹1,500|"},
		{"paste with a minus on an unsigned field kept", "f-amount", "", 0, "", "-1,500", "-1,500|"},
		{"typing into a kept paste leaves it alone", "f-amount", "1e5", 3, "0", "", "1e50|"},
		{"leading zeros typed", "f-amount", "", 0, "0150", "", "150|"},
		{"a zero kept before the point", "f-amount", "", 0, "0.5", "", "0.5|"},
		{"zeros before the point become one", "f-amount", "", 0, "000.5", "", "0.5|"},
		{"a zero typed before the first digit is dropped", "f-amount", "150", 0, "0", "", "|150"},
		{"a zero typed before a grouped value is dropped", "f-amount", "1,50,000", 0, "0", "", "|1,50,000"},
		{"a zero typed before the point is kept", "f-amount", ".5", 0, "0", "", "0|.5"},
		{"money scale 2", "f-amount", "", 0, "1234.567", "", "1,234.56|"},
		{"quantity scale 3", "f-qty", "", 0, "1234.5678", "", "1,234.567|"},
		{"rate scale 4", "f-rate", "", 0, "62.123456", "", "62.1234|"},
		{"percent scale 3", "f-discount", "", 0, "12.34567", "", "12.345|"},
		{"a typed decimal past the scale in the middle", "f-amount", "1.25", 3, "9", "", "1.2|5"},
		{"one point only", "f-amount", "", 0, "1.2.3", "", "1.23|"},
		{"letters dropped", "f-qty", "", 0, "12a3e", "", "123|"},
		{"no minus on an unsigned field", "f-amount", "", 0, "-500", "", "500|"},
		{"no minus on quantity", "f-qty", "", 0, "-5", "", "5|"},
	} {
		steps := []chromedp.Action[chromedp.Void]{setField(c.id, c.start, c.caret)}
		if c.paste != "" {
			steps = append(steps, paste(c.paste))
		} else {
			steps = append(steps, chromedp.KeyEvent(c.keys))
		}
		act(t, ctx, c.name, steps...)
		if got := value[string](t, ctx, caretAt(c.id)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}

	// A whole-unit quantity (scale 0) takes no point; a signed field keeps a
	// leading minus only. The gallery has neither, so the test marks f-qty
	// and f-amount the way QtyInput(whole) and Signed render them.
	act(t, ctx, "mark whole and signed", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-qty").dataset.scale = "0"; document.getElementById("f-amount").dataset.signed = ""`))
	for _, c := range []struct {
		name, id, start string
		caret           int
		keys, want      string
	}{
		{"whole quantity", "f-qty", "", 0, "12.5", "125|"},
		{"signed typed", "f-amount", "", 0, "-150000", "-1,50,000|"},
		{"signed minus in the middle dropped", "f-amount", "", 0, "15-00", "1,500|"},
		{"signed minus at the start of a value", "f-amount", "1,500", 0, "-", "-|1,500"},
		{"signed zero typed after the minus is dropped", "f-amount", "-150", 1, "0", "-|150"},
	} {
		act(t, ctx, c.name, setField(c.id, c.start, c.caret), chromedp.KeyEvent(c.keys))
		if got := value[string](t, ctx, caretAt(c.id)); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
	act(t, ctx, "signed paste", setField("f-amount", "", 0), paste("-1,50 0"))
	if got := value[string](t, ctx, caretAt("f-amount")); got != "-1,500|" {
		t.Errorf("signed paste: %q, want %q", got, "-1,500|")
	}

	// Backspace over a selection that starts after a comma removes the
	// selection, not the digit before the comma.
	act(t, ctx, "select 50 and Backspace", selectIn("f-amount", "1,50,000", 2, 4), chromedp.KeyEvent(kb.Backspace))
	if got := value[string](t, ctx, caretAt("f-amount")); got != "1|,000" {
		t.Errorf("Backspace over a selection: %q, want %q", got, "1|,000")
	}
}

// TestNumberInputBrowser_WritebackKeepsTyping (CD-336 (2), CD-48): the
// /ui/validate writeback replaces the value when the user hasn't typed since
// the request, even with the focus still in the box; when they have typed
// since, a late reply never overwrites the newer value, also when two
// requests queue (CD-355 (1)).
func TestNumberInputBrowser_WritebackKeepsTyping(t *testing.T) {
	hold := make(chan struct{})
	h := Handler()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ui/validate/money" {
			select {
			case <-hold:
			case <-time.After(10 * time.Second):
			}
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(s.Close)
	release := func() {
		t.Helper()
		select {
		case hold <- struct{}{}:
		case <-time.After(5 * time.Second):
			t.Fatal("no /ui/validate/money request waiting")
		}
	}
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/fields"); err != nil {
		t.Fatal(err)
	}
	const change = `(() => { const m = document.getElementById("f-amount-msg"); m.dataset.old = "1"; document.getElementById("f-amount").dispatchEvent(new Event("change", { bubbles: true })); })()`
	const swapped = `!document.getElementById("f-amount-msg").dataset.old`

	act(t, ctx, "type and change", setField("f-amount", "", 0), chromedp.KeyEvent("1500"), chromedp.Evaluate[chromedp.Void](change))
	release()
	until(t, ctx, "the reply", swapped)
	if got := value[string](t, ctx, `document.activeElement.id + " " + document.getElementById("f-amount").value`); got != "f-amount 1,500.00" {
		t.Errorf("untyped writeback: %q, want the focus kept and the server's 1,500.00", got)
	}

	act(t, ctx, "type, change, type again", setField("f-amount", "", 0), chromedp.KeyEvent("1500"), chromedp.Evaluate[chromedp.Void](change), chromedp.KeyEvent("7"))
	release()
	until(t, ctx, "the late reply", swapped)
	if got := value[string](t, ctx, caretAt("f-amount")); got != "15,007|" {
		t.Errorf("after a late reply: %q, want the newer typing kept as 15,007|", got)
	}

	// CD-355 (1): two changes queue two requests. Reply 1 carries 1500, which
	// the box no longer holds, so it leaves the typing alone; reply 2 carries
	// the box's value and writes the server's format back.
	act(t, ctx, "type, change, type, change", setField("f-amount", "", 0), chromedp.KeyEvent("1500"), chromedp.Evaluate[chromedp.Void](change), chromedp.KeyEvent("7"), chromedp.Evaluate[chromedp.Void](change))
	release()
	until(t, ctx, "reply 1", swapped)
	if got := value[string](t, ctx, caretAt("f-amount")); got != "15,007|" {
		t.Errorf("after reply 1 of 2: %q, want the newer typing kept as 15,007|", got)
	}
	act(t, ctx, "mark the message", chromedp.Evaluate[chromedp.Void](`document.getElementById("f-amount-msg").dataset.old = "1"`))
	release()
	until(t, ctx, "reply 2", swapped)
	if got := value[string](t, ctx, `document.getElementById("f-amount").value`); got != "15,007.00" {
		t.Errorf("after reply 2 of 2: %q, want the server's 15,007.00", got)
	}
}
