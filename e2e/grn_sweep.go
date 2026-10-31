//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/screens"
)

// grnSamples seeds goods receipts by Suresh (Stores, METRO-SITE) in every
// state docscreen.Screens visits (CD-298), against approved POs the admin
// raised on METRO-SITE / PRJ-METRO. A GRN can't be closed (CD-277), so
// there is no closed one. Approved and cancelled come first; then an
// approval rule for the Administrator (the only role holding approve on a
// GRN, CD-320 (2)) makes the pending and rejected ones. The fixture
// DocTypes share the PO/ and GR/ prefixes, and numbers are unique across
// DocTypes, so the real series start at 50001 (CD-295).
func (h *Harness) grnSamples() docscreen.SampleIDs {
	h.t.Helper()
	p := h.Principal(admin)
	namer := naming.New(h.App.DB(), h.Clock, h.App.Auth(), audit.Default)
	for dt, prefix := range map[string]string{purchase.DocType: "PO/26-27/", goodsreceipt.DocType: "GR/26-27/"} {
		if err := namer.SetCounter(context.Background(), p, dt, prefix, 50000); err != nil {
			h.t.Fatalf("set %s's %s counter: %v", dt, prefix, err)
		}
	}
	code := func(table, c string) string {
		return strconv.FormatInt(Scalar[int64](h, `SELECT id FROM `+table+` WHERE code = ?`, c), 10) //nolint:gosec // G202: fixed table names
	}
	po := func(lines int) (int64, []string) {
		in := doc.ActionInput{Values: map[string]string{
			"vendor_id": code("party", "SHREE"), "site_id": code("site", "METRO-SITE"), "project_id": code("project", "PRJ-METRO"), "order_date": plainDate,
		}}
		for range lines {
			in.Lines = append(in.Lines, map[string]string{"item_id": code("item", "MS-ANGLE-50"), "uom_id": code("uom", "KGS"),
				"qty": "100", "price": "1", "tax_code_id": code("tax_code", "GST18"), "hsn_sac": "7216"})
		}
		r := h.Do(admin, purchase.DocType, 0, doctype.ActionSave, in)
		r = h.Do(admin, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
		return r.ID, h.lineIDs(r.ID)
	}
	small, smallLines := po(longLines)
	big, bigLines := po(manyLines)
	grn := func(poID int64, lines []string) doc.Result {
		in := doc.ActionInput{Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(poID, 10), "site_id": code("site", "METRO-SITE"), "project_id": code("project", "PRJ-METRO"),
			"receipt_date": plainDate, "challan_kind": "Delivery challan", "challan_no": "DC-1042",
		}}
		for _, l := range lines {
			in.Lines = append(in.Lines, map[string]string{"po_line_id": l, "received_qty": "1"})
		}
		return h.Do(suresh, goodsreceipt.DocType, 0, doctype.ActionSave, in)
	}
	submit := func() doc.Result {
		r := grn(small, smallLines)
		return h.Do(suresh, goodsreceipt.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	}
	ids := docscreen.SampleIDs{Draft: grn(small, smallLines).ID, Lines500: grn(big, bigLines).ID}
	ids.Approved = submit().ID
	r := submit()
	ids.Cancelled = h.Do(ravi, goodsreceipt.DocType, r.ID, doctype.ActionCancel, doc.ActionInput{Version: r.Version, Reason: "Wrong delivery"}).ID

	if _, err := h.App.Approvals().CreateRule(auth.WithPrincipal(context.Background(), p), p, approval.RuleInput{
		DocType: goodsreceipt.DocType, ApproverType: approval.ApproverRole, Role: auth.RoleAdministrator,
	}); err != nil {
		h.t.Fatalf("approval rule for %s: %v", goodsreceipt.DocType, err)
	}
	ids.Pending = submit().ID
	r = submit()
	ids.Rejected = h.Do(admin, goodsreceipt.DocType, r.ID, doctype.ActionReject, doc.ActionInput{Version: r.Version, Reason: "Wrong delivery"}).ID
	return ids
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
// Deps leave out the line screens GRN doesn't have (CD-329).
func (h *Harness) grnScreens(ids docscreen.SampleIDs) []screens.Screen {
	var out []screens.Screen
	for _, sc := range docscreen.Screens(goodsreceipt.DocType, ids, h.App.DocDeps()) {
		switch {
		case strings.HasSuffix(sc.Name, " reject"):
			sc.As = admin
		case strings.HasSuffix(sc.Name, " cancel"):
			sc.As = ravi
		default:
			sc.As = suresh
		}
		out = append(out, sc)
	}
	return out
}
