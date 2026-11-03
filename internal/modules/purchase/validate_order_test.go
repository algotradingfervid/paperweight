package purchase_test

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	p2MsgAddressOwner = "That address belongs to another party. Pick one of this party's addresses." // CD-176
	p2MsgVendorOff    = "Vendor SHREE Shree Steel Traders is disabled. Pick an active vendor, or ask an admin to enable it."
)

// PO-007 (PO spec §3.1, §3.2, §8, §18; D-MST-03, MST-011, D-APR-15):
// snapshots and totals are written only while the PO is a draft. Validate
// runs again on approve, so master edits made while the PO is pending (vendor
// name and category, its address, an item's name) must not reach the
// approved PO. A new draft saved afterwards takes the new values.
func TestPO_SnapshotsSurviveMasterEdits(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	kit := p2Kit(env)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p2RefLines(t, env)...))
	if po.State != doctype.StatePending {
		t.Fatalf("reference PO state %q, want pending", po.State)
	}
	headCols := []string{"snap_vendor_code", "snap_vendor_name", "snap_vendor_gstin", "snap_vendor_category", "snap_vendor_address", "snap_site_name", "snap_branch_name"}
	lineCols := []string{"snap_item_code", "snap_item_name", "snap_uom_code", "snap_tax_code"}
	head, lines := p2Snap(t, env, po.ID, headCols...), p2LineSnap(t, env, po.ID, lineCols...)
	if head["snap_vendor_name"] != "Shree Steel Traders" || len(lines) != 2 || lines[0]["snap_item_name"] != "MS Angle 50x50x5" {
		t.Fatalf("snapshots before the edits = %v %v", head, lines)
	}

	// The vendor: a new name and category SEZ.
	parties := party.New(kit)
	shree := p2Int(t, masterID(t, env.DB, "party", "SHREE"))
	var pt party.Party
	var mumbai address.Address
	var angle item.Item
	addresses, items := address.New(kit), item.New(kit)
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if pt, err = parties.Get(ctx, tx, shree); err != nil {
			return err
		}
		if mumbai, err = addresses.Get(ctx, tx, p2Int(t, p2AddressID(t, env, "SHREE", "Mumbai"))); err != nil {
			return err
		}
		angle, err = items.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "item", "MS-ANGLE-50")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := parties.Update(ctx, env.Admin, pt.ID, pt.Version, party.Input{Code: pt.Code, Name: "Shree Steel Traders Pvt Ltd",
		IsVendor: true, IsCustomer: pt.IsCustomer, PAN: pt.PAN, GSTCategory: gstin.SEZ, DefaultPaymentTermsID: pt.DefaultPaymentTermsID}); err != nil {
		t.Fatalf("rename SHREE: %v", err)
	}
	if _, err := addresses.Update(ctx, env.Admin, mumbai.ID, mumbai.Version, address.Input{PartyID: mumbai.PartyID, Title: mumbai.Title,
		Line1: "Plot 9, New Road", Line2: mumbai.Line2, City: mumbai.City, StateCode: mumbai.StateCode, Country: mumbai.Country,
		Pincode: mumbai.Pincode, GSTIN: mumbai.GSTIN, IsBilling: mumbai.IsBilling, IsShipping: mumbai.IsShipping,
		PrimaryBilling: mumbai.PrimaryBilling, PrimaryShipping: mumbai.PrimaryShipping}); err != nil {
		t.Fatalf("edit the Mumbai address: %v", err)
	}
	if _, err := items.Update(ctx, env.Admin, angle.ID, angle.Version, item.Input{Code: angle.Code, Name: "MS Angle 50x50x6",
		Type: angle.Type, HSNSAC: angle.HSNSAC, StockUoMID: angle.StockUoMID, PurchaseUoMID: angle.PurchaseUoMID,
		DefaultTaxCodeID: angle.DefaultTaxCodeID, IsStock: angle.IsStock, StandardRate: angle.StandardRate}); err != nil {
		t.Fatalf("rename MS-ANGLE-50: %v", err)
	}

	p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, ""))
	if d := p2Load(t, env, po.ID); d.State != doctype.StateApproved {
		t.Fatalf("after approve: state %q", d.State)
	}
	if got := p2Snap(t, env, po.ID, headCols...); !mapsEqual(got, head) {
		t.Errorf("header snapshots after approve = %v, want unchanged %v (written only while draft)", got, head)
	}
	if got := p2LineSnap(t, env, po.ID, lineCols...); len(got) != 2 || !mapsEqual(got[0], lines[0]) || !mapsEqual(got[1], lines[1]) {
		t.Errorf("line snapshots after approve = %v, want unchanged %v", got, lines)
	}
	p2Totals(t, "approved PO", env, po.ID, p2Intra) // the SEZ change didn't recompute the split

	// A draft saved now reads the edited masters.
	fresh := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2RefLines(t, env)...))
	s := p2Snap(t, env, fresh.ID, "snap_vendor_name", "snap_vendor_category", "snap_vendor_address")
	if s["snap_vendor_name"] != "Shree Steel Traders Pvt Ltd" || s["snap_vendor_category"] != "SEZ" || s["snap_vendor_address"] == head["snap_vendor_address"] {
		t.Errorf("new draft snapshots = %v, want the edited vendor and address", s)
	}
	if got := p2LineSnap(t, env, fresh.ID, "snap_item_name"); len(got) == 0 || got[0]["snap_item_name"] != "MS Angle 50x50x6" {
		t.Errorf("new draft line snapshot = %v, want MS Angle 50x50x6", got)
	}
	p2Totals(t, "new draft from the SEZ vendor", env, fresh.ID, p2Inter)
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// PO-008 (PO spec §3.1, §8, §18; MST-042): a party without the vendor role
// is refused with party.CheckRole's text, on create and on a save of an
// existing draft, and nothing is written.
func TestPO_VendorRoleRequired(t *testing.T) {
	env := purchasetest.New(t)
	if _, err := party.New(p2Kit(env)).Create(t.Context(), env.Admin, party.Input{Code: "ACME", Name: "Acme Builders", IsCustomer: true}); err != nil {
		t.Fatalf("create customer ACME: %v", err)
	}
	want := doctype.FieldError{Field: "vendor_id", Message: "Acme Builders is not marked as a vendor. Set the party to Vendor or Both first."}
	acme := map[string]string{"vendor_id": masterID(t, env.DB, "party", "ACME")}

	before := p2CountAll(t, env)
	h := p2Header(t, env, "ACME", "METRO-SITE", "PRJ-METRO")
	_, err := p2Create(t, env, env.Priya, h, p2RefLines(t, env))
	p2WantInvalid(t, "create with ACME", err, want)
	p2SameCounts(t, "create with ACME", before, env)

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	before = p2CountAll(t, env)
	_, err = p2Save(t, env, env.Priya, po.ID, acme, nil)
	p2WantInvalid(t, "save with ACME", err, want)
	p2SameCounts(t, "save with ACME", before, env)
	if got := p2Plain(p2Load(t, env, po.ID).Header["vendor_id"]); got != masterID(t, env.DB, "party", "SHREE") {
		t.Errorf("vendor_id after the refusal = %s, want SHREE kept", got)
	}
}

