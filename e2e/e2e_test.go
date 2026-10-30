//go:build e2e

package e2e

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print/web/webtest"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

const (
	admin  = "admin@vardhan.test"
	meena  = "meena@vardhan.test"
	ravi   = "ravi@vardhan.test"
	priya  = "priya@vardhan.test"
	suresh = "suresh@vardhan.test"
	deepak = "deepak@vardhan.test"

	fixtureDate = "24-10-2026"
	plainDate   = "2026-10-24"
	firstNumber = "PO/26-27/00001"
	amendNumber = "PO/26-27/00001-1"
	firstGRN    = "GR/26-27/00001"
	firstClaim  = "EX/26-27/00001"
	refTotal    = "₹94,164.00"
	reasonPO    = "Vendor changed the terms"
	reasonGRN   = "Wrong quantity entered"

	phone360 = 360
	phone390 = 390
	desktop  = 1440

	saveContinue = "Save and continue"
	addItem      = "Add an item"
	saveLine     = "Save line"
	sendApproval = "Submit for approval"

	inboxLink = `document.querySelector("a.inbox-link")`
)

// In a printed page's text, with its spaces gone: the table's header row, and
// one whole line, Sr to amount, with its description between name and HSN. The
// previous amount's last digits run into Sr, so Sr can read "03".
const printHeader = "SrItemHSNQtyUoMRateAmount"

var printedRow = regexp.MustCompile(`(\d{1,2})(?:MS-ANGLE-50MSAngle50x50x5|WIRE-18GBindingWire18G)` +
	`(Cutto6mlengths,primercoatedanddeliveredtothePUNEstoresbay3withmilltestcertificates)?` +
	`721[67](?:1,200|50)kg₹(?:62\.50|96\.00)₹(?:75,000|4,800)\.00`)

// staleText is DOC-069's message for draft id.
func staleText(id int64) string {
	return fmt.Sprintf("Someone else changed Draft %d after you opened it. Reload it and make your changes again.", id)
}

func draft(id int64) string { return "Draft " + strconv.FormatInt(id, 10) }

// orderDetails fills step 1 of the order wizard with the reference PO's header.
func orderDetails(s *Session) {
	s.t.Helper()
	s.Pick("supplier_id", "SHREE")
	s.Pick("project_id", "PRJ-METRO")
	s.Fill("order_date", fixtureDate)
	s.Fill("amount", "94,164.00")
}

// addOrderLine adds a line through the line page, from the items step.
func addOrderLine(s *Session, item, qty, rate, uom string) {
	s.t.Helper()
	s.Press(addItem)
	s.WaitText(saveLine)
	s.Pick("item_id", item)
	s.Fill("qty", qty)
	s.Fill("rate", rate)
	s.Fill("uom", uom)
	s.Press(saveLine)
	s.WaitText("Step 2 of 3")
}

// progress is the wizard progress bar's value.
func progress(s *Session) int {
	s.t.Helper()
	return Eval[int](s, `Number(document.querySelector("progress.stepper__bar").value)`)
}

// wizardOrder creates the reference PO through the wizard and leaves s on the
// check step. It checks the progress bar at each step.
func wizardOrder(s *Session) int64 {
	s.t.Helper()
	s.Open("/doc/" + fixture.Order + "/new")
	if got := progress(s); got != 1 {
		s.t.Errorf("progress on step 1 = %d, want 1", got)
	}
	orderDetails(s)
	s.Press(saveContinue)
	s.WaitText("Step 2 of 3")
	if got := progress(s); got != 2 {
		s.t.Errorf("progress on step 2 = %d, want 2", got)
	}
	id := s.DocID()
	addOrderLine(s, "MS-ANGLE-50", "1,200.000", "62.5000", "kg")
	addOrderLine(s, "WIRE-18G", "50.000", "96.0000", "kg")
	s.Press(saveContinue)
	s.WaitText("Step 3 of 3")
	if got := progress(s); got != 3 {
		s.t.Errorf("progress on step 3 = %d, want 3", got)
	}
	return id
}

