//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// R1 (CD-387 (5), UI v2 §3.3, A-28): the failure banner reached the user on
// every long form only at the top of the page, and a line page or master form
// never reached it at all.

const (
	offlineTitle = "Couldn't reach the server."
	offlineText  = "Couldn't reach the server. Check your connection. Nothing you typed is lost."
	unsureText   = "Your change may have been saved. Reload to check before trying again."
	networkText  = `(document.querySelector("#network .banner")?.textContent.replace(/\s+/g, " ").trim() ?? "")`
)

var phones = []int64{phone320, phone360, phone390}

// abortRequests makes every request from s fail the way a dropped connection
// does, before it reaches the server (a route abort; navigator.onLine stays
// true), until the returned stop is called.
func abortRequests(s *Session) (stop func()) {
	s.t.Helper()
	block := func(patterns ...*network.BlockPattern) {
		s.Do("block requests", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
			if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
				return err
			}
			_, err := chromedp.Call(ctx, network.SetBlockedURLs, network.SetBlockedURLsParams{URLPatterns: patterns})
			return err
		}))
	}
	block(&network.BlockPattern{URLPattern: "*://*:*/*", Block: true})
	return func() { block() }
}

// reportOffline turns the network off or on and has the browser report it:
// offline's rule alone leaves navigator.onLine true.
func reportOffline(s *Session, off bool) {
	s.t.Helper()
	offline(s, off)
	s.Do("report offline "+strconv.FormatBool(off), chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, network.OverrideNetworkState, network.OverrideNetworkStateParams{Offline: off, DownloadThroughput: -1, UploadThroughput: -1})
		return err
	}))
	s.Until("navigator.onLine", `navigator.onLine === `+strconv.FormatBool(!off))
}

// scrollDown makes the page long and goes to its end, so the form is far below
// the top of the document.
func scrollDown(s *Session) {
	s.t.Helper()
	Eval[bool](s, `(() => {
  const d = document.createElement("div");
  d.style.height = "1500px";
  document.getElementById("main").prepend(d);
  window.scrollTo({top: document.documentElement.scrollHeight, behavior: "instant"});
  document.addEventListener("htmx:beforeSend", () => { window.networkFrom = window.scrollY; });
  return true;
})()`)
}

// bannerInView fails unless the banner says want, sits whole in the viewport
// below the sticky head and above the footer buttons on a scrolled page, and
// holds focus.
func bannerInView(s *Session, want string) {
	s.t.Helper()
	s.Until("the banner", networkText+`.includes(`+strconv.Quote(want)+`)`)
	at := Eval[float64](s, `window.networkFrom`)
	if at < 500 {
		s.t.Errorf("at %d px the page was at scrollY %v, want it scrolled", s.width, at)
	}
	noOverflow(s, "the banner")
	if !Eval[bool](s, `document.activeElement === document.querySelector("#network .banner")`) {
		s.t.Errorf("at %d px focus is on %s, want the banner", s.width, Eval[string](s, `document.activeElement.outerHTML.slice(0, 80)`))
	}
	// The banner adds its own height to the page above the form, nothing more.
	if y := Eval[float64](s, `window.scrollY`); y < at-1 || y > at+Eval[float64](s, `document.getElementById("network").getBoundingClientRect().height`)+1 {
		s.t.Errorf("at %d px focus moved the page from scrollY %v to %v", s.width, at, y)
	}
	got := Eval[[]float64](s, `(() => {
  const r = document.querySelector("#network .banner").getBoundingClientRect();
  const shown = (e) => e?.checkVisibility() ? e.getBoundingClientRect() : null;
  const head = [document.querySelector(".app-header"), document.querySelector(".wizard-head")].map(shown).filter((h) => h && h.top <= 0 && h.bottom > 0);
  const bar = shown(document.querySelector(".actionbar"));
  return [r.top, r.bottom, Math.max(0, ...head.map((h) => h.bottom)), bar ? bar.top : innerHeight, innerHeight];
})()`)
	top, bottom, head, bar, height := got[0], got[1], got[2], got[3], got[4]
	if top < head-0.5 || bottom > min(bar, height)+0.5 {
		s.t.Errorf("at %d px the banner spans %v..%v, want it between the head at %v and the footer at %v (viewport %v)", s.width, top, bottom, head, bar, height)
	}
}

// valueOf is the field's value on the page.
func valueOf(s *Session, field string) string {
	s.t.Helper()
	return Eval[string](s, `document.querySelector("[name=`+field+`]").value`)
}