// PO-009 (PO spec §3.1, §8, §18; CD-176): an address of another party is
// refused with CD-176's text, on the first save too, and nothing is written.
// A blank address defaults to the vendor's primary billing address, and an
// address of the vendor's own is kept.
func TestPO_AddressOwner(t *testing.T) {
	env := purchasetest.New(t)
	bengaluru := p2AddressID(t, env, "BHARAT", "Bengaluru")
	want := doctype.FieldError{Field: "vendor_address_id", Message: p2MsgAddressOwner}

	before := p2CountAll(t, env)
	h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	h["vendor_address_id"] = bengaluru
	_, err := p2Create(t, env, env.Priya, h, p2RefLines(t, env))
	p2WantInvalid(t, "first save with BHARAT's address", err, want)
	p2SameCounts(t, "first save with BHARAT's address", before, env)

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	addr := func() string { t.Helper(); return p2Plain(p2Load(t, env, po.ID).Header["vendor_address_id"]) }
	if got, want := addr(), p2PrimaryBilling(t, env, "SHREE"); got != want {
		t.Errorf("blank address defaulted to %s, want SHREE's primary billing %s", got, want)
	}
	thane := p2AddressID(t, env, "SHREE", "Thane")
	p2Must(t, "pick Thane")(p2Save(t, env, env.Priya, po.ID, map[string]string{"vendor_address_id": thane}, nil))
	if got := addr(); got != thane {
		t.Errorf("vendor's own Thane address = %s after save, want %s kept", got, thane)
	}

	before = p2CountAll(t, env)
	_, err = p2Save(t, env, env.Priya, po.ID, map[string]string{"vendor_address_id": bengaluru}, nil)
	p2WantInvalid(t, "save with BHARAT's address", err, want)
	p2SameCounts(t, "save with BHARAT's address", before, env)
	if got := addr(); got != thane {
		t.Errorf("address after the refusal = %s, want Thane %s kept", got, thane)
	}
}

