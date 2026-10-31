package purchase_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// P8 · Print (plan MOD-PO P8; PO spec §9). Every print is fetched through
// the real print handler (print/web) over the real auth.Service, so the print
// right, the read check and the view purchase registers on print.Default for
// purchase.Order are all on the path. Nothing here names a purchase print
// identifier: deleting the print.Register call fails every test.

// p8NoPrint is AUTH's 403 for print on a PO (print/web, CD-73).
const p8NoPrint = "You don't have permission to print Purchase Order. Ask your administrator for a role that allows it.\n"

// p8Get fetches the print page of order id as p.
func p8Get(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) (int, string) {
	t.Helper()
	lh := allmasters.New(p2Kit(env)).Letterhead
	h := printweb.Routes(printweb.Config{DB: env.DB, Auth: env.Auth, Info: env.Engine, Letterhead: lh})
	req := httptest.NewRequest(http.MethodGet, "/print/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// p8Print is p8Get that stops the test unless the page is served.
func p8Print(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) string {
	t.Helper()
	code, body := p8Get(t, env, p, id)
	if code != http.StatusOK {
		t.Fatalf("print order %d as %s: status %d, want 200:\n%s", id, p.Name, code, body)
	}
	return body
}

// p8Contains fails for each of want missing from html.
func p8Contains(t *testing.T, what, html string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(html, w) {
			t.Errorf("%s: print lacks %q:\n%s", what, w, html)
		}
	}
}

// p8Lacks fails for each of absent found in html.
func p8Lacks(t *testing.T, what, html string, absent ...string) {
	t.Helper()
	for _, a := range absent {
		if strings.Contains(html, a) {
			t.Errorf("%s: print has %q, which is never printed:\n%s", what, a, html)
		}
	}
}

// p8RefLines are p2RefLines with each item's own HSN (the builder's default
// line carries MS-ANGLE-50's 7216).
func p8RefLines(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	lines := p2RefLines(t, env)
	lines[0]["hsn_sac"], lines[1]["hsn_sac"] = "7216", "7217"
	return lines
}

// p8Terms creates payment terms code named name (100% at 30 days) and returns
// its id as a form value.
func p8Terms(t *testing.T, env *purchasetest.Env, code, name string) string {
	t.Helper()
	rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := paymentterms.New(p2Kit(env)).Create(t.Context(), env.Admin, paymentterms.Input{Code: code, Name: name, Rows: rows})
	if err != nil {
		t.Fatalf("create terms %s: %v", code, err)
	}
	return strconv.FormatInt(saved.ID, 10)
}

// p8Approve approves pending order id as p at its stored version.
func p8Approve(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) {
	t.Helper()
	if res := p2Must(t, "approve as "+p.Name)(p2Do(t, env, p, id, doc.ActionApprove, "")); res.State != doctype.StateApproved {
		t.Fatalf("order %d after %s's approve: %s, want approved", id, p.Name, res.State)
	}
}

// p8Reference is the approved reference PO (00-fixtures.md): Priya's, SHREE
// to METRO-SITE, the two reference lines, required by 15-11-2026 on Net 30
// days with a vendor quote ref, an earlier PO no., terms and remarks,
// approved by Ravi (Purchase Manager) as PO/26-27/00001.
func p8Reference(t *testing.T, env *purchasetest.Env) int64 {
	t.Helper()
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{
			"required_by":      "15-11-2026",
			"payment_terms_id": p8Terms(t, env, "NET30", "Net 30 days"),
			"vendor_quote_ref": "SST/Q/2026/118",
			"earlier_po_no":    "VIP/PO/1187",
			"terms":            "Delivery within 7 days of PO date.",
			"remarks":          "Check the angle grade before paying.",
		}))
	p8Approve(t, env, env.Ravi, po.ID)
	return po.ID
}

// p8Section is the first <tag class="class">…</tag> element in html, or "".
func p8Section(html, tag, class string) string {
	re := regexp.MustCompile(`(?s)<` + tag + ` class="` + regexp.QuoteMeta(class) + `">.*?</` + tag + `>`)
	return re.FindString(html)
}

