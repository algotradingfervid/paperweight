//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/print/web/webtest"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	cardsJS = `[...document.querySelectorAll("main .card")]`

	saveLater  = "Save and finish later"
	deleteDraf = "Delete draft"

	remarksTyped = "Typed in the second tab"
	badAmount    = "1.234,56"
	amountError  = `"` + badAmount + `" is not a valid amount. Write it like 1,234.50 or 1234.50.`
	rateError    = `"` + badAmount + `" is not a valid number. Write it like 1234.50.`

	photoWide, photoTall = 64, 32 // wider than tall, so a rotation shows
	maxThumb             = 160.0

	listPeers = 26 // one more than a page
)

// actionLabels are the labels of every document action the app has, Print
// included, whichever place the action sits in.
var actionLabels = []string{
	"Submit", sendApproval, saveContinue, saveLater, deleteDraf, "Delete", "Edit", "Withdraw", "Approve", "Reject",
	"Revise", "Cancel", "Close", "Amend", "Print",
}

// actions are the action labels on the page: the bar's buttons and every item
// of More actions.
func actions(s *Session) []string {
	s.t.Helper()
	got := Eval[[]string](s, `[...document.querySelectorAll("main button, main a, .actionbar button, .actionbar a, .more-actions a, .more-actions button")]
  .filter((e) => e.checkVisibility()).map((e) => e.textContent.replace(/\s+/g, " ").trim())`)
	var out []string
	for _, l := range actionLabels {
		if slices.Contains(got, l) {
			out = append(out, l)
		}
	}
	return out
}

// wantActions compares the page's actions with want, in actionLabels order.
func wantActions(t *testing.T, s *Session, what string, want ...string) {
	t.Helper()
	got := actions(s)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s: actions %q, want %q\n%s", what, got, want, s.page())
	}
}

// noInputs checks that the page has nothing to type in or choose, and no
// action whose label starts with Edit ("Edit details" too; CD-248).
func noInputs(t *testing.T, s *Session, what string) {
	t.Helper()
	got := Eval[[]string](s, `[...document.querySelectorAll("main input, main textarea, main select")]
  .filter((e) => e.type !== "hidden").map((e) => e.outerHTML)`)
	if len(got) != 0 {
		t.Errorf("%s: the page has fields: %q", what, got)
	}
	edits := Eval[[]string](s, `[...document.querySelectorAll("main button, main a, .actionbar button, .actionbar a, .more-actions a, .more-actions button")]
  .filter((e) => e.checkVisibility()).map((e) => e.textContent.replace(/\s+/g, " ").trim()).filter((l) => l.startsWith("Edit"))`)
	if len(edits) != 0 {
		t.Errorf("%s: the page offers %q", what, edits)
	}
}

// pendingPO, rejectedPO, cancelledPO and closedPO are the reference PO in that
// state, through the real engine.
func pendingPO(h *Harness) int64 {
	h.t.Helper()
	r := refPO(h)
	h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	return r.ID
}

func rejectedPO(h *Harness) int64 {
	h.t.Helper()
	id := pendingPO(h)
	h.Do(ravi, fixture.Order, id, doctype.ActionReject, doc.ActionInput{Version: orderVersion(h, id), Reason: reasonPO})
	return id
}

func cancelledPO(h *Harness) int64 {
	h.t.Helper()
	id := approvedPO(h)
	h.Do(ravi, fixture.Order, id, doctype.ActionCancel, doc.ActionInput{Version: orderVersion(h, id), Reason: reasonPO})
	return id
}

func closedPO(h *Harness) int64 {
	h.t.Helper()
	id := approvedPO(h)
	h.Do(priya, fixture.Order, id, doctype.ActionClose, doc.ActionInput{Version: orderVersion(h, id), Reason: reasonPO})
	return id
}

