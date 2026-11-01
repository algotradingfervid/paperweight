//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// GRN samples (MOD-GRN S5): the long PO's three lines of 1,000 at a price
// whose grand total, with the long tax code's 28%, is just under the
// largest amount, as purchase's; each receipt takes every line in full,
// line 1 with 10 rejected for the longest reason.
const (
	grnLineQty    = "1000.000"
	grnLinePrice  = "2604166.0000"
	grnRejected   = "10.000"
	grnReasonLen  = 200 // reject_reason's MaxLen
	grnChallanLen = 40  // challan_no's MaxLen
	grnShortLen   = 20  // vehicle_no's and eway_bill_no's MaxLen
	grnBillDate   = "24-10-2026"
	grnComboPOs   = 50 // step 1's combobox, from minPOSearch orders (grnweb)
	// grnFullPOs are fully received orders at METRO-SITE: with them Suresh's
	// step 1 lists more than six orders at ?all=1, a Select, and his
	// pending ones stay radios.
	grnFullPOs = 3
	// grnNoPOs holds Stores at PUNE-WH, where no order is delivered: step 1
	// with nothing to choose.
	grnNoPOs = "stores.pune@vardhan.test"
)

// grnSamples are the sweep's goods receipts: one per state docscreen.Screens
// visits, and the others the UX doc's screens need (§5.2 to §5.9).
type grnSamples struct {
	ids        docscreen.SampleIDs
	po         int64 // an approved order with pending, for ?po=
	service    int64 // a draft against a services-only order
	taxInvoice int64 // approved, received on a tax invoice: the bill offer
	bill       int64 // approved with bill details
	dup        int64 // approved with bill's invoice no.: both warn
	closedPO   int64 // approved, its order short-closed since
	billGone   int64 // cancelled with bill details, amended
	amendment  int64 // billGone's approved amendment: bill copied
}

