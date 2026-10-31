package purchase_test

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// P8 fix round 1: each test pins a part of the print that the review's
// mutation pass found unchecked (print.sql's filters and joins, the IGST
// rows, the branch letterhead, the rounded total, blank refs and the place
// reason guard).

// p8TaxRows are the CGST, SGST and IGST rows of html's totals, in order.
func p8TaxRows(html string) []string {
	var rows []string
	for _, m := range regexp.MustCompile(`<p>((?:CGST|SGST|IGST) [^<]*)</p>`).FindAllStringSubmatch(p8Section(html, "div", "totals"), -1) {
		rows = append(rows, m[1])
	}
	return rows
}

// PO spec §9: each PO's print has only its own lines. PO A has the two
// reference lines, PO B one BOLT-M12 line; neither print shows the other's.
func TestPO_PrintOwnLinesOnly(t *testing.T) {
	env := purchasetest.New(t)
	a := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	b := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(
		p2Line(t, env, "BOLT-M12", "BOX", "1", "500")))

	htmlA := p8Print(t, env, env.Priya, a.ID)
	p8Contains(t, "PO A", htmlA, "MS-ANGLE-50", "WIRE-18G")
	p8Lacks(t, "PO A", htmlA, "BOLT-M12")
	if n := strings.Count(htmlA, `<td class="sr">`); n != 2 {
		t.Errorf("PO A: %d line rows, want 2:\n%s", n, htmlA)
	}

	htmlB := p8Print(t, env, env.Priya, b.ID)
	p8Contains(t, "PO B", htmlB, "BOLT-M12")
	p8Lacks(t, "PO B", htmlB, "MS-ANGLE-50", "WIRE-18G")
	if n := strings.Count(htmlB, `<td class="sr">`); n != 1 {
		t.Errorf("PO B: %d line rows, want 1:\n%s", n, htmlB)
	}
}

// PO spec §9, PO-046 print part: each PO names its own approver. Ravi
// approves PO A, then Meena (Director, a higher rule's role) approves PO B;
// A's print still names Ravi and B's names Meena.
func TestPO_PrintOwnApprover(t *testing.T) {
	env := purchasetest.New(t)
	a := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p8RefLines(t, env)...))
	b := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p8RefLines(t, env)...))
	p8Approve(t, env, env.Ravi, a.ID)
	p8Approve(t, env, env.Meena, b.ID)

	htmlA := p8Print(t, env, env.Priya, a.ID)
	p8Contains(t, "PO A", htmlA, `<p class="approved">Approved by Ravi Kulkarni on 24-10-2026 10:30</p>`)
	p8Lacks(t, "PO A", htmlA, "Meena")
	htmlB := p8Print(t, env, env.Priya, b.ID)
	p8Contains(t, "PO B", htmlB, `<p class="approved">Approved by Meena Iyer on 24-10-2026 10:30</p>`)
	p8Lacks(t, "PO B", htmlB, "Ravi")
}

// PO spec §9: the approver comes from the PO's own approval history, never
// another DocType's. An approved row for purchase.Request with the draft
// PO's id must not print. It is a plain SQL insert, as testkit.Seed adds
// fixtures, because no approval API can write it here: purchase.Order is the
// only Approvable DocType in this test binary (purchase.Request isn't, and
// writes no approval history).
func TestPO_PrintApproverOwnDocType(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...))
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(),
			`INSERT INTO approval_history (doctype, doc_id, action, user_id, user_name, at) VALUES (?, ?, 'approved', ?, ?, ?)`,
			purchase.RequestDocType, po.ID, env.Suresh.ID, env.Suresh.Name, types.TimestampOf(env.Clock.Now()).String())
		return err
	}); err != nil {
		t.Fatalf("insert the request's approval row: %v", err)
	}
	html := p8Print(t, env, env.Priya, po.ID)
	p8Lacks(t, "draft PO beside an approved request", html, `class="approved"`, "Approved by", env.Suresh.Name)
}