// TestE2E_ApprovalInboxRowContents: APR-079. The later submission has the lower
// id, so only the submitted time can put the oldest first.
func TestE2E_ApprovalInboxRowContents(t *testing.T) {
	h := Start(t)
	later := refPO(h)
	oldest := refPO(h)
	h.Do(priya, fixture.Order, oldest.ID, doctype.ActionSubmit, doc.ActionInput{Version: oldest.Version})
	h.Clock.Advance(time.Hour)
	h.Do(priya, fixture.Order, later.ID, doctype.ActionSubmit, doc.ActionInput{Version: later.Version})

	r := h.SignIn(ravi, desktop)
	r.Open("/approvals")
	r.Until("two rows", cardsJS+`.length === 2`)
	cards := Eval[[]string](r, cardsJS+`.map((c) => c.innerText.replace(/\s+/g, " "))`)
	for _, want := range []string{
		draft(oldest.ID), "Purchase Order", "SHREE Shree Steel Traders", refTotal, "Priya Shah", "24-10-2026 10:30",
	} {
		if !strings.Contains(cards[0], want) {
			t.Errorf("the first row has no %q: %s", want, cards[0])
		}
	}
	if !strings.Contains(cards[1], draft(later.ID)) || !strings.Contains(cards[1], "24-10-2026 11:30") {
		t.Errorf("the second row is not the later submission: %s", cards[1])
	}
	if got := Eval[string](r, cardsJS+`[0].querySelector(".card-head a").getAttribute("href")`); got != DocPath(fixture.Order, oldest.ID) {
		t.Errorf("the first row links to %q", got)
	}
}

// TestE2E_Doc_StaleTabConflict: DOC-093, in two tabs of one browser.
func TestE2E_Doc_StaleTabConflict(t *testing.T) {
	h := Start(t)
	id := refPO(h).ID
	details := DocPath(fixture.Order, id) + "/edit/details"
	first := h.SignIn(priya, desktop)
	first.Open(details)
	second := first.SecondTab()
	second.Open(details)

	first.Front()
	first.Fill("remarks", "Saved in the first tab")
	first.Press(saveContinue)
	first.WaitText("Step 2 of 3")

	second.Front()
	second.Fill("remarks", remarksTyped)
	second.Press(saveContinue)
	second.WaitText(staleText(id))
	remarks := `document.getElementById("f-remarks").value`
	if got := Eval[string](second, remarks); got != remarksTyped {
		t.Errorf("remarks after the conflict = %q, want what was typed", got)
	}
	if got := Scalar[string](h, `SELECT remarks FROM fixture_order WHERE id = ?`, id); got != "Saved in the first tab" {
		t.Errorf("stored remarks = %q", got)
	}

	second.Press("Reload")
	second.WaitGone(staleText(id))
	second.Until("the stored remarks", remarks+` === "Saved in the first tab"`)
}

// TestE2E_Doc_ButtonsPerState: DOC-094. Anil holds every right; approving needs
// an eligible approver as well, so pending is also seen by Ravi and Priya.
func TestE2E_Doc_ButtonsPerState(t *testing.T) {
	h := Start(t)
	a, p, r := h.SignIn(admin, desktop), h.SignIn(priya, desktop), h.SignIn(ravi, desktop)
	view := func(s *Session, id int64) {
		t.Helper()
		s.Open(DocPath(fixture.Order, id))
		s.Until("the document page", `document.querySelector(".doc-state, .stepper__bar") !== null`)
	}

	drafted := refPO(h).ID
	view(a, drafted)
	wantActions(t, a, "draft", sendApproval, deleteDraf, "Print")
	a.Open(DocPath(fixture.Order, drafted) + "/edit/details")
	wantActions(t, a, "draft, on a step", saveContinue, saveLater)

	pending := pendingPO(h)
	view(a, pending)
	wantActions(t, a, "pending, for a user with every right", "Withdraw", "Print")
	noInputs(t, a, "pending")
	view(p, pending)
	wantActions(t, p, "pending, for the submitter", "Withdraw", "Print")
	view(r, pending)
	wantActions(t, r, "pending, for the approver", "Approve", "Reject", "Print")

	rejected := rejectedPO(h)
	view(a, rejected)
	wantActions(t, a, "rejected", "Revise", "Print")
	noInputs(t, a, "rejected")

	approved := approvedPO(h)
	view(a, approved)
	wantActions(t, a, "approved", "Cancel", "Close", "Print")
	noInputs(t, a, "approved")

	closed := closedPO(h)
	view(a, closed)
	wantActions(t, a, "closed", "Print")
	noInputs(t, a, "closed")

	cancelled := cancelledPO(h)
	view(a, cancelled)
	wantActions(t, a, "cancelled", "Amend", "Print")
	noInputs(t, a, "cancelled")

	// Outside draft the step forms send the user back to the read-only page.
	a.Open(DocPath(fixture.Order, approved) + "/edit/details")
	a.Until("the document page", `location.pathname === "`+DocPath(fixture.Order, approved)+`"`)
	noInputs(t, a, "the edit URL of an approved order")
}