// PO-035 (PO spec §9, §18): the approved reference PO's print matches the
// golden po_approved: PUNE's letterhead, the deliver-to block, the four refs,
// each line's net rate and amount with its HSN/SAC, the CGST and SGST rows,
// the totals, the words and Ravi as approver. A discounted line prints its
// net rate and amount, never the price or the discount.
func TestPO_PrintGolden(t *testing.T) {
	env := purchasetest.New(t)
	id := p8Reference(t, env)
	html := p8Print(t, env, env.Priya, id)

	p8Contains(t, "reference PO", html,
		"<title>PO_26-27_00001</title>",
		`<h2 class="legal-name">Vardhan Infra Projects Pvt Ltd</h2>`,
		`<p class="branch">Pune Works</p>`,
		`<p class="gstin">GSTIN: 27AAFCV2449G1Z7</p>`,
		`<h1 class="doc-title">Purchase Order</h1>`,
		`<p class="doc-number">PO/26-27/00001</p>`,
		`<p class="doc-date">Date: 24-10-2026</p>`,
		`<p class="ref">Required by: 15-11-2026</p>`,
		`<p class="ref">Payment terms: Net 30 days</p>`,
		`<p class="ref">Vendor quote ref: SST/Q/2026/118</p>`,
		`<p class="ref">Earlier PO no.: VIP/PO/1187</p>`,
		`<p class="party-name">Shree Steel Traders</p>`,
		`<p class="gstin">GSTIN: 27AABCS1429B1ZU</p>`,
		`<td class="hsn">7216</td>`, `<td class="hsn">7217</td>`,
		`<td class="rate">₹62.50</td>`, `<td class="amount">₹75,000.00</td>`,
		`<td class="rate">₹96.00</td>`, `<td class="amount">₹4,800.00</td>`,
		"<p>Taxable Value ₹79,800.00</p>",
		"<p>CGST 9% ₹7,182.00</p>",
		"<p>SGST 9% ₹7,182.00</p>",
		`<p class="grand-total">Grand Total ₹94,164.00</p>`,
		`<p class="words">Rupees Ninety Four Thousand One Hundred Sixty Four Only</p>`,
		`<p class="place-reason">`+p2SameState+`</p>`,
		"<p>Delivery within 7 days of PO date.</p>",
		`<p class="approved">Approved by Ravi Kulkarni on 24-10-2026 10:30</p>`,
	)
	// The refs print in the spec's order (§9).
	refs := p8Section(html, "div", "refs")
	last := -1
	for _, label := range []string{"Required by:", "Payment terms:", "Vendor quote ref:", "Earlier PO no.:"} {
		i := strings.Index(refs, label)
		if i <= last {
			t.Errorf("refs out of order or missing %q (at %d, previous at %d):\n%s", label, i, last, refs)
		}
		last = i
	}
	if dt := p8Section(html, "section", "deliver-to"); !strings.Contains(dt, "METRO-SITE") {
		t.Errorf("deliver-to block %q doesn't name the delivery site METRO-SITE:\n%s", dt, html)
	}
	p8Lacks(t, "reference PO", html, "IGST", "Check the angle grade", "Reverse charge")
	printtest.Golden(t, "po_approved.golden.html", []byte(strings.ReplaceAll(html, brand.DocumentLogoURL, "/files/1")))

	t.Run("net_rate", func(t *testing.T) {
		l := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "100")
		l["discount_pct"] = "12.5"
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(l))
		html := p8Print(t, env, env.Priya, po.ID)
		p8Contains(t, "discounted line", html, `<td class="rate">₹87.50</td>`, `<td class="amount">₹875.00</td>`,
			"<p>Taxable Value ₹875.00</p>")
		p8Lacks(t, "discounted line", html, "₹100.00", "12.5")
	})
}

// PO-005, print part (PO spec §7, §7.1; CD-254): the print shows
// gstcalc.PlaceReason's text for each of the three cases, from the
// snapshots.
func TestPO_PlaceReasonShown(t *testing.T) {
	env := purchasetest.New(t)
	same := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	diff := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}))
	p2SetCategory(t, env, "SHREE", gstin.SEZ)
	sez := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	for _, c := range []struct {
		what string
		id   int64
		want string
	}{{"same state", same.ID, p2SameState}, {"different states", diff.ID, p2DiffState}, {"SEZ supplier", sez.ID, p2SEZ}} {
		html := p8Print(t, env, env.Priya, c.id)
		if got := p8Section(html, "p", "place-reason"); got != `<p class="place-reason">`+c.want+`</p>` {
			t.Errorf("%s: place reason %q, want %q", c.what, got, c.want)
		}
	}
}

// PO-028 (PO spec §7, §9, §18): with reverse charge on, the print carries
// "Reverse charge applies. GST of ₹{x} is payable by us, not to the vendor."
// with the PO's tax as {x}; with it off the note is absent.
func TestPO_ReverseChargePrint(t *testing.T) {
	env := purchasetest.New(t)
	on := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"reverse_charge": "true"}))
	if got := p2Load(t, env, on.ID).Header["reverse_charge"]; got != true {
		t.Fatalf("reverse_charge stored as %v, want true", got)
	}
	html := p8Print(t, env, env.Priya, on.ID)
	p8Contains(t, "reverse charge on", html,
		`<p class="reverse-charge">Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor.</p>`)

	off := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	html = p8Print(t, env, env.Priya, off.ID)
	p8Contains(t, "reverse charge off", html, `<h1 class="doc-title">Purchase Order</h1>`)
	p8Lacks(t, "reverse charge off", html, "Reverse charge", `class="reverse-charge"`)
}