// TestE2E_NetworkBanner_InViewOnEveryLongForm: a save whose connection drops on
// a scrolled long page, at each phone width, shows the banner in view with focus
// on it, keeps what was typed, and Try again saves it. Line pages and master
// forms post through htmx, so they get here rather than to the browser's own
// error page.
func TestE2E_NetworkBanner_InViewOnEveryLongForm(t *testing.T) {
	for _, width := range phones {
		t.Run(strconv.FormatInt(width, 10), func(t *testing.T) {
			h := startPurchase(t) // Suresh's step 1 chooses its one PO
			id := h.metroDraft(priya)
			base := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10)
			p := h.SignIn(priya, width)

			// A document wizard step.
			p.Open(base + "/edit/terms")
			p.Fill("remarks", "Deliver by noon")
			scrollDown(p)
			stop := abortRequests(p)
			p.Press(saveContinue)
			bannerInView(p, offlineText)
			if got := valueOf(p, "remarks"); got != "Deliver by noon" {
				t.Errorf("remarks after the drop = %q", got)
			}
			stop()
			p.Press("Try again")
			p.Until("the next step", `location.pathname.endsWith("/edit/items")`)
			if got := Scalar[string](h, `SELECT remarks FROM purchase_order WHERE id = ?`, id); got != "Deliver by noon" {
				t.Errorf("stored remarks = %q", got)
			}

			// A line page: the blocker's screen.
			p.Open(base + "/lines/1")
			p.Fill("qty", "25")
			scrollDown(p)
			stop = abortRequests(p)
			p.Press(saveLine)
			bannerInView(p, offlineText)
			if got := Eval[string](p, `location.pathname`); got != base+"/lines/1" {
				t.Errorf("the page left for %q", got)
			}
			if got := valueOf(p, "qty"); got != "25" {
				t.Errorf("qty after the drop = %q", got)
			}
			stop()
			p.Press("Try again")
			p.Until("back on Items", `location.pathname.endsWith("/edit/items")`)
			if got := Scalar[int](h, `SELECT qty FROM purchase_order_line WHERE parent_id = ?`, id); got != 25000 {
				t.Errorf("stored qty = %d, want 25000 (25)", got)
			}

			// A master form, at its version.
			a := h.SignIn(admin, width)
			uom := h.idOf("uom", "KGS")
			a.Open("/m/uom/" + uom)
			a.Fill("name", "Kilograms "+strconv.FormatInt(width, 10))
			scrollDown(a)
			stop = abortRequests(a)
			a.Press("Save")
			bannerInView(a, offlineText)
			if got := valueOf(a, "name"); got != "Kilograms "+strconv.FormatInt(width, 10) {
				t.Errorf("name after the drop = %q", got)
			}
			stop()
			a.Press("Try again")
			a.WaitText("UoM saved.")
			if got := Scalar[string](h, `SELECT name FROM uom WHERE id = ?`, uom); got != "Kilograms "+strconv.FormatInt(width, 10) {
				t.Errorf("stored name = %q", got)
			}

			// GRN Quantities, where the reviewer scrolled to line 5.
			po := h.Do(priya, purchase.DocType, id, doctype.ActionSubmit, doc.ActionInput{Version: Scalar[int](h, `SELECT version FROM purchase_order WHERE id = ?`, id)})
			s := h.SignIn(suresh, width)
			s.Open("/doc/" + goodsreceipt.DocType)
			startGRN(s)
			grnDelivery(s, "CH-"+strconv.FormatInt(width, 10)+strconv.FormatInt(po.ID, 10))
			name := "Received now, line 1, Binding Wire 18G"
			fillCard(s, name, "5")
			scrollDown(s)
			stop = abortRequests(s)
			s.Press(saveContinue)
			bannerInView(s, offlineText)
			if got := Eval[string](s, cardInput(name)+`.value`); got != "5" {
				t.Errorf("Received now after the drop = %q", got)
			}
			stop()
			s.Press("Try again")
			onStep(s, "Step 4 of 4")
		})
	}
}

