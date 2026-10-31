package goodsreceipt_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// g6Choice is the picker row the plan's G6 Do 1 gives for stored PO po:
// its display, vendor snapshot name, site name, lines with pending and all
// lines.
func g6Choice(t *testing.T, env *grntest.Env, po doc.Doc, vendor string, pending int) goodsreceipt.POChoice {
	t.Helper()
	site := g2Query(t, env.DB, `SELECT s.name FROM purchase_order o JOIN site s ON s.id = o.site_id WHERE o.id = ?`, po.ID)
	if len(site) != 1 || po.Number == "" {
		t.Fatalf("PO %d: number %q, site %v", po.ID, po.Number, site)
	}
	return goodsreceipt.POChoice{ID: po.ID, Display: po.Number, VendorName: vendor, SiteName: site[0][0], Pending: pending, Lines: len(po.Lines)}
}

// g6Choices calls POChoices and stops the test on an error.
func g6Choices(t *testing.T, env *grntest.Env, what string, all bool, q string, page int) []goodsreceipt.POChoice {
	t.Helper()
	got, err := env.Service.POChoices(t.Context(), env.Suresh, all, q, page)
	if err != nil {
		t.Fatalf("%s: POChoices: %v", what, err)
	}
	return got
}

// g6WantChoices compares rows in order.
func g6WantChoices(t *testing.T, what string, got []goodsreceipt.POChoice, want ...goodsreceipt.POChoice) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s:\n got %+v\nwant %+v", what, got, want)
	}
}