// PO spec §9: only an approval names an approver. A pending PO (submitted
// only) and a rejected one (submitted, then rejected by Ravi) print no
// approved line.
func TestPO_PrintNoApproverUnlessApproved(t *testing.T) {
	env := purchasetest.New(t)
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p8RefLines(t, env)...))
	rejected := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p8RefLines(t, env)...))
	if res := p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, rejected.ID, doc.ActionReject, "Price too high")); res.State != doctype.StateRejected {
		t.Fatalf("order %d after reject: %s, want rejected", rejected.ID, res.State)
	}
	for _, c := range []struct {
		what string
		id   int64
	}{{"pending", pending.ID}, {"rejected", rejected.ID}} {
		html := p8Print(t, env, env.Priya, c.id)
		p8Contains(t, c.what, html, `<h1 class="doc-title">Purchase Order`)
		p8Lacks(t, c.what, html, `class="approved"`, "Approved by")
	}
}

// AD-13 ruling (P8 round 2): the amends line names the PO this one amends,
// not the first in the chain. The amendment PO/26-27/00001-1 is cancelled
// and amended again; PO/26-27/00001-2 prints "Amends PO/26-27/00001-1".
func TestPO_PrintAmendsAmendment(t *testing.T) {
	env := purchasetest.New(t)
	id := p8Reference(t, env)
	for range 2 {
		p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, id, doc.ActionCancel, "Wrong delivery date"))
		res := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, id, doc.ActionAmend, ""))
		if res.ID == id || res.State != doctype.StateDraft {
			t.Fatalf("amend of %d = %+v, want a new draft", id, res)
		}
		p2Must(t, "Priya submits the amendment")(p2Do(t, env, env.Priya, res.ID, doc.ActionSubmit, ""))
		p8Approve(t, env, env.Ravi, res.ID)
		id = res.ID
	}
	html := p8Print(t, env, env.Priya, id)
	p8Contains(t, "second amendment", html,
		`<p class="doc-number">PO/26-27/00001-2</p>`,
		`<p class="amends">Amends PO/26-27/00001-1</p>`)
	if n := strings.Count(html, `class="amends"`); n != 1 {
		t.Errorf("second amendment: %d amends lines, want 1:\n%s", n, html)
	}
}

// PO spec §7, §9: an inter-state PO (BHARAT, Karnataka, to METRO-SITE in
// Maharashtra) prints one IGST row at the full rate and no CGST or SGST rows.
func TestPO_PrintIGST(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT")}))
	html := p8Print(t, env, env.Priya, po.ID)
	if got, want := strings.Join(p8TaxRows(html), "|"), "IGST 18% ₹14,364.00"; got != want {
		t.Errorf("inter-state tax rows %q, want %q:\n%s", got, want, html)
	}
	p8Contains(t, "inter-state", html, "<p>Taxable Value ₹79,800.00</p>",
		`<p class="grand-total">Grand Total ₹94,164.00</p>`)
	p8Lacks(t, "inter-state", html, "CGST", "SGST")
}

// PO-028 (PO spec §7, §9): on an inter-state PO with reverse charge on, the
// note's GST is the IGST.
func TestPO_PrintReverseChargeIGST(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p8RefLines(t, env)...),
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", "BHARAT"), "reverse_charge": "true"}))
	html := p8Print(t, env, env.Priya, po.ID)
	p8Contains(t, "inter-state reverse charge", html,
		`<p class="reverse-charge">Reverse charge applies. GST of ₹14,364.00 is payable by us, not to the vendor.</p>`)
}