// PO-036 (PO spec §9, §10, §18; R3): printing needs print and visibility.
// Priya (Purchase User, print all) and Ravi (Purchase Manager, print all) get
// the reference PO. Suresh (Stores: read all, no print) reads it but gets
// AUTH's 403. Priya can't see Admin's TECH-SITE order (PRJ-TECHPARK is
// outside her restriction) and gets the same 404 as a missing record.
func TestPO_PrintRights(t *testing.T) {
	env := purchasetest.New(t)
	id := p8Reference(t, env)
	for _, p := range []auth.Principal{env.Priya, env.Ravi} {
		p8Contains(t, p.Name, p8Print(t, env, p, id), `<p class="doc-number">PO/26-27/00001</p>`)
	}

	if code, body := p8Get(t, env, env.Suresh, id); code != http.StatusForbidden || body != p8NoPrint {
		t.Errorf("suresh: %d %q, want 403 %q", code, body, p8NoPrint)
	}
	// Suresh does read it: the 403 is for print, not read.
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		info, err := env.Engine.Info(t.Context(), tx, purchase.DocType, id)
		if err != nil {
			return err
		}
		return env.Auth.CanTx(t.Context(), tx, env.Suresh, purchase.DocType, auth.RightRead, &auth.RecordRef{
			DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
		})
	}); err != nil {
		t.Errorf("suresh read on the reference PO: %v, want nil", err)
	}

	tech := p2Must(t, "admin's TECH-SITE order")(p2Create(t, env, env.Admin,
		p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), p8RefLines(t, env)))
	p8Contains(t, "admin", p8Print(t, env, env.Admin, tech.ID), `<h1 class="doc-title">Purchase Order</h1>`)
	missingCode, missingBody := p8Get(t, env, env.Priya, tech.ID+1000)
	if missingCode != http.StatusNotFound {
		t.Fatalf("missing order: status %d, want 404", missingCode)
	}
	if code, body := p8Get(t, env, env.Priya, tech.ID); code != missingCode || body != missingBody {
		t.Errorf("priya on the TECH-SITE order: %d %q, want the missing record's %d %q", code, body, missingCode, missingBody)
	}
}

// PO-046, print part (PO spec §12, §18; plan P3 note): with no approval rule
// a back-dated PO is approved on submit, and its print names the submitter
// as approver from the approval history's approved_no_rule entry.
// TestPO_BackEntry (P3) owns the rest of PO-046.
func TestPO_BackEntryPrint(t *testing.T) {
	env := purchasetest.New(t, purchasetest.WithNoRules())
	po := purchasetest.NewOrder(t, env, purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"order_date": "15-04-2026"}))
	if po.State != doctype.StateApproved || po.Number != "PO/26-27/00001" {
		t.Fatalf("back entry: %s %q, want approved PO/26-27/00001 on submit", po.State, po.Number)
	}
	html := p8Print(t, env, env.Ravi, po.ID)
	p8Contains(t, "back entry", html,
		`<p class="doc-date">Date: 15-04-2026</p>`,
		`<p class="approved">Approved by Priya Shah on 24-10-2026 10:30</p>`)
}

// PO spec §9, plan P8: comments, the Sent record, Expected on and the rest
// rows (with the comment SetRestNotComing posts) never print, nor do the
// internal remarks (§3.1).
func TestPO_PrintLeavesOutInternal(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	id := p8Reference(t, env)

	if _, err := env.Comments.AddComment(ctx, env.Priya, purchase.DocType, id, "Call Ramesh before dispatch."); err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if err := env.Service.MarkSent(ctx, env.Priya, id, 0, purchase.SentInput{
		SentOn: types.Today(env.Clock), SentVia: "email", Note: "Mailed to the Mumbai office.",
	}); err != nil {
		t.Fatalf("MarkSent: %v", err)
	}
	on, err := purchase.ParseExpectedOn("20-11-2026")
	if err != nil {
		t.Fatal(err)
	}
	if err := env.Service.SetExpected(ctx, env.Priya, id, 0, on, "Vendor moved the dispatch."); err != nil {
		t.Fatalf("SetExpected: %v", err)
	}
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		return purchase.SetRestNotComing(ctx, tx, id, 9001, env.Suresh.ID, true)
	}); err != nil {
		t.Fatalf("SetRestNotComing: %v", err)
	}

	html := p8Print(t, env, env.Priya, id)
	p8Contains(t, "with internal records", html, `<p class="doc-number">PO/26-27/00001</p>`, `<p class="grand-total">Grand Total ₹94,164.00</p>`)
	p8Lacks(t, "with internal records", html,
		"Call Ramesh", "Mailed to the Mumbai office", "Marked as sent", "Sent on",
		"Expected on", "20-11-2026", "Vendor moved the dispatch", "won't come",
		"Check the angle grade", "Remarks")
}