// historyRows are the History rows, top to bottom, as one line each: the
// heading, then " | " and each change listed below it.
func historyRows(s *Session) []string {
	s.t.Helper()
	return Eval[[]string](s, `[...document.querySelectorAll(".doc-audit .history-row")].map((e) => {
  const text = (n) => n.innerText.replace(/\s+/g, " ").trim();
  return [e.querySelector(".history-head"), ...e.querySelectorAll(".history-changes li")].map(text).join(" | ");
})`)
}

// TestE2E_Audit_HistoryView: DOC-089. History is on the document page, which a
// draft doesn't have, so a submission sits above the case's two rows.
func TestE2E_Audit_HistoryView(t *testing.T) {
	h := Start(t)
	created := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	first, second := refPO(h), refPO(h)
	a := h.SignIn(admin, desktop)

	h.Clock.Set(created.Add(4*time.Hour + 15*time.Minute))
	a.Open(DocPath(fixture.Order, first.ID) + "/edit/details")
	a.Fill("remarks", "Deliver to Pune site")
	a.Press(saveContinue)
	a.WaitText("Step 2 of 3")
	a.Open(DocPath(fixture.Order, second.ID) + "/edit/details")
	a.Fill("amount", "1,30,000.00")
	a.Press(saveContinue)
	a.WaitText("Step 2 of 3")

	h.Clock.Set(created.Add(4*time.Hour + 30*time.Minute))
	h.Do(priya, fixture.Order, first.ID, doctype.ActionSubmit, doc.ActionInput{Version: orderVersion(h, first.ID)})
	h.Do(priya, fixture.Order, second.ID, doctype.ActionSubmit, doc.ActionInput{Version: orderVersion(h, second.ID)})

	// Each row's heading, then the changes listed below it (CD-248).
	openHistory(a, DocPath(fixture.Order, first.ID))
	want := []string{
		"24-10-2026 15:00 · Priya Shah · Submitted",
		"24-10-2026 14:45 · Anil Kapoor · Saved | Remarks: (empty) → Deliver to Pune site",
		"24-10-2026 10:30 · Priya Shah · Created",
	}
	if got := historyRows(a); !slices.Equal(got, want) {
		t.Errorf("history rows:\n%q\nwant\n%q", got, want)
	}

	openHistory(a, DocPath(fixture.Order, second.ID))
	if got, want := historyRows(a), "24-10-2026 14:45 · Anil Kapoor · Saved | Amount: ₹94,164.00 → ₹1,30,000.00"; !slices.Contains(got, want) {
		t.Errorf("history rows %q, want %q", got, want)
	}
}

// TestE2E_ButtonsMatchRights: AUTH-084.
func TestE2E_ButtonsMatchRights(t *testing.T) {
	h := Start(t)
	id := pendingPO(h)
	path := DocPath(fixture.Order, id)
	p, r := h.SignIn(priya, desktop), h.SignIn(ravi, desktop)

	p.Open(path)
	p.WaitText("Withdraw")
	wantActions(t, p, "Priya", "Withdraw", "Print")

	r.Open(path)
	r.WaitText("Approve")
	wantActions(t, r, "Ravi", "Approve", "Reject", "Print")
}

// photo is a JPEG wider than tall.
func photo(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, photoWide, photoTall)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// exifPhoto is photo with EXIF orientation 6 (FILE-034): shown upright it is
// taller than wide. The EXIF segment goes after SOI, before the image's own.
func exifPhoto(t *testing.T) []byte {
	t.Helper()
	exif := filestest.JPEGWithEXIF(6)
	return append(exif[:len(exif)-2:len(exif)-2], photo(t)[2:]...)
}

// attachedClaim is a claim by Deepak carrying files, posted to /attachments as
// a document page's upload would, then submitted: a draft's page is its check
// page, which lists no attachments. It leaves d on the claim's page.
func attachedClaim(h *Harness, d *Session, attach map[string][]byte) {
	h.t.Helper()
	var employee string
	for _, e := range h.Fx.Employees {
		if e.Code == "EMP-007" {
			employee = strconv.FormatInt(e.ID, 10)
		}
	}
	res := h.Do(deepak, fixture.Claim, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": employee, "claim_date": plainDate},
		Lines:  []map[string]string{{"expense_type": "TRAVEL", "amount": "1250.00"}},
	})
	path := DocPath(fixture.Claim, res.ID)
	d.Open(path + "/edit/check")
	d.WaitText("Step 3 of 3")
	for name, data := range attach {
		if got := d.Upload(fmt.Sprintf("/attachments/%s/%d", fixture.Claim, res.ID), name, data); got != 201 {
			h.t.Fatalf("upload %s = %d, want 201", name, got)
		}
	}
	h.Do(deepak, fixture.Claim, res.ID, doctype.ActionSubmit, doc.ActionInput{
		Version: Scalar[int](h, `SELECT version FROM fixture_claim WHERE id = ?`, res.ID),
	})
	d.Open(path)
	d.WaitText("Attachments")
}