// MOD-PO P2 Do (PO spec §8; CD-280 D7 (d)): when vendor_id differs from
// snap_vendor_id (the vendor changed since the last save), the address is
// replaced by the new vendor's primary billing address, even when the post
// names the old vendor's address; Validate then writes snap_vendor_id.
// Changing back restores the first vendor's primary billing address.
func TestPO_VendorChangeResetsAddress(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderHeader(map[string]string{"vendor_address_id": p2AddressID(t, env, "SHREE", "Thane")}))
	read := func() (addr, snapVendor string) {
		t.Helper()
		return p2Plain(p2Load(t, env, po.ID).Header["vendor_address_id"]), p2Snap(t, env, po.ID, "snap_vendor_id")["snap_vendor_id"]
	}
	shree, bharat := masterID(t, env.DB, "party", "SHREE"), masterID(t, env.DB, "party", "BHARAT")
	if a, v := read(); a != p2AddressID(t, env, "SHREE", "Thane") || v != shree {
		t.Fatalf("first save: address %s snap_vendor_id %s, want Thane and SHREE %s", a, v, shree)
	}

	p2Must(t, "vendor to BHARAT")(p2Save(t, env, env.Priya, po.ID, map[string]string{"vendor_id": bharat}, nil))
	if a, v := read(); a != p2PrimaryBilling(t, env, "BHARAT") || v != bharat {
		t.Errorf("vendor changed to BHARAT: address %s snap_vendor_id %s; want BHARAT's primary billing %s and %s", a, v, p2PrimaryBilling(t, env, "BHARAT"), bharat)
	}

	// The vendor changes back while the post still names an address of the
	// vendor being left: replaced, not refused.
	p2Must(t, "vendor back to SHREE with BHARAT's address posted")(p2Save(t, env, env.Priya, po.ID,
		map[string]string{"vendor_id": shree, "vendor_address_id": p2AddressID(t, env, "BHARAT", "Bengaluru")}, nil))
	if a, v := read(); a != p2PrimaryBilling(t, env, "SHREE") || v != shree {
		t.Errorf("vendor back to SHREE: address %s snap_vendor_id %s; want SHREE's primary billing %s and %s", a, v, p2PrimaryBilling(t, env, "SHREE"), shree)
	}
}

// PO-010 (PO spec §4, §18): a site on another project is refused with §4's
// text, on create and on an existing draft, and nothing is written.
func TestPO_SiteProjectMismatch(t *testing.T) {
	env := purchasetest.New(t)
	want := doctype.FieldError{Field: "site_id|project_id", Message: "Site TECH-SITE is on project PRJ-TECHPARK. Pick that project or another site."}

	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-METRO"), p2RefLines(t, env))
	p2WantInvalid(t, "create TECH-SITE on PRJ-METRO", err, want)
	p2SameCounts(t, "create TECH-SITE on PRJ-METRO", before, env)

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	before = p2CountAll(t, env)
	_, err = p2Save(t, env, env.Priya, po.ID, map[string]string{"site_id": masterID(t, env.DB, "site", "TECH-SITE")}, nil)
	p2WantInvalid(t, "save TECH-SITE on PRJ-METRO", err, want)
	p2SameCounts(t, "save TECH-SITE on PRJ-METRO", before, env)
	if got := p2Plain(p2Load(t, env, po.ID).Header["site_id"]); got != masterID(t, env.DB, "site", "METRO-SITE") {
		t.Errorf("site_id after the refusal = %s, want METRO-SITE kept", got)
	}
}