// TestE2E_NetworkBanner_SessionStepIsNotUnsure: a master wizard step writes only
// the session, so a drop gets Try again, which moves on with the typed values.
func TestE2E_NetworkBanner_SessionStepIsNotUnsure(t *testing.T) {
	h := Start(t)
	a := h.SignIn(admin, phone390)
	a.Open("/m/party/new")
	a.Fill("code", "LAKSHMI")
	a.Fill("name", "Lakshmi Traders")
	pickRadio(a, "role", "Vendor")
	stop := abortRequests(a)
	a.Press("Save and continue")
	a.Until("the banner", networkText+`.includes(`+strconv.Quote(offlineText)+`)`)
	if got := valueOf(a, "name"); got != "Lakshmi Traders" {
		t.Errorf("name after the drop = %q", got)
	}
	stop()
	a.Press("Try again")
	a.Until("the next step", `document.getElementById("network").children.length === 0 && location.pathname !== "/m/party/new"`)
	a.Press("Back")
	a.Until("the first step", `document.querySelector("[name=name]")?.value === "Lakshmi Traders"`)
}

// TestE2E_NetworkBanner_CreateOnlineIsUnsureOfflineRetries: CD-387 (5) on a
// master create. Online, a failed answer can't be told from a saved record, so
// it gets Reload and never a re-send. While the browser reports offline nothing
// was sent: Try again, and exactly one record.
func TestE2E_NetworkBanner_CreateOnlineIsUnsureOfflineRetries(t *testing.T) {
	h := Start(t)
	const count = `SELECT count(*) FROM uom WHERE code = 'CRTN'`
	a := h.SignIn(admin, phone390)
	a.Open("/m/uom/new")
	a.Fill("code", "CRTN")
	a.Fill("name", "Carton")
	stop := abortRequests(a)
	a.Press("Save")
	a.Until("the unsure banner", networkText+`.includes(`+strconv.Quote(unsureText)+`)`)
	if n := Eval[int](a, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
		t.Errorf("an online failed create offers %d Try again buttons", n)
	}
	stop()

	// The warning stays until its form is answered: a fresh page goes on.
	a.Open("/m/uom/new")
	a.Fill("code", "CRTN")
	a.Fill("name", "Carton")
	reportOffline(a, true)
	a.Press("Save")
	a.Until("the offline banner", networkText+`.includes(`+strconv.Quote(offlineText)+`)`)
	reportOffline(a, false)
	a.Press("Try again")
	a.WaitText("UoM saved.")
	if got := Scalar[int](h, count); got != 1 {
		t.Errorf("%d CRTN rows, want 1", got)
	}
}

// TestE2E_NetworkBanner_FirstSaveOffline: CD-387 (5) on a new document's first
// save while the browser reports offline; the online case is
// TestE2E_NetworkBanner_LostCreateAnswerOffersReload.
func TestE2E_NetworkBanner_FirstSaveOffline(t *testing.T) {
	h := Start(t)
	const count = `SELECT count(*) FROM fixture_order`
	before := Scalar[int](h, count)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	reportOffline(p, true)
	p.Press(saveContinue)
	p.Until("the offline banner", networkText+`.includes(`+strconv.Quote(offlineText)+`)`)
	reportOffline(p, false)
	p.Press("Try again")
	p.Until("the draft saved", `location.pathname.includes("/edit/")`)
	if got := Scalar[int](h, count) - before; got != 1 {
		t.Errorf("%d new drafts, want 1", got)
	}
}

// TestE2E_ReceiptDroppedKeepsTyping: a receipt upload's POST carries its version
// in the query. Dropped, it says the file text with no Reload, and the typed
// fields stay (EXP review).
func TestE2E_ReceiptDroppedKeepsTyping(t *testing.T) {
	h := startExpense(t)
	d := h.SignIn(deepak, phone360)
	d.Open("/doc/" + expense.ClaimDocType)
	d.Press("New expense claim")
	onStep(d, "Step 1 of 3")
	d.Fill("purpose", "Pune Metro site visit")
	d.Press(saveContinue)
	onStep(d, "Step 2 of 3")
	d.Press("Add an expense")
	d.WaitText("Take photo")
	settle(d)
	d.Fill("amount", "1,250")
	stop := abortRequests(d)
	d.SetFile(claimPhoto, receiptPhoto(t))
	d.Until("the file text", `document.getElementById("receipt-msg").textContent.includes(`+strconv.Quote(msgDropped)+`)`)
	if n := Eval[int](d, `document.getElementById("network").children.length`); n != 0 {
		t.Errorf("the upload's drop shows %d banners in #network: %s", n, Eval[string](d, networkText))
	}
	if got := valueOf(d, "amount"); got != "1,250" {
		t.Errorf("amount after the drop = %q", got)
	}
	stop()
	d.SetFile(claimPhoto, receiptPhoto(t))
	d.Until("the receipt's thumbnail", `!!document.querySelector("#receipt-files img")`)
	d.Until("the file text gone", `document.getElementById("receipt-msg").textContent === ""`)
	if got := valueOf(d, "amount"); got != "1,250" {
		t.Errorf("amount after the retry = %q", got)
	}

	// The expense line page's Save line sits in the sticky bar, outside its form.
	pickRadio(d, "expense_type_id", "Food")
	stop = abortRequests(d)
	d.Press(saveLine)
	d.Until("the banner", networkText+`.includes(`+strconv.Quote(offlineText)+`)`)
	if got := valueOf(d, "amount"); got != "1,250.00" { // formatted on leaving the field
		t.Errorf("amount after the dropped save = %q", got)
	}
	stop()
	d.Press("Try again")
	d.Until("back on Expenses", `location.pathname.endsWith("/edit/expenses") || location.pathname.endsWith("/edit/items")`)
}