// thumbLoaded waits for the attachment image named name to load.
func thumbLoaded(s *Session, name string) string {
	s.t.Helper()
	img := `document.querySelector('.attachments__thumb img[alt="` + name + `"]')`
	s.Until(name+" to load", img+`?.complete === true && `+img+`.naturalWidth > 0`)
	return img
}

// TestE2E_ExpenseAttachmentPreview: FILE-033.
func TestE2E_ExpenseAttachmentPreview(t *testing.T) {
	h := Start(t)
	d := h.SignIn(deepak, desktop)
	attachedClaim(h, d, map[string][]byte{"r.jpg": photo(t), "r.heic": filestest.HEIC("heic")})

	img := thumbLoaded(d, "r.jpg")
	if got := Eval[string](d, img+`.getAttribute("loading")`); got != "lazy" {
		t.Errorf("the preview loads %q, want lazy", got)
	}
	size := Eval[[]float64](d, `[`+img+`.getBoundingClientRect().width, `+img+`.getBoundingClientRect().height]`)
	if size[0] == 0 || size[0] > maxThumb || size[1] == 0 || size[1] > maxThumb {
		t.Errorf("the preview is %v px, want at most %v", size, maxThumb)
	}

	chip := `document.querySelector("a.attachments__chip")`
	d.WaitText("HEIC image, opens as download")
	if !Eval[bool](d, chip+`.hasAttribute("download") && `+chip+`.querySelector("svg") !== null`) {
		t.Errorf("the HEIC item isn't an icon with a download link: %s", Eval[string](d, chip+`.outerHTML`))
	}
	if got := Eval[int](d, `document.querySelectorAll(".attachments img").length`); got != 1 {
		t.Errorf("%d images in the list, want only r.jpg's", got)
	}
}

// TestE2E_ExifOrientation: FILE-035, on an image wider than tall as stored.
// The computed style is the check: Chrome's natural size follows EXIF
// whatever the CSS, so it can't fail (CD-248).
func TestE2E_ExifOrientation(t *testing.T) {
	h := Start(t)
	d := h.SignIn(deepak, desktop)
	attachedClaim(h, d, map[string][]byte{"r-rotated.jpg": exifPhoto(t)})

	img := thumbLoaded(d, "r-rotated.jpg")
	if got := Eval[string](d, `getComputedStyle(`+img+`).imageOrientation`); got != "from-image" {
		t.Errorf("image-orientation = %q", got)
	}
}

// TestE2E_PrintPageNumbers: FILE-058, on webtest's 60-line Draft 42 and its
// approved PO. N is the PDF's page count (CD-248); Draft 42 runs to more than one.
func TestE2E_PrintPageNumbers(t *testing.T) {
	srv := webtest.NewServer(t, testkit.Principal(t, priya))
	s := &Session{t: t, ctx: uibrowser.New(t), width: desktop}

	pages := ParsePDF(t, printPDF(s, fmt.Sprintf("%s/print/test.PurchaseOrder/%d", srv.URL, webtest.DraftPOID)))
	if len(pages) < 2 {
		t.Fatalf("pages = %d, want more than one", len(pages))
	}
	for i, pg := range pages {
		want := fmt.Sprintf("Draft%d·Page%dof%d", webtest.DraftPOID, i+1, len(pages))
		if !strings.Contains(pg.Text(), want) {
			t.Errorf("page %d has no %q: %s", i+1, want, tail(pg.Text()))
		}
	}

	pages = ParsePDF(t, printPDF(s, fmt.Sprintf("%s/print/test.PurchaseOrder/%d", srv.URL, webtest.ReferencePOID)))
	if want := "PO/26-27/00001·Page1of" + strconv.Itoa(len(pages)); !strings.Contains(pages[0].Text(), want) {
		t.Errorf("the approved PO's page has no %q: %s", want, tail(pages[0].Text()))
	}
}

// tail is the end of a page's text, where the footer is.
func tail(s string) string {
	const n = 200
	return s[max(len(s)-n, 0):]
}

// mediaPrint switches the tab to print media.
func mediaPrint(s *Session) {
	s.t.Helper()
	s.Do("print media", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, emulation.SetEmulatedMedia, emulation.SetEmulatedMediaParams{Media: "print"})
		return err
	}))
}