// PO-011 (PO spec §3.1, §18): a blank project is filled from the site's
// project for an unrestricted user (Arjun; Ravi has no create on POs). A
// site with no project leaves it blank, and a given project is kept.
func TestPO_ProjectDefaultsFromSite(t *testing.T) {
	env := purchasetest.New(t)
	for _, c := range []struct {
		site, project, want string
	}{
		{"METRO-SITE", "", masterID(t, env.DB, "project", "PRJ-METRO")},
		{"PUNE-WH", "", "<nil>"},
		{"PUNE-WH", "PRJ-TECHPARK", masterID(t, env.DB, "project", "PRJ-TECHPARK")},
	} {
		res, err := p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", c.site, c.project), p2RefLines(t, env))
		if err != nil {
			t.Errorf("%s with project %q: %v", c.site, c.project, err)
			continue
		}
		if got := p2Plain(p2Load(t, env, res.ID).Header["project_id"]); got != c.want {
			t.Errorf("%s with project %q: project_id = %s, want %s", c.site, c.project, got, c.want)
		}
	}
}

// PO-012 (PO spec §3.1, §18; CD-178), on the real auth.Service: Priya,
// restricted to PRJ-METRO, can't save a draft with a site and a blank
// project: DOC's "Project is required." comes first, Validate doesn't default
// it, and nothing is written. With the project it saves.
func TestPO_RestrictedDraftNeedsProject(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", ""), p2RefLines(t, env))
	p2WantInvalid(t, "Priya, blank project", err, doctype.FieldError{Field: "project_id", Message: "Project is required."})
	p2SameCounts(t, "Priya, blank project", before, env)
	if _, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env)); err != nil {
		t.Errorf("Priya with PRJ-METRO: %v, want saved", err)
	}
}

// PO-015 (PO spec §3.2, §18; MST-086, CD-277 (4)): 2.5 Nos of BOLT-M12 is
// refused through uom.CheckLineQty with its text, prefixed once; 3 saves.
func TestPO_WholeNumberUoM(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p2Line(t, env, "BOLT-M12", "NOS", "2.5", "12")})
	p2WantInvalid(t, "2.5 Nos", err, doctype.FieldError{Field: "qty", Line: 1,
		Message: "Line 1: Quantity 2.5 can't be a fraction for UoM NOS. Enter a whole number."})
	p2SameCounts(t, "2.5 Nos", before, env)
	if _, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p2Line(t, env, "BOLT-M12", "NOS", "3", "12")}); err != nil {
		t.Errorf("3 Nos: %v, want saved", err)
	}
}

// MOD-PO P2 (PO spec §3.2 "must have a conversion"; CD-199, CD-278): a line
// UoM the item doesn't convert from is refused with CD-278's text.
func TestPO_UoMNeedsConversion(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p2Line(t, env, "MS-ANGLE-50", "NOS", "3", "62.5")})
	p2WantInvalid(t, "MS-ANGLE-50 in NOS", err, doctype.FieldError{Field: "uom_id", Line: 1,
		Message: "Line 1: Item MS-ANGLE-50 has no conversion for UoM NOS. Pick another UoM."})
	p2SameCounts(t, "MS-ANGLE-50 in NOS", before, env)
}

// PO-017 (PO spec §4, §18): an overseas vendor is refused with §4's text and
// nothing is written.
func TestPO_OverseasRefused(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	kit := p2Kit(env)
	saved, err := party.New(kit).Create(ctx, env.Admin, party.Input{Code: "HAMBURG", Name: "Hamburg Stahl GmbH", IsVendor: true, GSTCategory: gstin.Overseas})
	if err != nil {
		t.Fatalf("create overseas vendor: %v", err)
	}
	if _, err := address.New(kit).Create(ctx, env.Admin, address.Input{PartyID: saved.ID, Title: "Hamburg", Line1: "Hafenstrasse 1",
		City: "Hamburg", Country: "Germany", IsBilling: true, PrimaryBilling: true}); err != nil {
		t.Fatalf("create the Hamburg address: %v", err)
	}
	before := p2CountAll(t, env)
	_, err = p2Create(t, env, env.Priya, p2Header(t, env, "HAMBURG", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env))
	p2WantInvalid(t, "overseas vendor", err, doctype.FieldError{Field: "vendor_id", Message: "Overseas vendors aren't supported on purchase orders yet."})
	p2SameCounts(t, "overseas vendor", before, env)
}