// PO spec §9: the print is on the delivery site's branch letterhead.
// Admin's TECH-SITE order prints BLR's (Bengaluru Site Office), not PUNE's.
func TestPO_PrintBranchLetterhead(t *testing.T) {
	env := purchasetest.New(t)
	tech := p2Must(t, "admin's TECH-SITE order")(p2Create(t, env, env.Admin,
		p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK"), p8RefLines(t, env)))
	html := p8Print(t, env, env.Admin, tech.ID)
	p8Contains(t, "TECH-SITE order", html,
		`<p class="branch">Bengaluru Site Office</p>`,
		`<p class="gstin">GSTIN: 29AAFCV2449G1Z3</p>`)
	p8Lacks(t, "TECH-SITE order", html, "Pune Works", "27AAFCV2449G1Z7")
}

// PO spec §9 (FILE-082): a grand total with paise prints the Rounded Total
// row, and the words follow the rounded figure. One kg at ₹10 with 18% GST
// is ₹11.80, rounded to ₹12.00.
func TestPO_PrintRoundedTotal(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(
		p2Line(t, env, "MS-ANGLE-50", "KGS", "1", "10")))
	html := p8Print(t, env, env.Priya, po.ID)
	p8Contains(t, "paise total", html,
		`<p class="grand-total">Grand Total ₹11.80</p>`,
		`<p class="rounded-total">Rounded Total ₹12.00</p>`,
		`<p class="words">Rupees Twelve Only</p>`)
	p8Lacks(t, "paise total", html, "Eleven", "Paise")
}

// PO spec §9 (FILE-064): a ref left blank prints no label. The default PO
// has no Required by, vendor quote ref or earlier PO no.
func TestPO_PrintBlankRefs(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	html := p8Print(t, env, env.Priya, po.ID)
	p8Contains(t, "blank refs", html, `<h1 class="doc-title">Purchase Order`)
	p8Lacks(t, "blank refs", html, "Required by", "Vendor quote ref", "Earlier PO no.")
}

// PO spec §7.1, §9: a draft with no vendor prints without a place reason
// and is still served. A draft with no delivery site has no branch, so no
// letterhead: AD-13 ruled its print is refused with 422 and p8NoSite, never
// a 404 (the record exists) or a 500.
func TestPO_PrintNoPlaceReasonWithoutParties(t *testing.T) {
	env := purchasetest.New(t)
	t.Run("no_vendor", func(t *testing.T) {
		res := p2Must(t, "no vendor")(p2Create(t, env, env.Admin, p2Header(t, env, "", "METRO-SITE", "PRJ-METRO"), p8RefLines(t, env)))
		code, body := p8Get(t, env, env.Admin, res.ID)
		if code != http.StatusOK {
			t.Fatalf("no vendor: status %d, want 200:\n%s", code, body)
		}
		p8Contains(t, "no vendor", body, `<h1 class="doc-title">Purchase Order`)
		p8Lacks(t, "no vendor", body, `class="place-reason"`, "Intra-state", "Inter-state")
		// No vendor means no address snapshot, so no address lines.
		vendor := p8Section(body, "section", "party")
		p8Contains(t, "no vendor's party block", vendor, "<h3>Vendor</h3>")
		p8Lacks(t, "no vendor's party block", vendor, `<p class="address">`)
	})
	t.Run("no_site", func(t *testing.T) {
		res := p2Must(t, "no site")(p2Create(t, env, env.Admin, p2Header(t, env, "SHREE", "", "PRJ-METRO"), p8RefLines(t, env)))
		code, body := p8Get(t, env, env.Admin, res.ID)
		if code != http.StatusUnprocessableEntity {
			t.Fatalf("no site: status %d, want 422:\n%s", code, body)
		}
		if !strings.Contains(html.UnescapeString(body), p8NoSite) {
			t.Errorf("no site: body %q lacks %q", body, p8NoSite)
		}
	})
}

// p8NoSite is AD-13's refusal for printing a PO with no deliver-to site.
const p8NoSite = "Pick the deliver-to site before printing this order."

// PO spec §9: a line's own description prints under its item. The
// description differs from the item name, so it prints whatever rule drops
// a description that repeats the name.
func TestPO_PrintDescription(t *testing.T) {
	env := purchasetest.New(t)
	l := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "100")
	l["description"] = "Cut to 6 m lengths"
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(l))
	p8Contains(t, "described line", p8Print(t, env, env.Priya, po.ID), `<div class="desc">Cut to 6 m lengths</div>`)
}

// PO spec §9 (FILE-063): an unregistered vendor's address has no GSTIN line,
// so its last line (the state) prints, and the block says GSTIN:
// Unregistered. LOCAL (Ganesh Hardware) is the unregistered Pune vendor.
func TestPO_PrintUnregisteredVendor(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "LOCAL vendor")(p2Create(t, env, env.Admin, p2Header(t, env, "LOCAL", "METRO-SITE", "PRJ-METRO"), p8RefLines(t, env)))
	vendor := p8Section(p8Print(t, env, env.Admin, res.ID), "section", "party")
	p8Contains(t, "unregistered vendor's party block", vendor,
		"Ganesh Hardware",
		`<p class="address">Maharashtra (27)</p><p class="gstin">GSTIN: Unregistered</p>`)
}