// TestE2E_PrintButton: FILE-070. window.print is replaced to see the toolbar
// call it, since a headless browser prints nothing.
func TestE2E_PrintButton(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	path := DocPath(fixture.Order, id)
	printPath := fmt.Sprintf("/print/%s/%d", fixture.Order, id)
	r := h.SignIn(ravi, desktop)
	r.Open(path)
	r.WaitText(firstNumber)

	ctx, cancel := context.WithTimeout(r.ctx, stepDeadline)
	defer cancel()
	opened := chromedp.WaitNewTarget(ctx, func(i *target.Info) bool { return strings.HasSuffix(i.URL, printPath) })
	r.Press("Print")
	var tabID target.ID
	select {
	case tabID = <-opened:
	case <-ctx.Done():
		t.Fatalf("Print opened no new tab: %v\n%s", ctx.Err(), r.page())
	}
	tab := r.tab(chromedp.WithTargetID(tabID))

	toolbar := `document.querySelector(".toolbar")`
	tab.Until("the toolbar", toolbar+`?.checkVisibility() === true`)
	tab.WaitText("Print / Save as PDF")
	tab.WaitText("Close")
	if got := Eval[string](r, `location.pathname`); got != path {
		t.Errorf("the document's tab moved to %s", got)
	}
	Eval[chromedp.Void](tab, `window.print = () => { window.printed = true; }`)
	tab.Press("Print / Save as PDF")
	tab.Until("window.print", `window.printed === true`)
	mediaPrint(tab)
	tab.Until("the toolbar hidden in print", toolbar+`.checkVisibility() === false`)

	// Suresh can read the order, but has no print right (FILE-050).
	st := h.SignIn(suresh, desktop)
	st.Open(path)
	st.WaitText(firstNumber)
	if got := actions(st); slices.Contains(got, "Print") {
		t.Errorf("Suresh's actions %q include Print", got)
	}
	st.Open(printPath)
	st.WaitText("You don't have permission to print Purchase Order.")
	got := Eval[int](st, `fetch(location.href).then((r) => r.status)`, chromedp.EvalAwaitPromise)
	if got != 403 {
		t.Errorf("the print URL answers %d, want 403", got)
	}
}

// TestE2E_AmountInputFormatting: NUM-059. NUM-057's text is money's, and Rate
// has its own, so both fields are typed in. A fixture order line has no amount
// of its own, so "the amount updates" is a claim line's amount, in the line
// grid, moving the claim's total.
func TestE2E_AmountInputFormatting(t *testing.T) {
	h := Start(t)
	id := refPO(h).ID
	p := h.SignIn(priya, desktop)
	p.Open(DocPath(fixture.Order, id) + "/lines/new")
	rate := `document.getElementById("f-rate").value`
	p.Fill("rate", "1411200")
	p.Tab()
	p.Until("the rate formatted", rate+` === "14,11,200.00"`)

	p.Fill("rate", badAmount)
	p.Tab()
	p.WaitText(rateError)
	if got := Eval[string](p, rate); got != badAmount {
		t.Errorf("rate after the error = %q, want what was typed", got)
	}

	p.Open(DocPath(fixture.Order, id) + "/edit/details")
	p.Fill("amount", badAmount)
	p.Tab()
	p.WaitText(amountError)
	if got := Eval[string](p, `document.getElementById("f-amount").value`); got != badAmount {
		t.Errorf("amount after the error = %q, want what was typed", got)
	}

	var employee string
	for _, e := range h.Fx.Employees {
		if e.Code == "EMP-007" {
			employee = strconv.FormatInt(e.ID, 10)
		}
	}
	claim := h.Do(deepak, fixture.Claim, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": employee, "claim_date": plainDate},
		Lines:  []map[string]string{{"expense_type": "TRAVEL", "amount": "1250.00"}},
	})
	d := h.SignIn(deepak, desktop)
	d.Open(DocPath(fixture.Claim, claim.ID) + "/edit/items")
	d.Until("the grid", `document.getElementById("grid-1-amount")?.checkVisibility() === true`)
	d.WaitText("₹1,250.00")
	focusSelected(d, "grid-1-amount")
	d.Keys("1411200")
	d.Tab()
	d.Until("the amount formatted", `document.getElementById("grid-1-amount").value === "14,11,200.00"`)
	d.Until("the new total", `document.getElementById("doc-totals").innerText.includes("₹14,11,200.00")`)
}