// PO-018 (PO spec §3.1, §4, §18), on the env's frozen clock (authtest.T0,
// 24-10-2026 10:30 IST): an order date after today (the IST business date) is
// refused, past dates always save; a required-by before the order date is
// refused. Moving the clock past midnight IST moves "today".
func TestPO_DateRules(t *testing.T) {
	env := purchasetest.New(t)
	future := doctype.FieldError{Field: "order_date", Message: "Order date can't be in the future."}
	early := doctype.FieldError{Field: "required_by", Message: "Required by can't be before the order date."}
	save := func(orderDate, requiredBy string) error {
		t.Helper()
		h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
		h["order_date"], h["required_by"] = orderDate, requiredBy
		_, err := p2Create(t, env, env.Priya, h, p2RefLines(t, env))
		return err
	}
	refused := func(what string, err error, want doctype.FieldError, before p2Counts) {
		t.Helper()
		p2WantInvalid(t, what, err, want)
		p2SameCounts(t, what, before, env)
	}

	before := p2CountAll(t, env)
	refused("order date 25-10-2026", save("2026-10-25", ""), future, before)
	for _, d := range []string{"2026-10-24", "2026-04-01", "2026-03-31", "2025-04-01"} {
		if err := save(d, ""); err != nil {
			t.Errorf("order date %s: %v, want saved (past dates are allowed)", d, err)
		}
	}
	before = p2CountAll(t, env)
	refused("required by 23-10-2026", save("2026-10-24", "2026-10-23"), early, before)
	for _, d := range []string{"2026-10-24", "2026-11-30"} {
		if err := save("2026-10-24", d); err != nil {
			t.Errorf("required by %s: %v, want saved", d, err)
		}
	}

	// 19:00 UTC on 24 Oct is 00:30 IST on 25 Oct.
	env.SetNow(t, time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC))
	if err := save("2026-10-25", ""); err != nil {
		t.Errorf("order date 25-10-2026 after midnight IST: %v, want saved", err)
	}
	before = p2CountAll(t, env)
	refused("order date 26-10-2026 after midnight IST", save("2026-10-26", ""), future, before)
}

// PO-071 (PO spec §3.2, §27.5; research S14; P1's Positive flag): a line
// qty of 0, or -1 posted straight to the engine, is refused with DOC's text,
// on create and on an existing draft; the line rows are unchanged.
func TestPO_QtyMustBePositive(t *testing.T) {
	env := purchasetest.New(t)
	want := doctype.FieldError{Field: "qty", Line: 1, Message: "Line 1: Quantity must be more than 0."}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(p2Line(t, env, "MS-ANGLE-50", "KGS", "5", "62.5")))
	for _, q := range []string{"0", "-1"} {
		before := p2CountAll(t, env)
		_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
			[]map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", q, "62.5")})
		p2WantInvalid(t, "create with qty "+q, err, want)
		p2SameCounts(t, "create with qty "+q, before, env)

		_, err = p2Save(t, env, env.Priya, po.ID, nil, []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", q, "62.5")})
		p2WantInvalid(t, "save with qty "+q, err, want)
		p2SameCounts(t, "save with qty "+q, before, env)
		if got := p2Plain(p2Load(t, env, po.ID).Lines[0].Values["qty"]); got != "5" {
			t.Errorf("after qty %s: stored qty %s, want 5", q, got)
		}
	}
}

// PO-072 (PO spec §3.2, §27.5; research S15; P1's NonNegative flag): a price
// of -1.0000 is refused with DOC's text; 0.0000 saves and stays 0.
func TestPO_RateNotNegative(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "5", "-1.0000")})
	p2WantInvalid(t, "price -1", err, doctype.FieldError{Field: "price", Line: 1, Message: "Line 1: Rate can't be negative."})
	p2SameCounts(t, "price -1", before, env)

	res := p2Must(t, "price 0")(p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "5", "0.0000")}))
	v := p2Load(t, env, res.ID).Lines[0].Values
	if p2Plain(v["price"]) != types.Rate(0).Plain() || p2Money(v["amount"]) != "0.00" {
		t.Errorf("price 0 stored as price %s amount %s, want 0 and 0.00 (not replaced by a default)", p2Plain(v["price"]), p2Money(v["amount"]))
	}
}