// GRN-001 (GRN spec §8, §11; AUTH-110; UX doc §5.2; CD-234): POChoices for
// restricted Suresh on the real auth.Service. Suresh (Stores) holds only
// read on purchase.Order, the one right the picker's Scope names. Only
// approved POs on his sites (PUNE-WH, METRO-SITE) with pending above 0 are
// listed; with all, fully received ones too; never another site's, a
// closed, cancelled or draft PO. Rows are ordered by vendor name, then PO
// number: the POs are made so that id order, number order and vendor-then-id
// order all differ from it. q matches PO number, vendor code, vendor name
// and GSTIN, case-insensitively (AD-12 ruling); page is 1-based, 25 a page.
func TestGRN_POPicker(t *testing.T) {
	env := grntest.New(t)
	metro := g1MasterID(t, env.DB, "site", "METRO-SITE")

	// A: SHREE, saved first (lowest id), approved after C, so numbered after it.
	a := g6Order(t, env, nil, purchasetest.StopAtDraft())
	// C: SHREE, two lines; line 1 fully received below, so 1 of 2 pending.
	c := g6Order(t, env, nil, purchasetest.OrderLines(
		map[string]string{"qty": "100"},
		map[string]string{"item_id": g1MasterID(t, env.DB, "item", "WIRE-18G"), "qty": "50", "price": "96", "hsn_sac": "7217"},
	))
	a = g6Approve(t, env, a.ID)
	// B: BHARAT, numbered last, listed first.
	b := g6Order(t, env, map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "BHARAT")})
	// L: LOCAL (Ganesh Hardware) on PUNE-WH, Suresh's other site.
	l := g6Order(t, env, map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "LOCAL"), "site_id": g1MasterID(t, env.DB, "site", "PUNE-WH")})
	// E: COROM, fully received by a GRN.
	e := g6Order(t, env, map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "COROM")})
	// Never listed for Suresh: another site's, a closed, a cancelled and a draft PO.
	tech := g6TechPO(t, env)
	closed := g6Order(t, env, nil)
	env.ClosePO(t, env.Priya, closed.ID, "Vendor stopped supplying")
	cancelled := g6Order(t, env, nil)
	env.CancelPO(t, env.Ravi, cancelled.ID, "Ordered twice")
	draft := g6Order(t, env, nil, purchasetest.StopAtDraft())

	env.GRN(t, env.Suresh, c.ID, []grntest.Line{{POLineID: c.Lines[0].ID, Received: "100"}}, doctype.StateApproved)
	env.GRN(t, env.Suresh, e.ID, []grntest.Line{{POLineID: e.Lines[0].ID, Received: "100"}}, doctype.StateApproved)

	if a.ID >= c.ID || c.ID >= b.ID || a.Number <= c.Number || b.Number <= a.Number {
		t.Fatalf("setup: ids A %d C %d B %d, numbers %s %s %s: want id order A, C, B and number order C, A, B", a.ID, c.ID, b.ID, a.Number, c.Number, b.Number)
	}
	if got := g1Int(t, a.Header["site_id"]); got != metro {
		t.Fatalf("setup: PO A on site %s, want METRO-SITE %s", got, metro)
	}

	rowA, rowB, rowC := g6Choice(t, env, a, "Shree Steel Traders", 1), g6Choice(t, env, b, "Bharat Tools", 1), g6Choice(t, env, c, "Shree Steel Traders", 1)
	rowL, rowE := g6Choice(t, env, l, "Ganesh Hardware (unregistered)", 1), g6Choice(t, env, e, "Coromandel Cables", 0)
	if rowC.Lines != 2 {
		t.Fatalf("setup: PO C has %d lines, want 2", rowC.Lines)
	}

	pending := []goodsreceipt.POChoice{rowB, rowL, rowC, rowA}
	g6WantChoices(t, "pending only", g6Choices(t, env, "pending only", false, "", 1), pending...)
	g6WantChoices(t, "with fully received", g6Choices(t, env, "all", true, "", 1), rowB, rowE, rowL, rowC, rowA)

	// The restriction comes from Auth.Scope, not a fixed filter: the
	// unrestricted Administrator also sees the TECH-SITE PO; nobody sees the
	// closed, cancelled or draft one.
	adm, err := env.Service.POChoices(t.Context(), env.Admin, true, "", 1)
	if err != nil {
		t.Fatalf("POChoices as admin: %v", err)
	}
	ids := make([]int64, 0, len(adm))
	for _, r := range adm {
		ids = append(ids, r.ID)
	}
	if !slices.Contains(ids, tech.ID) {
		t.Errorf("admin's choices %v lack the TECH-SITE PO %d", ids, tech.ID)
	}
	for _, never := range []doc.Doc{closed, cancelled, draft} {
		if slices.Contains(ids, never.ID) {
			t.Errorf("admin's choices %v hold PO %d (state %s)", ids, never.ID, env.Load(t, "purchase.Order", never.ID).State)
		}
	}

	// q, case-insensitive substring of PO number, vendor code, vendor name
	// or GSTIN; blank is no filter (AD-12 ruling on UX doc §5.2).
	gstin := g2Query(t, env.DB, `SELECT snap_vendor_gstin FROM purchase_order WHERE id = ?`, c.ID)[0][0]
	if len(gstin) != 15 {
		t.Fatalf("setup: PO C's vendor GSTIN %q", gstin)
	}
	for _, tc := range []struct {
		name, q string
		want    []goodsreceipt.POChoice
	}{
		{"PO number", strings.ToLower(b.Number), []goodsreceipt.POChoice{rowB}},
		{"vendor code", "local", []goodsreceipt.POChoice{rowL}}, // LOCAL's name is Ganesh Hardware
		{"vendor name", "BHARAT t", []goodsreceipt.POChoice{rowB}},
		{"GSTIN", strings.ToLower(gstin[6:]), []goodsreceipt.POChoice{rowC, rowA}},
		{"no match", "zzq", nil},
	} {
		got := g6Choices(t, env, "q "+tc.name, false, tc.q, 1)
		if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(got, tc.want)) {
			t.Errorf("q %s (%q):\n got %+v\nwant %+v", tc.name, tc.q, got, tc.want)
		}
	}

	// page: 1-based, below 1 is 1; page 2 of a short list is empty.
	g6WantChoices(t, "page 0", g6Choices(t, env, "page 0", false, "", 0), pending...)
	g6WantChoices(t, "page -1", g6Choices(t, env, "page -1", false, "", -1), pending...)
	if got := g6Choices(t, env, "page 2", false, "", 2); len(got) != 0 {
		t.Errorf("page 2 of %d rows: %+v, want none", len(pending), got)
	}
}