// TestE2E_ExpiredLineSaveNamesTheReason: a line page's htmx save after the
// session ended reaches sign-in with the reason, and returns to the line page.
func TestE2E_ExpiredLineSaveNamesTheReason(t *testing.T) {
	h := startPurchase(t)
	id := h.metroDraft(priya)
	line := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) + "/lines/1"
	p := h.SignIn(priya, phone390)
	p.Open(line)
	p.Fill("qty", "25")
	if _, err := h.App.DB().SQL().ExecContext(context.Background(), `DELETE FROM sessions`); err != nil {
		t.Fatal(err)
	}
	p.Press(saveLine)
	p.Until("sign-in", `location.pathname === "/signin"`)
	p.WaitText("Your session has expired. Sign in again.")
	if got := Eval[string](p, `document.querySelector("[name=next]")?.value ?? ""`); got != line {
		t.Errorf("next = %q, want %q", got, line)
	}
}

// inDialogBanner fails unless the open dialog holds the banner, focused and
// whole in the viewport, on top of everything.
func inDialogBanner(s *Session, want string) {
	s.t.Helper()
	const banner = `document.querySelector("dialog[open] .banner:has([data-retry], [data-reload])")`
	s.Until("the banner in the dialog", `(`+banner+`?.textContent ?? "").replace(/\s+/g, " ").includes(`+strconv.Quote(want)+`)`)
	if !Eval[bool](s, `document.activeElement === `+banner) {
		s.t.Errorf("focus is on %s, want the banner", Eval[string](s, `document.activeElement.outerHTML.slice(0, 80)`))
	}
	if !Eval[bool](s, `(() => {
  const b = `+banner+`.querySelector("button");
  const r = b.getBoundingClientRect();
  return r.top >= 0 && r.bottom <= innerHeight && b.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2));
})()`) {
		s.t.Error("Try again can't be reached in the dialog")
	}
}

// TestE2E_NetworkBanner_LostAnswerThenOffline: a first save went out online and
// its answer was lost, and the browser went offline right after it left. The
// server has it: Reload, and one draft.
func TestE2E_NetworkBanner_LostAnswerThenOffline(t *testing.T) {
	h := Start(t)
	const count = `SELECT count(*) FROM fixture_order`
	before := Scalar[int](h, count)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	Eval[bool](p, `(document.addEventListener("htmx:beforeSend", (e) => { if (e.detail.requestConfig.path.endsWith("/new")) Object.defineProperty(navigator, "onLine", {get: () => false, configurable: true}); }), true)`)
	h.DropAnswers("/doc/" + fixture.Order + "/new")
	p.Press(saveContinue)
	p.Until("the unsure banner", networkText+`.includes(`+strconv.Quote(unsureText)+`)`)
	if n := Eval[int](p, `document.querySelectorAll("#network [data-retry]").length`); n != 0 {
		t.Errorf("the banner offers %d Try again buttons", n)
	}
	h.DropAnswers("")
	if got := Scalar[int](h, count) - before; got != 1 {
		t.Errorf("%d new drafts, want 1", got)
	}
}