// PO-073 (PO spec §27.4; CD-276 (3); research S16): the same item on two
// lines saves, submits and approves with no warning; each line keeps its own
// description and pending.
func TestPO_SameItemTwoLines(t *testing.T) {
	env := purchasetest.New(t)
	a, b := p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "62.5"), p2Line(t, env, "MS-ANGLE-50", "KGS", "50", "62.5")
	a["description"], b["description"] = "MS Angle 50x50x5, 3 m lengths", "MS Angle 50x50x5, 6 m lengths"
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(a, b))
	if po.State != doctype.StateApproved || len(po.Lines) != 2 {
		t.Fatalf("order state %q with %d lines, want approved with 2", po.State, len(po.Lines))
	}
	if w := p2Warnings(t, env, env.Priya, po.ID); len(w) != 0 {
		t.Errorf("OrderWarnings = %q, want none", w)
	}
	for i, want := range []struct{ desc, pending string }{{a["description"], "100"}, {b["description"], "50"}} {
		v := po.Lines[i].Values
		if v["description"] != want.desc {
			t.Errorf("line %d description %v, want %q", i+1, v["description"], want.desc)
		}
		qty, _ := v["qty"].(types.Qty)
		already, _ := v["already_received"].(types.Qty)
		received, _ := v["received_qty"].(types.Qty)
		if got := purchase.Pending(qty, already, received); got != qtyOf(want.pending) {
			t.Errorf("line %d pending %s, want %s", i+1, got.Plain(), want.pending)
		}
	}
}

// PO-074 (PO spec §4, §27.5; D-APR-15; research S18), on the real
// auth.Service: SHREE disabled while Priya's PO waits for approval. Ravi's
// approve is refused with DOC's disabled text; the PO stays pending with no
// number and the request stays open; a new draft naming SHREE is refused the
// same way. Re-enabling SHREE lets the approve through (undo).
func TestPO_VendorDisabledWhilePending(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(p2RefLines(t, env)...))
	if po.State != doctype.StatePending {
		t.Fatalf("PO state %q, want pending", po.State)
	}
	parties := party.New(p2Kit(env))
	shree := p2Int(t, masterID(t, env.DB, "party", "SHREE"))
	setDisabled := func(off bool) {
		t.Helper()
		var pt party.Party
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			pt, err = parties.Get(ctx, tx, shree)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if err := parties.SetDisabled(ctx, env.Admin, shree, pt.Version, off); err != nil {
			t.Fatalf("SetDisabled(SHREE, %v): %v", off, err)
		}
	}
	request := func() approval.Request {
		t.Helper()
		var r approval.Request
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var ok bool
			var err error
			r, ok, err = env.Approvals.LatestRequest(ctx, tx, purchase.DocType, po.ID)
			if err == nil && !ok {
				t.Error("the PO has no approval request")
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return r
	}
	want := doctype.FieldError{Field: "vendor_id", Message: p2MsgVendorOff}

	setDisabled(true)
	before := p2CountAll(t, env)
	_, err := p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, "")
	p2WantInvalid(t, "approve with SHREE disabled", err, want)
	p2SameCounts(t, "approve with SHREE disabled", before, env)
	if d := p2Load(t, env, po.ID); d.State != doctype.StatePending || d.Number != "" || d.Version != po.Version {
		t.Errorf("after the refused approve: state %q number %q version %d, want pending, none, %d", d.State, d.Number, d.Version, po.Version)
	}
	if r := request(); r.Outcome != "" {
		t.Errorf("approval request outcome %q, want still open", r.Outcome)
	}
	_, err = p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env))
	p2WantInvalid(t, "new draft with SHREE disabled", err, want)
	p2SameCounts(t, "new draft with SHREE disabled", before, env)

	setDisabled(false)
	res := p2Must(t, "approve with SHREE enabled")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, ""))
	if res.State != doctype.StateApproved || res.Number != "PO/26-27/00001" {
		t.Errorf("approve after re-enabling: state %q number %q, want approved PO/26-27/00001", res.State, res.Number)
	}
	if r := request(); r.Outcome == "" {
		t.Error("approval request still open after the approve")
	}
}