// grnSamples seeds goods receipts by Suresh (Stores, METRO-SITE) against
// approved POs the admin raised on METRO-SITE / PRJ-METRO from the long
// vendor and item, with the longest delivery values (UX doc §4). A GRN
// can't be closed (CD-277), so there is no closed one. Approved and
// cancelled ones come first; then an approval rule for the Administrator
// (the only role holding approve on a GRN, CD-320 (2)) makes the pending
// and rejected ones. The fixture DocTypes share the PO/ and GR/ prefixes,
// and numbers are unique across DocTypes, so the real series start at
// 50001 (CD-295). The admin also raises grnComboPOs orders on TECH-SITE,
// so his step 1 is the combobox, and grnFullPOs received in full; and
// grnNoPOs is created.
func (h *Harness) grnSamples(long testkit.LongRows) grnSamples {
	h.t.Helper()
	p := h.Principal(admin)
	ctx := context.Background()
	namer := naming.New(h.App.DB(), h.Clock, h.App.Auth(), audit.Default)
	for dt, prefix := range map[string]string{purchase.DocType: "PO/26-27/", goodsreceipt.DocType: "GR/26-27/"} {
		if err := namer.SetCounter(ctx, p, dt, prefix, 50000); err != nil {
			h.t.Fatalf("set %s's %s counter: %v", dt, prefix, err)
		}
	}
	code := func(table, c string) string {
		return strconv.FormatInt(Scalar[int64](h, `SELECT id FROM `+table+` WHERE code = ?`, c), 10) //nolint:gosec // G202: fixed table names
	}
	id := func(v int64) string { return strconv.FormatInt(v, 10) }
	metro, prjMetro := code("site", "METRO-SITE"), code("project", "PRJ-METRO")
	po := func(site, project string, lines []map[string]string) (int64, []string) {
		in := doc.ActionInput{Values: map[string]string{
			"vendor_id": id(long.Party), "site_id": site, "project_id": project, "order_date": plainDate,
		}, Lines: lines}
		r := h.Do(admin, purchase.DocType, 0, doctype.ActionSave, in)
		r = h.Do(admin, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
		return r.ID, h.lineIDs(r.ID)
	}
	goods := func(n int, qty, price string) []map[string]string {
		var out []map[string]string
		for range n {
			out = append(out, map[string]string{"item_id": id(long.Item), "uom_id": id(h.Fx.Units[0].ID), "qty": qty, "price": price, "description": uitest.LongName100})
		}
		return out
	}
	small, smallLines := po(metro, prjMetro, goods(longLines, grnLineQty, grnLinePrice))
	big, bigLines := po(metro, prjMetro, goods(manyLines, "1.000", "1"))
	svc, svcLines := po(metro, prjMetro, []map[string]string{{"item_id": code("item", "SVC-CRANE"), "uom_id": code("uom", "HRS"), "qty": "10", "price": "1500"}})
	closing, closingLines := po(metro, prjMetro, goods(1, grnLineQty, "1"))
	techSite, techPrj := code("site", "TECH-SITE"), code("project", "PRJ-TECHPARK")
	for range grnComboPOs {
		po(techSite, techPrj, goods(1, "1.000", "1"))
	}

	grn := func(poID int64, lines []string, qty, kind string) doc.Result {
		in := doc.ActionInput{Values: map[string]string{
			"purchase_order_id": id(poID), "site_id": metro, "project_id": prjMetro, "receipt_date": plainDate,
			"remarks": uitest.LongError[:longRemarks],
		}}
		if kind != "" {
			for k, v := range map[string]string{
				"challan_kind": kind, "challan_no": uitest.LongCode60[:grnChallanLen], "challan_date": plainDate,
				"vehicle_no": uitest.LongCode60[:grnShortLen], "transporter": uitest.LongName100, "eway_bill_no": uitest.LongCode60[:grnShortLen],
			} {
				in.Values[k] = v
			}
		}
		for i, l := range lines {
			line := map[string]string{"po_line_id": l, "received_qty": qty}
			if i == 0 && qty == grnLineQty {
				line["rejected_qty"], line["reject_reason"] = grnRejected, uitest.LongError[:grnReasonLen]
			}
			in.Lines = append(in.Lines, line)
		}
		return h.Do(suresh, goodsreceipt.DocType, 0, doctype.ActionSave, in)
	}
	const challan, invoice = "Delivery challan", "Tax invoice"
	approve := func(r doc.Result) doc.Result {
		return h.Do(suresh, goodsreceipt.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	}
	submit := func() doc.Result { return approve(grn(small, smallLines, grnLineQty, challan)) }
	cancel := func(r doc.Result) doc.Result {
		return h.Do(ravi, goodsreceipt.DocType, r.ID, doctype.ActionCancel, doc.ActionInput{Version: r.Version, Reason: uitest.LongError})
	}

	s := grnSamples{po: big}
	s.ids = docscreen.SampleIDs{Draft: grn(small, smallLines, grnLineQty, challan).ID, Lines500: grn(big, bigLines, "1.000", challan).ID}
	s.ids.Approved = submit().ID
	s.ids.Cancelled = cancel(submit()).ID
	s.service = grn(svc, svcLines, "10", "").ID
	s.taxInvoice = approve(grn(small, smallLines, grnLineQty, invoice)).ID

	grns := goodsreceipt.New(goodsreceipt.Deps{DB: h.App.DB(), Clock: h.Clock, Auth: h.App.Auth(), Audit: audit.Default, Files: h.App.Files()})
	bill := func(r doc.Result, no string) {
		in := goodsreceipt.BillInput{InvoiceNo: no, InvoiceDate: grnBillDate, InvoiceAmount: maxAmount}
		if err := grns.SaveBill(ctx, h.Principal(suresh), r.ID, 0, in); err != nil {
			h.t.Fatalf("bill details on GRN %d: %v", r.ID, err)
		}
	}
	r := submit()
	bill(r, uitest.LongCode60[:grnChallanLen])
	s.bill = r.ID
	r = submit()
	bill(r, uitest.LongCode60[:grnChallanLen])
	s.dup = r.ID

	r = submit()
	bill(r, uitest.LongCode60[grnChallanLen:])
	r = cancel(r) // bill details have their own version (grn_bill)
	s.billGone = r.ID
	s.amendment = approve(h.Do(suresh, goodsreceipt.DocType, r.ID, doctype.ActionAmend, doc.ActionInput{Version: r.Version})).ID

	s.closedPO = approve(grn(closing, closingLines, "500.000", challan)).ID
	for range grnFullPOs {
		full, fullLines := po(metro, prjMetro, goods(1, "1.000", "1"))
		approve(grn(full, fullLines, "1.000", challan))
	}
	v := Scalar[int64](h, `SELECT version FROM purchase_order WHERE id = ?`, closing)
	h.Do(priya, purchase.DocType, closing, doctype.ActionClose, doc.ActionInput{Version: int(v), Reason: uitest.LongError})

	if _, err := h.App.Approvals().CreateRule(auth.WithPrincipal(ctx, p), p, approval.RuleInput{
		DocType: goodsreceipt.DocType, ApproverType: approval.ApproverRole, Role: auth.RoleAdministrator,
	}); err != nil {
		h.t.Fatalf("approval rule for %s: %v", goodsreceipt.DocType, err)
	}
	s.ids.Pending = submit().ID
	r = submit()
	s.ids.Rejected = h.Do(admin, goodsreceipt.DocType, r.ID, doctype.ActionReject, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID

	h.storesWithNoOrders()
	return s
}

// storesWithNoOrders creates grnNoPOs, a Stores user restricted to
// PUNE-WH, with the fixture password.
func (h *Harness) storesWithNoOrders() {
	h.t.Helper()
	ctx := context.Background()
	p := h.Principal(admin)
	u, _, err := h.App.Auth().CreateUser(ctx, p, auth.NewUser{Email: grnNoPOs, Name: "Sunil Pawar", Password: password, Roles: []string{"Stores"}})
	if err != nil {
		h.t.Fatalf("create %s: %v", grnNoPOs, err)
	}
	pune := Scalar[int64](h, `SELECT id FROM site WHERE code = 'PUNE-WH'`)
	if err := h.App.Auth().ChangeRestrictions(ctx, p, u, auth.DimSite, []int64{pune}, nil, nil); err != nil {
		h.t.Fatalf("restrict %s: %v", grnNoPOs, err)
	}
	if err := h.App.DB().Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE users SET must_change_password = 0 WHERE id = ?`, u)
		return err
	}); err != nil {
		h.t.Fatal(err)
	}
}

// lineIDs are PO id's line ids in line order, as form values.
func (h *Harness) lineIDs(id int64) []string {
	h.t.Helper()
	rows, err := h.App.DB().SQL().QueryContext(context.Background(), `SELECT id FROM purchase_order_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		h.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var l int64
		if err := rows.Scan(&l); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, strconv.FormatInt(l, 10))
	}
	if err := rows.Err(); err != nil {
		h.t.Fatal(err)
	}
	return out
}

// grnScreens are the GRN's docscreen pages: as Suresh, who receives; the
// reject page as the Administrator, the rule's approver; the cancel page as
// Ravi, the Purchase Manager, who holds cancel (GRN spec §10). The app's
// Deps leave out the line screens GRN doesn't have (CD-329). Suresh's new
// receipt waits for its radios (UX doc §5.2). Then grnFormScreens.
func (h *Harness) grnScreens(s grnSamples) []screens.Screen {
	var out []screens.Screen
	for _, sc := range docscreen.Screens(goodsreceipt.DocType, s.ids, h.App.DocDeps()) {
		switch {
		case strings.HasSuffix(sc.Name, " reject"):
			sc.As = admin
		case strings.HasSuffix(sc.Name, " cancel"):
			sc.As = ravi
		case strings.HasSuffix(sc.Name, " new"):
			sc.As, sc.Setup = suresh, visible(`input[type=radio][name=purchase_order_id]`)
		default:
			sc.As = suresh
		}
		out = append(out, sc)
	}
	return append(out, grnFormScreens(s)...)
}

// grnFormScreens are MOD-GRN S5's GRN screens beyond docscreen.Screens'
// (UX doc §5.2 to §5.9). Step 1's choice as a Select (Suresh with fully
// received orders shown), the combobox (the admin, with grnComboPOs more)
// and its rows, with nothing to choose (grnNoPOs) and its 422, and with
// ?po=; step 2 of a services-only receipt; steps 2 and 3 with their 422
// and 409; the document page as a reader, with bill details, a duplicate
// invoice, the tax invoice offer, a cancelled one with bill details and
// its amendment; the bill card's form at ?bill=edit, its 422 and 409, and
// as Priya, who may only read it; and the cancel page's two notes. Each
// Setup's wait checks the screen shows what its name says.
func grnFormScreens(s grnSamples) []screens.Screen {
	const page, step, sub = "/doc/{doctype}/{id}", "/doc/{doctype}/{id}/edit/{step}", "/doc/{doctype}/{id}/{action}"
	const newRoute, newURL = "/doc/{doctype}/new", "/doc/" + goodsreceipt.DocType + "/new"
	at := func(id int64) string { return DocPath(goodsreceipt.DocType, id) }
	const wizard = `document.getElementById("wizard-form")`
	const billForm = `document.querySelector("#bill form")`
	const conflict, invalid = "#wizard-conflict", "#error-summary-title"
	draft := at(s.ids.Draft)
	return []screens.Screen{
		{Name: "grn choose select", Route: newRoute, URL: newURL + "?all=1", As: suresh, Setup: visible(`select[name=purchase_order_id]`)},
		{Name: "grn choose combobox", Route: newRoute, URL: newURL, As: admin, Setup: visible(`input[role=combobox]`)},
		{Name: "grn choose combobox rows", Route: newRoute, URL: newURL, As: admin, Setup: grnSearch},
		{Name: "grn choose none", Route: newRoute, URL: newURL, As: grnNoPOs, Setup: visible(`//p[contains(., "No approved orders are waiting for delivery.")]`)},
		{Name: "grn choose none 422", Route: newRoute, URL: newURL, As: grnNoPOs, Setup: expensePost(wizard, "", invalid)},
		{Name: "grn choose preselected", Route: newRoute, URL: newURL + "?po=" + strconv.FormatInt(s.po, 10), As: suresh,
			Setup: visible(`input[type=radio][name=purchase_order_id][value="` + strconv.FormatInt(s.po, 10) + `"]:checked`)},
		{Name: "grn delivery service only", Route: step, URL: at(s.service) + "/edit/delivery", As: suresh},
		{Name: "grn delivery 422", Route: step, URL: draft + "/edit/delivery", As: suresh, Setup: expensePost(wizard, `f.elements.challan_date.value = "x";`, invalid)},
		{Name: "grn delivery 409", Route: step, URL: draft + "/edit/delivery", As: suresh, Setup: expensePost(wizard, staleVersion, conflict)},
		{Name: "grn items 422", Route: step, URL: draft + "/edit/items", As: suresh,
			Setup: expensePost(wizard, `f.querySelectorAll("input[inputmode=numeric], input[inputmode=decimal]").forEach((e) => { e.value = "x"; });`, invalid)},
		{Name: "grn items 409", Route: step, URL: draft + "/edit/items", As: suresh, Setup: expensePost(wizard, staleVersion, conflict)},

		{Name: "grn draft reader", Route: page, URL: draft, As: ravi},
		{Name: "grn with bill", Route: page, URL: at(s.bill), As: suresh, Setup: visible(`//*[@id="bill"]//*[contains(., "is also on")]`)},
		{Name: "grn duplicate invoice", Route: page, URL: at(s.dup), As: suresh},
		{Name: "grn bill read only", Route: page, URL: at(s.bill), As: priya},
		{Name: "grn tax invoice offer", Route: page, URL: at(s.taxInvoice), As: suresh},
		{Name: "grn cancelled with bill", Route: page, URL: at(s.billGone), As: suresh},
		{Name: "grn amendment", Route: page, URL: at(s.amendment), As: suresh, Setup: visible(`//*[@id="bill"]//p[starts-with(., "Copied from")]`)},
		{Name: "grn bill edit", Route: page, URL: at(s.bill) + "?bill=edit", As: suresh, Setup: visible(`#bill form`)},
		{Name: "grn bill 422", Route: page, URL: at(s.ids.Approved), As: suresh,
			Setup: expensePost(billForm, `f.elements.invoice_no.value = ""; f.elements.invoice_date.value = "x";`, `#bill `+invalid)},
		{Name: "grn bill 409", Route: page, URL: at(s.bill) + "?bill=edit", As: suresh,
			Setup: expensePost(billForm, staleVersion, `//*[@id="bill"]//a[contains(., "Reload Bill details")]`)},

		{Name: "grn cancel with bill", Route: sub, URL: at(s.bill) + "/cancel", As: ravi, Setup: visible(`//*[contains(., "Correct your accounts too.")]`)},
		{Name: "grn cancel closed PO", Route: sub, URL: at(s.closedPO) + "/cancel", As: ravi, Setup: visible(`//*[contains(., "only an amendment of this receipt can receive against it again.")]`)},
	}
}

// visible is a Setup that waits until wait (a CSS selector or an XPath) is
// visible.
func visible(wait string) func(context.Context) error {
	return func(ctx context.Context) error { return chromedp.Do(ctx, chromedp.WaitVisible(wait)) }
}

// grnSearch types into step 1's combobox and waits for its rows.
func grnSearch(ctx context.Context) error {
	return chromedp.Do(ctx,
		chromedp.SendKeys(chromedp.CSS(`input[role=combobox]`), "PO"),
		chromedp.WaitVisible(chromedp.CSS(`[role=listbox] [role=option]`)))
}

// grnReportScreens are MOD-GRN S4's reports entry, Bills to enter and
// Received items with their filter pages, as Priya (Purchase User: read
// and export on the GRN), and each report with its chip filter on.
func grnReportScreens() []screens.Screen {
	out := []screens.Screen{{Name: "grn reports", Route: "/reports/grn", URL: "/reports/grn", As: priya}}
	for _, r := range []string{"bills", "items"} {
		base := "/reports/grn/" + r
		out = append(out,
			screens.Screen{Name: "grn report " + r, Route: base, URL: base, As: priya},
			screens.Screen{Name: "grn report " + r + " filter", Route: base + "/filter", URL: base + "/filter", As: priya})
	}
	return append(out,
		screens.Screen{Name: "grn report bills no bill", Route: "/reports/grn/bills", URL: "/reports/grn/bills?nobill=1", As: priya},
		screens.Screen{Name: "grn report items rejected", Route: "/reports/grn/items", URL: "/reports/grn/items?rejected=1", As: priya})
}