// TestE2E_NetworkBanner_FieldCheckKeepsTheUnsureBanner: after a lost first save,
// leaving a field sends its check. Passing or failing, that says nothing about
// the save, so the warning stays.
func TestE2E_NetworkBanner_FieldCheckKeepsTheUnsureBanner(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	h.DropAnswers("/doc/" + fixture.Order + "/new")
	p.Press(saveContinue)
	p.Until("the unsure banner", networkText+`.includes(`+strconv.Quote(unsureText)+`)`)
	h.DropAnswers("")
	for _, typed := range []string{"95,000.00", "not a number"} {
		Eval[bool](p, `(window.checked = 0, document.addEventListener("htmx:afterRequest", (e) => { if (e.detail.requestConfig.path.startsWith("/ui/validate/")) window.checked++; }), true)`)
		p.Fill("amount", typed)
		p.Tab()
		p.Until("the field's check answered", `window.checked > 0`)
		settle(p)
		if got := Eval[string](p, networkText); !strings.Contains(got, unsureText) {
			t.Errorf("after tabbing out of Amount with %q the banner reads %q", typed, got)
		}
	}
}

// TestE2E_NetworkBanner_InsideDialogs: #network sits behind a modal, so a failure
// from one shows inside it. The two dialogs that post: quick-create and Remove
// file. A closed dialog hands the banner back to the page.
func TestE2E_NetworkBanner_InsideDialogs(t *testing.T) {
	const list = "f-vendor_id-list"
	h := startPurchase(t)
	p := h.SignIn(admin, phone360)
	sheet := func() {
		t.Helper()
		p.Open("/modules/purchase")
		p.Press("New purchase order")
		onStep(p, "Step 1 of 4")
		input := chromedp.CSS(`input[aria-controls="` + list + `"]`)
		p.Do("search the vendor", chromedp.Click(input), chromedp.SendKeys(input, "Sai Krupa"))
		p.Until("the New option", `document.querySelector("#`+list+` [data-quick]")?.textContent.replace(/\s+/g, " ").trim() === 'New vendor "Sai Krupa"'`)
		p.WaitText(`No vendor matches "Sai Krupa"`)
		clickOn(p, "#"+list+" [data-quick]")
		p.Until("the sheet", `document.getElementById("`+list+`-sheet")?.open === true`)
		p.Fill("quick_line1", "Survey 42, Hadapsar")
		p.Fill("quick_city", "Pune")
		p.Do("choose the state", chromedp.Evaluate[chromedp.Void](`(() => { const s = document.getElementById("f-quick_state_code"); s.value = "27"; s.dispatchEvent(new Event("change", {bubbles: true})); })()`))
	}
	sheet()

	stop := abortRequests(p)
	pressInDialog(p, "Add and choose")
	inDialogBanner(p, unsureText)
	stop()
	sheet() // the warning stays until its form is answered: a fresh page goes on
	reportOffline(p, true)
	pressInDialog(p, "Add and choose")
	inDialogBanner(p, offlineText)
	reportOffline(p, false)
	pressInDialog(p, "Try again")
	p.Until("the vendor chosen", `!document.querySelector("dialog[open]") && document.querySelector("#`+list+`").closest(".picker").querySelector("[data-picker-summary]").textContent.trim() === "SAIKRUPA Sai Krupa"`)
	if got := Scalar[int](h, `SELECT count(*) FROM party WHERE code = 'SAIKRUPA'`); got != 1 {
		t.Errorf("%d SAIKRUPA parties, want 1", got)
	}

	c := Start(t)
	id := claimDraft(c).ID
	d := c.SignIn(deepak, phone360)
	d.Open(checkStep(id))
	d.SetFile(takePhoto, fileOf(t, "receipt.jpg", shadedPhoto(t, 8, 8, 1)))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	press := func() {
		t.Helper()
		d.Do("open the confirm", chromedp.Evaluate[chromedp.Void](`document.querySelector(`+strconv.Quote(removeOf("receipt.jpg"))+`).click()`))
		d.Until("the confirm", `!!document.querySelector("dialog[open]")`)
	}
	press()
	stop = abortRequests(d)
	pressInDialog(d, "Remove file")
	inDialogBanner(d, unsureText)

	// Closing the dialog keeps the warning, on the page.
	pressInDialog(d, "Keep file")
	d.Until("the banner back on the page", networkText+`.includes(`+strconv.Quote(unsureText)+`)`)
	stop()
	d.Open(checkStep(id))
	d.Until("the receipt's thumbnail", thumbShown("receipt.jpg"))
	press()
	reportOffline(d, true)
	pressInDialog(d, "Remove file")
	inDialogBanner(d, offlineText)
	reportOffline(d, false)
	pressInDialog(d, "Try again")
	d.Until("the file gone", `!document.querySelector("dialog[open]") && !(`+thumbShown("receipt.jpg")+`)`)
}

