//go:build e2e

package e2e

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// rr1aLongPhoto is the longest realistic receipt name, as a phone's gallery
// hands one over.
const rr1aLongPhoto = "WhatsApp Image 2026-10-24 at 14.35.12 (1).jpeg"

// rr1aPhoto writes receiptPhoto's 1 × 1 JPEG under name, with tail after
// its end marker, so two photos with different tails are different files.
func rr1aPhoto(t *testing.T, name, tail string) string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AVN//2Q==")
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(f, append(data, tail...), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// rr1aReceiptFits measures the claim line page with a receipt attached, at
// the top of the page: Type's first row of tiles ends above the sticky
// footer's top and Amount ends within one 300 px swipe below it (expense UX
// §5.5, "Where things sit": Food and Fuel on screen, Amount one scroll
// down); the receipt's name shows; the full view link, Change, and Take
// photo and Choose file once Change is open are each at least 44 px tall;
// closing Change folds them again.
const rr1aReceiptFits = `(() => {
  const out = [];
  scrollTo(0, 0);
  const bar = document.querySelector("#main .actionbar");
  if (!bar) return ["no footer"];
  const top = bar.getBoundingClientRect().top;
  const tile = document.querySelector('.segmented--tiles label');
  const amount = document.getElementById("f-amount");
  if (!tile || !amount) return ["type tile " + !!tile + ", amount " + !!amount];
  const t = tile.getBoundingClientRect(), a = amount.getBoundingClientRect();
  if (t.bottom > top + 0.5) out.push("Type's first row ends at " + t.bottom + ", under the footer at " + top);
  if (a.bottom > top + 300) out.push("Amount ends at " + a.bottom + ", more than one swipe under the footer at " + top);
  const files = document.getElementById("receipt-files");
  if (!files || !files.textContent.includes(NAME)) out.push("the receipt's name isn't shown");
  const shown = (e) => !!e && e.getClientRects().length > 0 && getComputedStyle(e).visibility !== "hidden";
  const tall = (name, e) => {
    if (!shown(e)) out.push(name + " isn't shown");
    else if (e.getBoundingClientRect().height < 44) out.push(name + " is " + e.getBoundingClientRect().height + " px tall");
  };
  tall("the full view link", files && files.querySelector("a[href^='/files/']"));
  const change = files && files.querySelector("details > summary");
  tall("Change", change);
  const photo = document.querySelector("#receipt input[capture]")?.closest("label"), choose = document.querySelector("#receipt-file")?.closest("label");
  if (!photo || !choose) return out.concat(["no Take photo or Choose file"]);
  if (shown(photo) || shown(choose)) out.push("Take photo or Choose file shows while Change is closed");
  if (!change) return out;
  change.click();
  tall("Take photo after Change", photo);
  tall("Choose file after Change", choose);
  change.click();
  if (shown(photo) || shown(choose)) out.push("closing Change leaves Take photo or Choose file");
  return out;
})()`

// TestE2E_ExpenseLinePhotoFits (RR1a 1, expense UX §5.5): at 320 × 568,
// 360 × 640 and 390 × 664 with a receipt photo attached (just uploaded on a
// new line, and on the saved line's page), Type's first row is above the
// sticky footer and Amount one swipe below it, the receipt's name shows,
// and each receipt action stays one tap away and 44 px tall. Changing the
// receipt still uploads, and nothing scrolls sideways.
func TestE2E_ExpenseLinePhotoFits(t *testing.T) {
	h := startExpense(t)
	photo := rr1aPhoto(t, rr1aLongPhoto, "")
	fits := strings.Replace(rr1aReceiptFits, "NAME", "'"+rr1aLongPhoto+"'", 1)
	for _, size := range []struct{ w, h int64 }{{320, 568}, {360, 640}, {390, 664}} {
		d := h.SignIn(deepak, size.w)
		claim := newClaim(d, "", "")
		d.Open(DocPath("expense.Claim", claim) + "/lines/new")
		d.WaitText("Take photo")
		d.Do("the phone's height", chromedp.EmulateViewport(size.w, size.h, chromedp.EmulateMobile, chromedp.EmulateTouch))
		settle(d)
		d.SetFile(claimPhoto, photo)
		d.Until("the receipt's thumbnail", `!!document.querySelector('#receipt-files img')?.complete`)
		settle(d)
		if bad := Eval[[]string](d, fits); len(bad) != 0 {
			t.Errorf("new line at %d × %d: %s", size.w, size.h, strings.Join(bad, "; "))
		}
		noOverflow(d, "the new line with a photo")
		d.Fill("amount", "1,25,00,000")
		pickRadio(d, "expense_type_id", "Food")
		d.Press(saveLine)
		d.Until("back on Expenses", `location.pathname.endsWith("/edit/items")`)

		d.Open(DocPath("expense.Claim", claim) + "/lines/1")
		d.WaitText(saveLine)
		d.Do("the phone's height", chromedp.EmulateViewport(size.w, size.h, chromedp.EmulateMobile, chromedp.EmulateTouch))
		settle(d)
		if bad := Eval[[]string](d, fits); len(bad) != 0 {
			t.Errorf("saved line at %d × %d: %s", size.w, size.h, strings.Join(bad, "; "))
		}
		noOverflow(d, "the saved line with a photo")
		// Change still reaches the upload, and a new photo replaces the shown one.
		d.Do("open Change", chromedp.Evaluate[chromedp.Void](`document.querySelector("#receipt-files details > summary").click()`))
		d.SetFile(claimPhoto, rr1aPhoto(t, "bill-2.jpg", "2"))
		d.Until("the new receipt's name", `document.getElementById("receipt-files").textContent.includes("bill-2.jpg")`)
	}
}

// TestE2E_ExpenseReceiptFocus (RR1a 1, CD-310): a receipt's upload folds
// the upload's buttons behind the new row's Change, so focus goes to that
// Change, never the page body; a refused upload keeps focus on the input
// used, which stays shown. Both with Take photo and Choose file.
func TestE2E_ExpenseReceiptFocus(t *testing.T) {
	h := startExpense(t)
	d := h.SignIn(deepak, phone360)
	claim := newClaim(d, "", "")
	const active = `(() => { const a = document.activeElement; return a.id || a.tagName + (a.closest("#receipt-files details") ? " in Change" : "") + (a.matches("[capture]") ? " camera" : ""); })()`
	for i, input := range []struct{ sel, want string }{{claimPhoto, "INPUT camera"}, {"#receipt-file", "receipt-file"}} {
		d.Open(DocPath("expense.Claim", claim) + "/lines/new")
		d.WaitText("Take photo")
		settle(d)
		d.SetFile(input.sel, fileOf(t, "receipt.gif", gif()))
		d.Until("the type refusal", `document.getElementById("receipt-msg").textContent.includes("Receipt must be")`)
		settle(d)
		if got := Eval[string](d, active); got != input.want {
			t.Errorf("%s refused: focus on %q, want %q", input.sel, got, input.want)
		}
		d.SetFile(input.sel, rr1aPhoto(t, "bill.jpg", strconv.Itoa(i)))
		d.Until("the receipt's row", `!!document.querySelector("#receipt-files .attachments-change")`)
		settle(d)
		if got := Eval[string](d, active); got != "SUMMARY in Change" {
			t.Errorf("%s uploaded: focus on %q, want the row's Change", input.sel, got)
		}
	}
}

// TestE2E_ExpenseReceiptErrorKeepsButtons (RR1a fix 4): with a receipt
// attached, a refused replacement (a GIF chosen under Change) keeps focus
// on the input used, and Take photo and Choose file stay on screen beside
// the message even once Change is closed, so the user can try again without
// hunting for them.
func TestE2E_ExpenseReceiptErrorKeepsButtons(t *testing.T) {
	h := startExpense(t)
	d := h.SignIn(deepak, phone360)
	claim := newClaim(d, "", "")
	d.Open(DocPath("expense.Claim", claim) + "/lines/new")
	d.WaitText("Take photo")
	settle(d)
	d.SetFile(claimPhoto, rr1aPhoto(t, "bill.jpg", ""))
	d.Until("the receipt's row", `!!document.querySelector("#receipt-files .attachments-change")`)
	settle(d)
	d.Do("open Change", chromedp.Evaluate[chromedp.Void](`document.querySelector("#receipt-files details > summary").click()`))
	d.SetFile("#receipt-file", fileOf(t, "receipt.gif", gif()))
	d.Until("the type refusal", `document.getElementById("receipt-msg").textContent.includes("Receipt must be")`)
	settle(d)
	if got := Eval[string](d, `document.activeElement.id`); got != "receipt-file" {
		t.Errorf("refused replacement: focus on %q, want Choose file's input", got)
	}
	d.Do("close Change", chromedp.Evaluate[chromedp.Void](`document.querySelector("#receipt-files details > summary").click()`))
	const buttons = `(() => {
  if (document.querySelector("#receipt-files details[open]")) return ["Change is still open"];
  const out = [];
  for (const [name, i] of [["Take photo", document.querySelector("#receipt input[capture]")], ["Choose file", document.getElementById("receipt-file")]]) {
    const l = i?.closest("label");
    if (!l || l.getClientRects().length === 0) out.push(name + " is hidden beside the refusal");
    else if (l.getBoundingClientRect().height < 44) out.push(name + " is " + l.getBoundingClientRect().height + " px tall");
  }
  return out;
})()`
	if bad := Eval[[]string](d, buttons); len(bad) != 0 {
		t.Errorf("refused replacement, Change closed: %s", strings.Join(bad, "; "))
	}
}