// MOD-PO P2 Do (PO spec §3.1, §3.2, §8 "Defaults"; §14.1): blank values are
// filled on save: the vendor's primary billing address, the settings' PO
// terms, the line's description, HSN/SAC, UoM and tax code from the item,
// and discount 0. Values given are kept, and a cleared one comes back.
func TestPO_SaveDefaults(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	kit := modkit.New(modkit.Deps{DB: env.DB, Auth: env.Auth, Audit: audit.Default, Clock: env.Clock})
	s, err := kit.GetSettings(ctx, env.Admin)
	if err != nil {
		t.Fatal(err)
	}
	const terms = "Delivery within 7 days of this order."
	if err := kit.SaveSettings(ctx, env.Admin, s.Version, modkit.SettingsInput{LockedUpTo: s.LockedUpTo, DefaultPOTerms: terms}); err != nil {
		t.Fatalf("save settings: %v", err)
	}

	blankLine := map[string]string{"item_id": masterID(t, env.DB, "item", "MS-ANGLE-50"), "qty": "10", "price": "62.5",
		"uom_id": "", "tax_code_id": "", "description": "", "hsn_sac": "", "discount_pct": ""}
	given := p2Line(t, env, "WIRE-18G", "KGS", "5", "96", "GST12")
	given["description"] = "Binding wire, 25 kg coils"
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
		purchasetest.OrderHeader(map[string]string{"vendor_address_id": "", "terms": ""}), purchasetest.OrderLines(blankLine, given))

	check := func(what string) {
		t.Helper()
		d := p2Load(t, env, po.ID)
		if got, want := p2Plain(d.Header["vendor_address_id"]), p2PrimaryBilling(t, env, "SHREE"); got != want {
			t.Errorf("%s: vendor_address_id %s, want SHREE's primary billing %s", what, got, want)
		}
		if d.Header["terms"] != terms {
			t.Errorf("%s: terms %v, want the settings' %q", what, d.Header["terms"], terms)
		}
		if len(d.Lines) != 2 {
			t.Fatalf("%s: %d lines, want 2", what, len(d.Lines))
		}
		for i, want := range []map[string]string{
			{"description": "MS Angle 50x50x5", "hsn_sac": "7216", "uom_id": masterID(t, env.DB, "uom", "KGS"),
				"tax_code_id": masterID(t, env.DB, "tax_code", "GST18"), "discount_pct": types.Percent(0).Plain()},
			{"description": "Binding wire, 25 kg coils", "uom_id": masterID(t, env.DB, "uom", "KGS"),
				"tax_code_id": masterID(t, env.DB, "tax_code", "GST12")},
		} {
			for k, w := range want {
				if got := p2Plain(d.Lines[i].Values[k]); got != w {
					t.Errorf("%s: line %d %s = %s, want %s", what, i+1, k, got, w)
				}
			}
		}
	}
	check("first save")

	// Cleared on purpose, the defaults come back on the next save.
	p2Must(t, "clear the defaults")(p2Save(t, env, env.Priya, po.ID, map[string]string{"vendor_address_id": "", "terms": ""},
		[]map[string]string{blankLine, given}))
	check("after clearing")
}

// MOD-PO P2 Do (PO spec §3.1, §24.1; CD-280 D7 (a)): Validate never changes
// copied_from_no or copy_notes, on the first save or later ones.
func TestPO_CopiedFieldsKept(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"copied_from_no": "PO/26-27/00007"}))
	const notes = "Rate copied from PO/26-27/00007; check it."
	p2SetCopyNotes(t, env, po.ID, notes)
	p2Must(t, "save a header change")(p2Save(t, env, env.Priya, po.ID, map[string]string{"remarks": "Urgent"}, nil))
	p2Must(t, "save a line change")(p2Save(t, env, env.Priya, po.ID, nil, p2RefLines(t, env)))
	got := p2Snap(t, env, po.ID, "copied_from_no", "copy_notes", "remarks")
	if got["copied_from_no"] != "PO/26-27/00007" || got["copy_notes"] != notes || got["remarks"] != "Urgent" {
		t.Errorf("after two saves: %v, want copied_from_no PO/26-27/00007 and copy_notes %q unchanged", got, notes)
	}
}