// TestE2E_MasterRefusalInView: htmx keeps the scroll over a swap, so a refusal
// must bring itself into view: the banner of a stale save on a master form and
// a sub-form, and a field error's summary, under the sticky head at each phone
// width.
func TestE2E_MasterRefusalInView(t *testing.T) {
	h := Start(t)
	inView := func(s *Session, what, target string) {
		t.Helper()
		s.Until(what, `!!document.querySelector(`+strconv.Quote(target)+`)`)
		settle(s)
		if !Eval[bool](s, `document.activeElement.closest(`+strconv.Quote(target)+`) !== null`) {
			t.Errorf("%s at %d px: focus is on %s", what, s.width, Eval[string](s, `document.activeElement.outerHTML.slice(0, 80)`))
		}
		got := Eval[[]float64](s, `(() => {
  const r = document.querySelector(`+strconv.Quote(target)+`).getBoundingClientRect();
  const head = [document.querySelector(".app-header"), document.querySelector(".wizard-head")].filter((e) => e?.checkVisibility()).map((e) => e.getBoundingClientRect()).filter((b) => b.top <= 0 && b.bottom > 0);
  return [r.top, r.bottom, Math.max(0, ...head.map((b) => b.bottom)), innerHeight];
})()`)
		if got[0] < got[2]-0.5 || got[0] >= got[3] {
			t.Errorf("%s at %d px: top %v, want below the head at %v and in a viewport of %v", what, s.width, got[0], got[2], got[3])
		}
	}
	stale := func(table, id string) {
		t.Helper()
		if _, err := h.App.DB().SQL().ExecContext(context.Background(), `UPDATE `+table+` SET version = version + 1 WHERE id = ?`, id); err != nil {
			t.Fatal(err)
		}
	}
	party := strconv.FormatInt(h.Fx.Parties[0].ID, 10)
	address := strconv.FormatInt(Scalar[int64](h, `SELECT id FROM address WHERE party_id = ? LIMIT 1`, party), 10)
	for _, width := range phones {
		a := h.SignIn(admin, width)
		uom := h.idOf("uom", "KGS")

		a.Open("/m/uom/" + uom)
		a.Fill("code", "BAD CODE!")
		scrollDown(a)
		a.Press("Save")
		inView(a, "a field error", ".error-summary")

		a.Open("/m/uom/" + uom)
		a.Fill("name", "Kilograms")
		scrollDown(a)
		stale("uom", uom)
		a.Press("Save")
		inView(a, "a stale master form", "#record-alert")

		a.Open("/m/party/" + party + "/addresses/" + address)
		a.Fill("city", "Nashik")
		scrollDown(a)
		stale("address", address)
		a.Press("Save")
		inView(a, "a stale sub-form", "#record-alert")
	}
}

// TestE2E_NetworkBanner_RefusalKeepsTheUnsureBanner: after a lost first save,
// the same form refused (422) is no proof either way: the warning stays, and a
// background search failing doesn't replace it. One draft.
func TestE2E_NetworkBanner_RefusalKeepsTheUnsureBanner(t *testing.T) {
	h := Start(t)
	const count = `SELECT count(*) FROM fixture_order`
	before := Scalar[int](h, count)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	h.DropAnswers("/doc/" + fixture.Order + "/new")
	p.Press(saveContinue)
	p.Until("the unsure banner", networkText+`.includes(`+strconv.Quote(unsureText)+`)`)
	h.DropAnswers("")
	Eval[bool](p, `(document.getElementById("f-amount").value = "not a number", true)`)
	p.Press(saveContinue)
	p.Until("the refusal", `!!document.querySelector(".error-summary")`)
	settle(p)
	if got := Eval[string](p, networkText); !strings.Contains(got, unsureText) {
		t.Errorf("after a 422 on the same form the banner reads %q", got)
	}
	stop := abortRequests(p)
	Eval[bool](p, `(htmx.ajax("get", "/pick/party?q=SH", {source: document.body, swap: "none"}), true)`)
	settle(p)
	stop()
	if got := Eval[string](p, networkText); !strings.Contains(got, unsureText) {
		t.Errorf("after a failed background search the banner reads %q", got)
	}
	if got := Scalar[int](h, count) - before; got != 1 {
		t.Errorf("%d new drafts, want 1", got)
	}
}