// listRowIDs are the document ids of the rows shown, in order.
func listRowIDs(s *Session) []string {
	s.t.Helper()
	return Eval[[]string](s, `[...new Set([...document.querySelectorAll('main a[href^="/doc/`+fixture.Order+`/"]')]
  .filter((a) => a.checkVisibility()).map((a) => a.getAttribute("href").split("/").pop()).filter((id) => /^[0-9]+$/.test(id)))]`)
}

func wantRows(t *testing.T, s *Session, what string, want []string) {
	t.Helper()
	s.Until(what, `document.getElementById("filter-q") !== null`)
	if got := listRowIDs(s); !slices.Equal(got, want) {
		t.Errorf("%s: rows %q, want %q", what, got, want)
	}
}

// TestE2E_ListStateInURL: NUM-125, on fixture.Order (search over remarks, a
// sortable amount and the state filter). The list has no filter control yet,
// so the status filter arrives as a URL; search, sort and page go through the
// page. Amounts stay under the approval rule, so submit approves.
func TestE2E_ListStateInURL(t *testing.T) {
	h := Start(t)
	for i := range listPeers + 1 {
		amount := 30000 + (i*7%(listPeers+1))*100 // out of id order, under 50,000
		remarks := "steel for the depot"
		if i == 0 {
			remarks = "cement for the depot"
		}
		r := h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
			Values: map[string]string{
				"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
				"order_date": plainDate, "amount": strconv.Itoa(amount) + ".00", "remarks": remarks,
			},
			Lines: []map[string]string{{"item_id": strconv.FormatInt(h.M.Item["WIRE-18G"], 10), "qty": "50.000", "rate": "96.0000", "uom": "kg"}},
		})
		if s := h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version}); s.State != doctype.StateApproved {
			t.Fatalf("order %d is %s after submit, want approved (no rule)", r.ID, s.State)
		}
	}
	// Matches the search; only the status filter drops it.
	h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
			"order_date": plainDate, "amount": "49999.00", "remarks": "steel, not sent yet",
		},
	})

	want := func(limit, offset int) []string {
		t.Helper()
		rows, err := h.App.DB().SQL().QueryContext(context.Background(), `SELECT id FROM fixture_order
			WHERE state = 'approved' AND remarks LIKE '%steel%' ORDER BY amount DESC, id DESC LIMIT ? OFFSET ?`, limit, offset)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rows.Close() }()
		var out []string
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			out = append(out, strconv.FormatInt(id, 10))
		}
		return out
	}

	p := h.SignIn(priya, desktop)
	p.Open("/doc/" + fixture.Order + "?f=state:=:approved")
	p.Until("the list", `document.getElementById("filter-q") !== null`)
	p.FillID("filter-q", "steel")
	p.Keys("\r")
	p.Until("the search in the URL", `location.search.includes("q=steel")`)
	p.WaitText("26 purchase orders")
	p.Do("sort by amount", chromedp.Evaluate[chromedp.Void](`(() => {
  const e = document.getElementById("filter-sort");
  e.value = "amount.desc";
  e.dispatchEvent(new Event("change", {bubbles: true}));
})()`))
	p.Until("the sort in the URL", `location.search.includes("sort=amount.desc")`)
	p.Press("Next")
	p.Until("page 2", `location.search.includes("page=2")`)

	search := Eval[string](p, `location.search`)
	for _, wantParam := range []string{"f=state%3A%3D%3Aapproved", "q=steel", "sort=amount.desc", "page=2"} {
		if !strings.Contains(search, wantParam) {
			t.Errorf("URL %s has no %s", search, wantParam)
		}
	}
	url := "/doc/" + fixture.Order + search
	onePage := want(listPeers, listPeers-1)
	if len(onePage) != 1 {
		t.Fatalf("page 2 holds %d rows, want 1", len(onePage))
	}
	wantRows(t, p, "page 2", onePage)

	p.Open(url)
	wantRows(t, p, "after a reload", onePage)

	p.Open("/doc/" + fixture.Order + "/" + onePage[0])
	p.WaitText("Details")
	p.Back()
	p.Until("the list again", `location.search === `+strconv.Quote(search))
	wantRows(t, p, "after Back", onePage)
	if got := Eval[string](p, `document.getElementById("filter-q").value`); got != "steel" {
		t.Errorf("search box after Back = %q", got)
	}

	m := h.SignIn(meena, desktop)
	m.Open(url)
	wantRows(t, m, "as Meena", onePage)
}