// refPO is the reference PO, saved by Priya through the real engine.
func refPO(h *Harness) doc.Result {
	h.t.Helper()
	return h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
			"order_date": plainDate, "amount": "94164.00",
		},
		Lines: []map[string]string{
			{"item_id": strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10), "qty": "1200.000", "rate": "62.5000", "uom": "kg"},
			{"item_id": strconv.FormatInt(h.M.Item["WIRE-18G"], 10), "qty": "50.000", "rate": "96.0000", "uom": "kg"},
		},
	})
}

// approvedPO is the reference PO, approved by Ravi.
func approvedPO(h *Harness) int64 {
	h.t.Helper()
	r := refPO(h)
	s := h.Do(priya, fixture.Order, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	h.Do(ravi, fixture.Order, r.ID, doctype.ActionApprove, doc.ActionInput{Version: s.Version})
	return r.ID
}

// orderLine is the id of an order's line no.
func orderLine(h *Harness, order int64, no int) int64 {
	h.t.Helper()
	return Scalar[int64](h, `SELECT id FROM fixture_order_line WHERE parent_id = ? AND line_no = ?`, order, no)
}

func docState(h *Harness, table string, id int64) string {
	h.t.Helper()
	return Scalar[string](h, `SELECT state FROM `+table+` WHERE id = ?`, id)
}

// wizardReceipt creates and submits a receipt for one order line through the
// wizard and returns its id, leaving s on its document page.
func wizardReceipt(s *Session, orderLineID int64, qty string) int64 {
	s.t.Helper()
	s.Open("/doc/" + fixture.Receipt + "/new")
	s.Fill("receipt_date", fixtureDate)
	s.Press(saveContinue)
	s.WaitText("Step 2 of 3")
	id := s.DocID()
	s.Press(addItem)
	s.WaitText(saveLine)
	s.Fill("order_line_id", strconv.FormatInt(orderLineID, 10))
	s.Fill("qty", qty)
	s.Press(saveLine)
	s.WaitText("Step 2 of 3")
	s.Press(saveContinue)
	s.WaitText("Step 3 of 3")
	s.Press("Submit")
	s.WaitText(firstGRN)
	return id
}

// cancelDoc opens the document's reason page from More actions and posts the
// reason.
func cancelDoc(s *Session, path, label, reason string) {
	s.t.Helper()
	s.Open(path)
	s.Press("Cancel")
	s.WaitText("Reason for cancelling")
	s.Fill("reason", reason)
	s.Press(label)
}

// openHistory opens the document's History disclosure and returns its text.
func openHistory(s *Session, path string) string {
	s.t.Helper()
	s.Open(path)
	s.Do("open History", chromedp.Click(chromedp.CSS(".doc-audit summary")))
	s.Until("the history rows", `document.querySelector(".doc-audit .history-row")?.checkVisibility() === true`)
	return Eval[string](s, `document.querySelector(".doc-audit").innerText`)
}

// TestE2E_Doc_PurchaseLifecycle: DOC-092.
func TestE2E_Doc_PurchaseLifecycle(t *testing.T) {
	h := Start(t)
	p, r, st := h.SignIn(priya, phone390), h.SignIn(ravi, desktop), h.SignIn(suresh, phone360)
	a := h.SignIn(admin, desktop)

	order := wizardOrder(p)
	path := DocPath(fixture.Order, order)
	p.Press(sendApproval)
	p.WaitText("Pending approval")
	p.WaitText(draft(order))

	r.Open(path)
	r.Press("Approve")
	r.WaitText("Approved. " + firstNumber + " has been numbered.")
	r.Open(path)
	r.WaitText(firstNumber)
	r.WaitText("Approved")

	receipt := wizardReceipt(st, orderLine(h, order, 1), "400.000")
	r.Open(path)
	r.WaitText("Received 400 of 1,200 kg")

	grnPath := DocPath(fixture.Receipt, receipt)
	cancelDoc(r, path, "Cancel purchase order", reasonPO)
	r.WaitText("Can't cancel " + firstNumber + " because " + firstGRN + " (approved) is linked to it. Cancel " + firstGRN + " first.")
	r.Until("the link to the GRN", `document.querySelector('[role=alert] a[href="`+grnPath+`"]') !== null`)
	if got := docState(h, "fixture_order", order); got != "approved" {
		t.Errorf("order state after the refused cancel = %q, want approved", got)
	}

	cancelDoc(a, grnPath, "Cancel goods receipt", reasonGRN)
	a.WaitText("Cancelled")
	r.Open(path)
	r.WaitText("Received 0 of 1,200 kg")

	cancelDoc(r, path, "Cancel purchase order", reasonPO)
	r.WaitText("Cancelled")

	r.Open(path)
	r.Press("Amend")
	r.WaitText("Amended from " + firstNumber)
	amended := r.DocID()
	if amended == order {
		t.Fatalf("amend stayed on order %d", order)
	}
	r.WaitText(draft(amended))
	r.Until("the link to the original", `document.querySelector('a[href="`+path+`"]')?.textContent === "Amended from `+firstNumber+`"`)

	history := openHistory(r, path)
	for _, want := range []string{"Priya Shah · submit", "Ravi Kulkarni · approve", "Ravi Kulkarni · cancel: " + reasonPO} {
		if !strings.Contains(history, want) {
			t.Errorf("order history has no %q:\n%s", want, history)
		}
	}
	history = openHistory(a, grnPath)
	for _, want := range []string{"Suresh Patil · submit", "Anil Kapoor · cancel: " + reasonGRN} {
		if !strings.Contains(history, want) {
			t.Errorf("receipt history has no %q:\n%s", want, history)
		}
	}
}

// TestE2E_ApprovalWithdraw: APR-097.
func TestE2E_ApprovalWithdraw(t *testing.T) {
	h := Start(t)
	id := refPO(h).ID
	path := DocPath(fixture.Order, id)
	p, r := h.SignIn(priya, desktop), h.SignIn(ravi, desktop)

	p.Open(path)
	p.Press(sendApproval)
	p.WaitText("Pending approval")
	p.WaitText("Withdraw")

	r.Open("/approvals")
	r.WaitText(draft(id))
	if got := Eval[string](r, inboxLink+`.getAttribute("aria-label")`); got != "Waiting for my approval: 1" {
		t.Errorf("Ravi's count label = %q", got)
	}
	if got := Eval[string](r, inboxLink+`.querySelector(".badge").textContent`); got != "1" {
		t.Errorf("Ravi's count = %q, want 1", got)
	}

	p.Press("Withdraw")
	p.WaitText("Withdrawn. You can change the draft and submit it again.")
	p.WaitText(sendApproval)
	if got := docState(h, "fixture_order", id); got != "draft" {
		t.Errorf("state after withdraw = %q, want draft", got)
	}
	p.Open(path + "/edit/details")
	p.Fill("remarks", "Revised after withdraw")
	p.Press(saveContinue)
	p.WaitText("Step 2 of 3")
	if got := Scalar[string](h, `SELECT remarks FROM fixture_order WHERE id = ?`, id); got != "Revised after withdraw" {
		t.Errorf("remarks after the edit = %q", got)
	}

	r.Open("/approvals")
	r.WaitText("Nothing is waiting for your approval.")
	if r.Shows(draft(id)) {
		t.Errorf("%s is still in Ravi's list", draft(id))
	}
	if got := Eval[bool](r, inboxLink+`.querySelector(".badge") === null`); !got {
		t.Error("Ravi's count still shows")
	}
}

// TestE2E_NamingSettingsList: NAM-051.
func TestE2E_NamingSettingsList(t *testing.T) {
	h := Start(t)
	at := "2026-10-24T05:00:00.000Z"
	if _, err := h.App.DB().SQL().ExecContext(context.Background(),
		`INSERT INTO naming_counters (doctype, prefix, last, created_at, updated_at) VALUES (?, ?, 7, ?, ?)`,
		fixture.Order, "PO/26-27/", at, at); err != nil {
		t.Fatal(err)
	}
	a := h.SignIn(admin, desktop)
	a.Press("Numbering")
	for _, want := range []string{
		"Purchase Order · PO/{FY}/{#####} · Next: PO/26-27/00008",
		"Goods Receipt · GR/{FY}/{#####} · Next: GR/26-27/00001",
		"Expense Claim · EX/{FY}/{#####} · Next: EX/26-27/00001",
	} {
		a.WaitText(want)
	}
}

// printPDF opens path and prints it with the page's own CSS page size.
func printPDF(s *Session, url string) []byte {
	s.t.Helper()
	if err := uibrowser.At(s.ctx, s.width, url); err != nil {
		s.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(s.ctx, stepDeadline)
	defer cancel()
	data, err := chromedp.Run(ctx, chromedp.PrintToPDF(chromedp.PDFPreferCSSPageSize(), chromedp.PDFPrintBackground()))
	if err != nil {
		s.t.Fatalf("print %s: %v", url, err)
	}
	return data
}

// TestE2E_PrintA4: FILE-056.
func TestE2E_PrintA4(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	p := h.SignIn(priya, desktop)
	pages := ParsePDF(t, printPDF(p, fmt.Sprintf("%s/print/%s/%d", h.URL, fixture.Order, id)))
	if len(pages) != 1 {
		t.Fatalf("pages = %d, want 1", len(pages))
	}
	const a4w, a4h, slack = 595.0, 842.0, 1.0
	pg := pages[0]
	if pg.Width < a4w-slack || pg.Width > a4w+slack || pg.Height < a4h-slack || pg.Height > a4h+slack {
		t.Errorf("page is %.2f × %.2f pt, want 595 × 842 (±1)", pg.Width, pg.Height)
	}
	css := Eval[string](p, `fetch("/print/assets/print.css").then((r) => r.text())`, chromedp.EvalAwaitPromise)
	if !strings.Contains(strings.Join(strings.Fields(css), " "), "@page { size: A4 portrait; margin: 15mm;") {
		t.Errorf("print.css has no @page rule for A4 portrait with 15 mm margins:\n%s", css[:min(len(css), 400)])
	}
}

// TestE2E_PrintLongTable: FILE-057, on webtest's 60-line Draft 42: a fixture
// DocType's print view always prints the reference PO's two lines.
func TestE2E_PrintLongTable(t *testing.T) {
	srv := webtest.NewServer(t, testkit.Principal(t, priya))
	s := &Session{t: t, ctx: uibrowser.New(t), width: desktop}
	url := fmt.Sprintf("%s/print/test.PurchaseOrder/%d", srv.URL, webtest.DraftPOID)
	pages := ParsePDF(t, printPDF(s, url))
	if len(pages) < 2 {
		t.Fatalf("pages = %d, want more than one", len(pages))
	}
	seen := map[int]int{}
	for i, pg := range pages {
		text := pg.Text()
		rows := printedRow.FindAllStringSubmatchIndex(text, -1)
		if len(rows) > 0 && !strings.Contains(text[:rows[0][0]], printHeader) {
			t.Errorf("page %d: no header row above its first line: %.200s", i+1, text)
		}
		for _, m := range rows {
			sr, err := strconv.Atoi(text[m[2]:m[3]])
			if err != nil {
				t.Fatal(err)
			}
			seen[sr]++
			if wrapped := sr%3 == 0; wrapped != (m[4] >= 0) {
				t.Errorf("line %d: description printed = %v, want %v", sr, m[4] >= 0, wrapped)
			}
		}
	}
	for n := 1; n <= webtest.DraftLines; n++ {
		if seen[n] != 1 {
			t.Errorf("line %d is printed whole %d times, want once", n, seen[n])
		}
	}
	last := pages[len(pages)-1].Text()
	for _, want := range []string{"TaxableValue", "GrandTotal", "RupeesTwentyEightLakh", "Terms", "AuthorisedSignatory"} {
		if !strings.Contains(last, want) {
			t.Errorf("the last page has no %q: %s", want, last)
		}
		for i, pg := range pages[:len(pages)-1] {
			if strings.Contains(pg.Text(), want) {
				t.Errorf("%q is on page %d, not only on the last page", want, i+1)
			}
		}
	}
}

// TestE2E_PrintNoExternal: FILE-060.
func TestE2E_PrintNoExternal(t *testing.T) {
	h := Start(t)
	id := approvedPO(h)
	p := h.SignIn(priya, desktop)
	path := fmt.Sprintf("/print/%s/%d", fixture.Order, id)
	var (
		mu   sync.Mutex
		urls []string
	)
	csp := make(chan string, 1)
	requests := chromedp.Events(p.ctx, network.RequestWillBeSent)
	responses := chromedp.Events(p.ctx, network.ResponseReceived)
	go func() {
		for ev, err := range requests {
			if err != nil {
				return
			}
			mu.Lock()
			urls = append(urls, ev.Request.URL)
			mu.Unlock()
		}
	}()
	go func() {
		for ev, err := range responses {
			if err != nil {
				return
			}
			if strings.HasSuffix(ev.Response.URL, path) {
				csp <- fmt.Sprint(ev.Response.Headers["Content-Security-Policy"])
				return
			}
		}
	}()
	p.Do("enable the network log", chromedp.Func(func(ctx context.Context, _ *chromedp.Target) error {
		_, err := chromedp.Call(ctx, network.Enable, network.EnableParams{})
		return err
	}))
	p.Open(path)
	p.Until("the page to load", `document.readyState === "complete"`)
	select {
	case got := <-csp:
		if want := "default-src 'self'; img-src 'self' data:"; got != want {
			t.Errorf("page CSP = %q, want %q", got, want)
		}
	case <-time.After(stepDeadline):
		t.Fatal("no response seen for the print page")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(urls) == 0 {
		t.Fatal("the network log is empty")
	}
	base, err := url.Parse(h.URL)
	if err != nil {
		t.Fatal(err)
	}
	sawPage := false
	for _, raw := range urls {
		if strings.HasPrefix(raw, "data:") {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if u.Scheme+"://"+u.Host != base.Scheme+"://"+base.Host {
			t.Errorf("request to another origin: %s", raw)
		}
		sawPage = sawPage || u.Path == path
	}
	if !sawPage {
		t.Errorf("the log doesn't hold the page itself: %v", urls)
	}
}

// TestE2E_FixtureLifecycle: spec §10.2.
func TestE2E_FixtureLifecycle(t *testing.T) {
	h := Start(t)
	p, r, st := h.SignIn(priya, phone390), h.SignIn(ravi, desktop), h.SignIn(suresh, phone360)
	a := h.SignIn(admin, desktop)

	order := wizardOrder(p)
	path := DocPath(fixture.Order, order)
	p.WaitText(refTotal)
	p.WaitText("Shree Steel Traders")
	p.Press(sendApproval)
	p.WaitText("Pending approval")

	r.Open("/approvals")
	r.WaitText(draft(order))
	r.Press("Open " + draft(order))
	r.Press("Approve")
	r.WaitText(firstNumber)
	r.WaitText("Approved")
	if got := Scalar[string](h, `SELECT number FROM fixture_order WHERE id = ?`, order); got != firstNumber {
		t.Errorf("number = %q, want %s", got, firstNumber)
	}

	receipt := wizardReceipt(st, orderLine(h, order, 1), "400.000")

	cancelDoc(r, path, "Cancel purchase order", reasonPO)
	r.WaitText("Can't cancel " + firstNumber + " because " + firstGRN + " (approved) is linked to it. Cancel " + firstGRN + " first.")

	cancelDoc(a, DocPath(fixture.Receipt, receipt), "Cancel goods receipt", reasonGRN)
	a.WaitText("Cancelled")
	cancelDoc(r, path, "Cancel purchase order", reasonPO)
	r.WaitText("Cancelled")

	r.Open(path)
	r.Press("Amend")
	r.WaitText("Amended from " + firstNumber)
	amended := r.DocID()

	p.Open(DocPath(fixture.Order, amended))
	p.Press(sendApproval)
	p.WaitText("Pending approval")
	// Ravi made the amendment, so he can't approve it himself.
	m := h.SignIn(meena, desktop)
	m.Open(DocPath(fixture.Order, amended))
	m.Press("Approve")
	m.WaitText(amendNumber)
	if got := docState(h, "fixture_order", amended); got != "approved" {
		t.Errorf("amendment state = %q, want approved", got)
	}
}

// TestE2E_ConcurrentApproveSecondSeesMessage: Review Focus 3. Both pages are
// open with Approve showing, on two phones; Ravi (Purchase Manager) and Meena
// (Director) are both eligible at ₹94,164.00.
func TestE2E_ConcurrentApproveSecondSeesMessage(t *testing.T) {
	h := Start(t)
	res := refPO(h)
	h.Do(priya, fixture.Order, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	path := DocPath(fixture.Order, res.ID)
	r, m := h.SignIn(ravi, phone390), h.SignIn(meena, phone360)
	r.Open(path)
	m.Open(path)
	r.WaitText("Approve")
	m.WaitText("Approve")

	r.Press("Approve")
	r.WaitText("Approved. " + firstNumber + " has been numbered.")
	m.Press("Approve")
	m.WaitText(firstNumber + " is no longer waiting for approval. It was approved by Ravi Kulkarni.")

	if got := Scalar[int](h, `SELECT count(*) FROM naming_issued WHERE doctype = ?`, fixture.Order); got != 1 {
		t.Errorf("numbers issued = %d, want 1", got)
	}
	if got := Scalar[string](h, `SELECT number FROM fixture_order WHERE id = ?`, res.ID); got != firstNumber {
		t.Errorf("number = %q", got)
	}
}

// TestE2E_WizardKeepsDataOnBack: browser Back from step 3 to step 2, and on to
// step 1, shows the saved values, and saving again is not refused as stale.
func TestE2E_WizardKeepsDataOnBack(t *testing.T) {
	h := Start(t)
	p := h.SignIn(priya, phone390)
	p.Open("/doc/" + fixture.Order + "/new")
	orderDetails(p)
	p.Fill("remarks", "Deliver to bay 3")
	p.Press(saveContinue)
	p.WaitText("Step 2 of 3")
	addOrderLine(p, "MS-ANGLE-50", "1,200.000", "62.5000", "kg")
	p.Press(saveContinue)
	p.WaitText("Step 3 of 3")

	p.Back()
	p.WaitText("Step 2 of 3")
	p.WaitText("MS Angle 50x50x5")
	p.WaitText("1,200 kg")

	p.Press("Completed: Details")
	p.WaitText("Step 1 of 3")
	if got := Eval[string](p, `document.getElementById("f-remarks").value`); got != "Deliver to bay 3" {
		t.Errorf("remarks on step 1 = %q", got)
	}
	p.WaitText("SHREE Shree Steel Traders")

	p.Back()
	p.WaitText("Step 2 of 3")
	p.WaitText("1,200 kg")
	p.Press(saveContinue)
	p.WaitText("Step 3 of 3")
	if p.Shows("Someone else changed") {
		t.Error("saving after Back was refused as stale")
	}
}

// gridQty reads a line's stored quantity.
func gridQty(h *Harness, order int64, no int) string {
	h.t.Helper()
	return Scalar[string](h, `SELECT CAST(qty AS TEXT) FROM fixture_order_line WHERE parent_id = ? AND line_no = ?`, order, no)
}

func orderVersion(h *Harness, id int64) int {
	h.t.Helper()
	return Scalar[int](h, `SELECT version FROM fixture_order WHERE id = ?`, id)
}

func auditCount(h *Harness, dt string, id int64) int {
	h.t.Helper()
	return Scalar[int](h, `SELECT count(*) FROM audit_log WHERE doc_type = ? AND doc_id = ?`, dt, id)
}

// focusSelected puts the caret in input id with its text selected, as Tab does.
func focusSelected(s *Session, id string) {
	s.t.Helper()
	s.Do("focus "+id, chromedp.Evaluate[chromedp.Void](`(() => { const e = document.getElementById("`+id+`"); e.focus(); e.select(); })()`))
}

// tabTo presses Tab until input id has focus.
func tabTo(s *Session, id string) {
	s.t.Helper()
	const mostTabs = 20
	for range mostTabs {
		s.Tab()
		if Eval[string](s, `document.activeElement.id`) == id {
			return
		}
	}
	s.t.Fatalf("Tab never reached #%s; focus is on %q", id, Eval[string](s, `document.activeElement.outerHTML`))
}

// TestE2E_LineGridRapidEdits: CD-95, CD-139. Each save is held in flight, so
// the next edit is queued behind it; nothing waits between the rows.
func TestE2E_LineGridRapidEdits(t *testing.T) {
	h := Start(t)
	res := h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": strconv.FormatInt(h.M.Party["SHREE"], 10), "project_id": strconv.FormatInt(h.M.Project["PRJ-METRO"], 10),
			"order_date": plainDate, "amount": "1000.00",
		},
		Lines: []map[string]string{
			{"item_id": strconv.FormatInt(h.M.Item["MS-ANGLE-50"], 10), "qty": "10.000", "rate": "62.5000", "uom": "kg"},
			{"item_id": strconv.FormatInt(h.M.Item["WIRE-18G"], 10), "qty": "20.000", "rate": "96.0000", "uom": "kg"},
			{"item_id": strconv.FormatInt(h.M.Item["BOLT-M12"], 10), "qty": "30.000", "rate": "5.0000", "uom": "Nos"},
		},
	})
	id := res.ID
	items := DocPath(fixture.Order, id) + "/edit/items"
	p := h.SignIn(priya, desktop)
	p.Open(items)
	p.Until("the grid", `document.getElementById("grid-1-qty")?.checkVisibility() === true`)

	version, audits := orderVersion(h, id), auditCount(h, fixture.Order, id)
	const held = 500 * time.Millisecond
	h.DelayPosts("/lines/", held)
	focusSelected(p, "grid-1-qty")
	p.Keys("11")
	tabTo(p, "grid-2-qty")
	p.Keys("22")
	tabTo(p, "grid-3-qty")
	p.Keys("33")
	p.Tab()
	if got := Eval[string](p, `document.getElementById("doc-version").value`); got != strconv.Itoa(version) {
		t.Errorf("version on the page is %s after the third edit, want %d: no save was queued behind another, so this run proves nothing", got, version)
	}
	p.Until("three saves", `document.getElementById("doc-version").value === "`+strconv.Itoa(version+3)+`"`)
	h.DelayPosts("/lines/", 0)

	if p.Shows("Someone else changed") {
		t.Error("a conflict banner appeared against the user's own edits")
	}
	if got := orderVersion(h, id); got != version+3 {
		t.Errorf("version = %d, want %d", got, version+3)
	}
	for no, want := range map[int]string{1: "11000", 2: "22000", 3: "33000"} {
		if got := gridQty(h, id, no); got != want {
			t.Errorf("line %d qty = %s, want %s", no, got, want)
		}
	}
	if got := auditCount(h, fixture.Order, id); got != audits+3 {
		t.Errorf("audit entries = %d, want %d", got, audits+3)
	}

	p.Press(saveContinue)
	p.WaitText("Step 3 of 3")
	if p.Shows("Someone else changed") {
		t.Error("the step save after the grid edits was refused")
	}

	// A second browser saves in between: Priya's stale save is refused and
	// changes nothing.
	p.Open(items)
	p.Until("the grid", `document.getElementById("grid-1-qty")?.checkVisibility() === true`)
	other := h.SignIn(priya, desktop)
	other.Open(items)
	other.Until("the grid", `document.getElementById("grid-1-qty")?.checkVisibility() === true`)
	before := orderVersion(h, id)
	focusSelected(other, "grid-2-qty")
	other.Keys("44")
	other.Tab()
	other.Until("the other save", `document.getElementById("doc-version").value === "`+strconv.Itoa(before+1)+`"`)

	focusSelected(p, "grid-3-qty")
	p.Keys("55")
	p.Tab()
	p.WaitText(staleText(id))
	if got := gridQty(h, id, 2); got != "44000" {
		t.Errorf("the other user's line 2 qty = %s, want 44000", got)
	}
	if got := gridQty(h, id, 3); got != "33000" {
		t.Errorf("Priya's stale line 3 qty = %s, want 33000", got)
	}
	if got := orderVersion(h, id); got != before+1 {
		t.Errorf("version after the stale save = %d, want %d", got, before+1)
	}
}

// receiptPhoto writes a 1 × 1 JPEG, as a phone's camera would hand one over.
func receiptPhoto(t *testing.T) string {
	t.Helper()
	const jpegBase64 = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDAAgGBgcGBQgHBwcJCQgKDBQNDAsLDBkSEw8UHRofHh0aHBwgJC4nICIsIxwcKDcpLDAxNDQ0Hyc5PTgyPC4zNDL/wAALCAABAAEBAREA/8QAFAABAAAAAAAAAAAAAAAAAAAACf/EABQQAQAAAAAAAAAAAAAAAAAAAAD/2gAIAQEAAD8AVN//2Q=="
	data, err := base64.StdEncoding.DecodeString(jpegBase64)
	if err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(t.TempDir(), "receipt.jpg")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestE2E_ExpenseFromPhoneWithReceipt: Deepak at 360 px, Take photo, submit;
// Priya sees it through the team scope and approves.
func TestE2E_ExpenseFromPhoneWithReceipt(t *testing.T) {
	t.Skip("no document screen has a Take photo control yet: AD-7 CONTRACT-CR on document attachments")
	h := Start(t)
	d := h.SignIn(deepak, phone360)
	var employee int64
	for _, e := range h.Fx.Employees {
		if e.Code == "EMP-007" {
			employee = e.ID
		}
	}
	d.Open("/doc/" + fixture.Claim + "/new")
	d.Fill("employee_id", strconv.FormatInt(employee, 10))
	d.Fill("claim_date", fixtureDate)
	d.Press(saveContinue)
	d.WaitText("Step 2 of 3")
	claim := d.DocID()
	d.Press(addItem)
	d.WaitText(saveLine)
	d.Fill("expense_type", "TRAVEL")
	d.Fill("amount", "1,250.00")
	d.Press(saveLine)
	d.WaitText("Step 2 of 3")
	d.Press(saveContinue)
	d.WaitText("Step 3 of 3")

	take := chromedp.CSS(`input[type=file][capture]`)
	d.Do("take a photo", chromedp.SetUploadFiles(take, []string{receiptPhoto(t)}, chromedp.NodeReady))
	d.WaitText("receipt.jpg")
	if got := Scalar[int](h, `SELECT count(*) FROM attachment WHERE parent_doctype = ? AND parent_id = ?`, fixture.Claim, claim); got != 1 {
		t.Errorf("attachments = %d, want 1", got)
	}
	d.Press(sendApproval)
	d.WaitText("Pending approval")

	p := h.SignIn(priya, phone390)
	p.Open("/approvals")
	p.WaitText(draft(claim))
	p.Press("Open " + draft(claim))
	p.WaitText("receipt.jpg")
	p.Press("Approve")
	p.WaitText(firstClaim)
	if got := docState(h, "fixture_claim", claim); got != "approved" {
		t.Errorf("claim state = %q, want approved", got)
	}
}
