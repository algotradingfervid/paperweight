package mstfixture_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const qtyUnit types.Qty = 1000 // 1.000

var orderDate = types.Date{Year: 2026, Month: time.October, Day: 24}

// world is a seeded Env with the services.
type world struct {
	env *msttest.Env
	m   msttest.Masters
	svc *allmasters.Services
}

func newWorld(t *testing.T) world {
	t.Helper()
	env := msttest.New(t)
	return world{env: env, m: msttest.SeedMasters(t, env), svc: allmasters.New(env.Kit)}
}

// shree is a header for SHREE's Mumbai address at branch PUNE.
func (w world) shree() mstfixture.Header {
	return mstfixture.Header{
		VendorID: w.m.Parties["SHREE"], AddressID: w.m.Addresses["Mumbai"],
		BranchID: w.m.Branches["PUNE"], Date: orderDate,
	}
}

// line is qty of item code in UoM unit at ₹62.5000.
func (w world) line(code, unit string, qty types.Qty) mstfixture.Line {
	return mstfixture.Line{ItemID: w.m.Items[code], UoMID: w.m.UoMs[unit], TaxCodeID: w.m.TaxCodes["GST18"], Qty: qty, Rate: 62_5000}
}

func (w world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatalf("read: %v", err)
	}
}

// fieldMessages returns the field errors of a refused save.
func fieldMessages(t *testing.T, err error) []doctype.FieldError {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("Save = %v (%T); want an error with field errors", err, err)
	}
	return he.FieldErrors()
}

func wantFieldError(t *testing.T, err error, want doctype.FieldError) {
	t.Helper()
	for _, fe := range fieldMessages(t, err) {
		if fe == want {
			return
		}
	}
	t.Errorf("field errors = %+v; want %+v among them", fieldMessages(t, err), want)
}

// TestMstFixture_SeedMastersMatchFixtures: every canonical master comes out of
// the services with the 00-fixtures.md and M9 values.
func TestMstFixture_SeedMastersMatchFixtures(t *testing.T) {
	w := newWorld(t)
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		c, ok, err := w.svc.Company.Company(ctx, tx)
		if err != nil || !ok {
			t.Fatalf("Company = %v, %v", ok, err)
		}
		if c.LegalName != "Vardhan Infra Projects Pvt Ltd" || c.PAN != "AAFCV2449G" {
			t.Errorf("company = %+v", c)
		}
		for _, b := range []struct {
			code, name, state, gstin string
			head                     bool
		}{
			{"PUNE", "Pune Works", "27", "27AAFCV2449G1Z7", true},
			{"BLR", "Bengaluru Site Office", "29", "29AAFCV2449G1Z3", false},
		} {
			got, err := w.svc.Company.Get(ctx, tx, w.m.Branches[b.code])
			if err != nil {
				t.Fatalf("branch %s: %v", b.code, err)
			}
			if got.Code != b.code || got.Name != b.name || got.StateCode != b.state || got.GSTIN != b.gstin || got.IsHeadOffice != b.head {
				t.Errorf("branch %s = %+v", b.code, got)
			}
		}

		primary, err := w.svc.Address.PrimaryGSTIN(ctx, tx, w.m.Parties["SHREE"])
		if err != nil || primary != "27AABCS1429B1ZU" {
			t.Errorf("SHREE primary GSTIN = %q, %v", primary, err)
		}
		local, err := w.svc.Party.Get(ctx, tx, w.m.Parties["LOCAL"])
		if err != nil || local.GSTCategory != gstin.Unregistered || !local.IsVendor || local.IsCustomer {
			t.Errorf("LOCAL = %+v, %v", local, err)
		}
		for _, a := range []struct {
			place, pincode, gstin             string
			billing, shipping, primaryBilling bool
		}{
			{"Mumbai", "400001", "27AABCS1429B1ZU", true, false, true},
			{"Thane", "400601", "27AABCS1429B2ZT", false, true, false},
			{"Delhi", "110001", "07AABCS1429B1ZW", false, true, false},
			{"Bengaluru", "560001", "29AABCT1332L1ZA", true, false, true},
			{"Rajkot", "360001", "24AAACR5055K1ZD", true, false, true},
			{"Chennai", "600001", "33AAACT2727Q1Z3", true, false, true},
			{"Pune", "411001", "", true, false, true},
		} {
			got, err := w.svc.Address.Get(ctx, tx, w.m.Addresses[a.place])
			if err != nil {
				t.Fatalf("address %s: %v", a.place, err)
			}
			if got.City != a.place || got.Pincode != a.pincode || got.GSTIN != a.gstin ||
				got.IsBilling != a.billing || got.IsShipping != a.shipping || got.PrimaryBilling != a.primaryBilling {
				t.Errorf("address %s = %+v", a.place, got)
			}
		}

		for _, c := range []struct {
			code, unit string
			factor     types.Factor
		}{
			{"BOLT-M12", "BOX", 100 * uom.FactorOne},
			{"CABLE-4C", "DRUM", 500 * uom.FactorOne},
			{"BOLT-M12", "NOS", uom.FactorOne},
		} {
			f, ok, err := w.svc.Item.Factor(ctx, tx, w.m.Items[c.code], w.m.UoMs[c.unit])
			if err != nil || !ok || f != c.factor {
				t.Errorf("%s %s factor = %d, %v, %v; want %d", c.code, c.unit, f, ok, err, c.factor)
			}
		}
		for code, unit := range map[string]string{
			"MS-ANGLE-50": "KGS", "BOLT-M12": "NOS", "WIRE-18G": "KGS", "CABLE-4C": "MTR", "CEMENT-OPC": "BAG", "SVC-CRANE": "HRS",
		} {
			it, err := w.svc.Item.Get(ctx, tx, w.m.Items[code])
			if err != nil {
				t.Fatalf("item %s: %v", code, err)
			}
			if it.StockUoMID != w.m.UoMs[unit] || it.DefaultTaxCodeID != w.m.TaxCodes["GST18"] {
				t.Errorf("item %s = %+v; want stock %s, tax GST18", code, it, unit)
			}
		}

		for code, branch := range map[string]string{"PRJ-METRO": "PUNE", "PRJ-TECHPARK": "BLR"} {
			p, err := w.svc.Project.Get(ctx, tx, w.m.Projects[code])
			if err != nil || p.BranchID != w.m.Branches[branch] {
				t.Errorf("project %s = %+v, %v; want branch %s", code, p, err, branch)
			}
		}
		for code, want := range map[string]struct{ branch, project string }{
			"PUNE-WH": {"PUNE", ""}, "METRO-SITE": {"PUNE", "PRJ-METRO"}, "TECH-SITE": {"BLR", "PRJ-TECHPARK"},
		} {
			s, err := w.svc.Site.Get(ctx, tx, w.m.Sites[code])
			if err != nil || s.BranchID != w.m.Branches[want.branch] || s.ProjectID != w.m.Projects[want.project] {
				t.Errorf("site %s = %+v, %v; want %+v", code, s, err, want)
			}
		}

		e4, err := w.svc.Employee.Get(ctx, tx, w.m.Employees["EMP-004"])
		if err != nil || e4.ManagerID != w.m.Employees["EMP-003"] || e4.BranchID != w.m.Branches["PUNE"] {
			t.Errorf("EMP-004 = %+v, %v", e4, err)
		}
		if e4.Name != "Priya Shah" || e4.Joining != (types.Date{Year: 2024, Month: time.April, Day: 1}) {
			t.Errorf("EMP-004 = %+v", e4)
		}
		e8, err := w.svc.Employee.Get(ctx, tx, w.m.Employees["EMP-008"])
		if err != nil || e8.Status != employee.Left || e8.Relieving != (types.Date{Year: 2026, Month: time.March, Day: 31}) {
			t.Errorf("EMP-008 = %+v, %v", e8, err)
		}
		a, ok := w.env.Kit.Auth.(*auth.Auth)
		if !ok {
			t.Fatalf("Kit.Auth = %T", w.env.Kit.Auth)
		}
		old, found, err := a.User(ctx, tx, e8.UserID)
		if err != nil || !found || old.Enabled {
			t.Errorf("old@ = %+v, %v, %v; want disabled", old, found, err)
		}

		for code, name := range map[string]string{
			"TRAVEL": "Travel", "FOOD": "Food", "FUEL": "Fuel", "LODGING": "Lodging", "SITE-MISC": "Site miscellaneous",
		} {
			et, err := w.svc.ExpenseType.Get(ctx, tx, w.m.ExpenseTypes[code])
			if err != nil || et.Code != code || et.Name != name {
				t.Errorf("expense type %s = %+v, %v", code, et, err)
			}
		}
		return nil
	})

	wantIDs := map[string]int64{"PRJ-METRO": 1, "PRJ-TECHPARK": 2}
	if !reflect.DeepEqual(w.m.Projects, wantIDs) {
		t.Errorf("Projects = %v; want %v (fixture order)", w.m.Projects, wantIDs)
	}
	if len(w.m.Items) != 6 || len(w.m.Parties) != 5 || len(w.m.Employees) != 8 || len(w.m.Sites) != 3 ||
		len(w.m.ExpenseTypes) != 5 || len(w.m.UoMs) != 11 || len(w.m.TaxCodes) != 10 {
		t.Errorf("Masters = %+v; want 6 items, 5 parties, 8 employees, 3 sites, 5 expense types, 11 UoMs, 10 tax codes", w.m)
	}
}

// TestMstFixture_ApproveAssignsNumber: an order saved through the engine
// stores the snapshots, submit with no approval rule approves it and the
// series gives the first number of FY 2026-27 (M7).
func TestMstFixture_ApproveAssignsNumber(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(),
		[]mstfixture.Line{w.line("MS-ANGLE-50", "KGS", 1200*qtyUnit), w.line("BOLT-M12", "BOX", 2*qtyUnit)})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if res.State != doctype.StateDraft {
		t.Errorf("Save state = %q; want draft", res.State)
	}
	want := mstfixture.Snap{
		VendorName: "Shree Steel Traders", VendorGSTIN: "27AABCS1429B1ZU",
		Lines: []mstfixture.SnapLine{
			{ItemCode: "MS-ANGLE-50", Factor: uom.FactorOne, StockQty: 1200 * qtyUnit},
			{ItemCode: "BOLT-M12", Factor: 100 * uom.FactorOne, StockQty: 200 * qtyUnit},
		},
	}
	if got := mstfixture.Snapshot(t, w.env, res.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot = %+v; want %+v", got, want)
	}
	const number = "PO/26-27/00001"
	if got := mstfixture.Approve(t, w.env, res.ID); got != number {
		t.Errorf("Approve = %q; want %s", got, number)
	}
	if got := mstfixture.Snapshot(t, w.env, res.ID); !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot after approve = %+v; want %+v", got, want)
	}
}

// TestMstFixture_ReferencesSeeOrders: Engine.References reads mst_order, the
// table msttest.New migrated, so a used master is reported by its number, and
// Cancel leaves it used.
func TestMstFixture_ReferencesSeeOrders(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	res, err := mstfixture.Save(ctx, w.env, w.env.Admin, 0, w.shree(), []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	mstfixture.Approve(t, w.env, res.ID)
	mstfixture.Cancel(t, w.env, res.ID)
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		for table, id := range map[string]int64{"party": w.m.Parties["SHREE"], "item": w.m.Items["MS-ANGLE-50"]} {
			ref, used, err := w.env.Kit.UsedOutsideDrafts(ctx, tx, table, id)
			if err != nil || !used || ref.Display != "PO/26-27/00001" || ref.State != doctype.StateCancelled {
				t.Errorf("UsedOutsideDrafts(%s) = %+v, %v, %v; want the cancelled order", table, ref, used, err)
			}
		}
		return nil
	})
}

// TestMstFixture_SaveUsesCurrentVersion: Save on a draft reads the version
// itself, resends the lines (so a later refusal shows on an unchanged draft)
// and refreshes the snapshots.
func TestMstFixture_SaveUsesCurrentVersion(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	h := w.shree()
	res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	h.AddressID = w.m.Addresses["Thane"]
	again, err := mstfixture.Save(ctx, w.env, w.env.Kavya, res.ID, h, []mstfixture.Line{w.line("WIRE-18G", "KGS", qtyUnit)})
	if err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if again.ID != res.ID || again.Version != res.Version+1 {
		t.Errorf("second Save = %+v; want id %d, version %d", again, res.ID, res.Version+1)
	}
	snap := mstfixture.Snapshot(t, w.env, res.ID)
	if snap.VendorGSTIN != "27AABCS1429B2ZT" || len(snap.Lines) != 1 || snap.Lines[0].ItemCode != "WIRE-18G" {
		t.Errorf("Snapshot = %+v; want Thane's GSTIN and WIRE-18G", snap)
	}

	// A zero id clears a stored link: it is sent blank, not left out.
	h.AddressID = 0
	if _, err := mstfixture.Save(ctx, w.env, w.env.Kavya, res.ID, h, []mstfixture.Line{w.line("WIRE-18G", "KGS", qtyUnit)}); err != nil {
		t.Fatalf("third Save: %v", err)
	}
	if got := mstfixture.Snapshot(t, w.env, res.ID).VendorGSTIN; got != "" {
		t.Errorf("VendorGSTIN with no address = %q; want empty", got)
	}

	// Clearing the vendor clears its snapshot too.
	h.AddressID = w.m.Addresses["Thane"]
	if _, err := mstfixture.Save(ctx, w.env, w.env.Kavya, res.ID, h, []mstfixture.Line{w.line("WIRE-18G", "KGS", qtyUnit)}); err != nil {
		t.Fatalf("fourth Save: %v", err)
	}
	h.VendorID, h.AddressID = 0, 0
	if _, err := mstfixture.Save(ctx, w.env, w.env.Kavya, res.ID, h, []mstfixture.Line{w.line("WIRE-18G", "KGS", qtyUnit)}); err != nil {
		t.Fatalf("fifth Save: %v", err)
	}
	if got := mstfixture.Snapshot(t, w.env, res.ID); got.VendorName != "" || got.VendorGSTIN != "" {
		t.Errorf("Snapshot with no vendor = %+v; want no vendor name or GSTIN", got)
	}
}

// TestMstFixture_ResaveAfterLostConversion: a draft's line keeps its values,
// but resaving them after the item lost that UoM gets CD-199's refusal.
func TestMstFixture_ResaveAfterLostConversion(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	h, lines := w.shree(), []mstfixture.Line{w.line("BOLT-M12", "BOX", qtyUnit)}
	res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, lines)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	id := w.m.Items["BOLT-M12"]
	var in item.Input
	var version int
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		it, err := w.svc.Item.Get(ctx, tx, id)
		if err != nil {
			return err
		}
		convs, err := w.svc.Item.Conversions(ctx, tx, id)
		if err != nil {
			return err
		}
		in = item.Input{
			Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC, StockUoMID: it.StockUoMID,
			PurchaseUoMID: it.PurchaseUoMID, DefaultTaxCodeID: it.DefaultTaxCodeID, IsStock: it.IsStock, StandardRate: it.StandardRate,
		}
		for _, c := range convs {
			if c.UoMID != w.m.UoMs["BOX"] {
				in.Conversions = append(in.Conversions, c)
			}
		}
		version = it.Version
		return nil
	})
	if _, err := w.svc.Item.Update(ctx, w.env.Admin, id, version, in); err != nil {
		t.Fatalf("Update: %v", err)
	}

	_, err = mstfixture.Save(ctx, w.env, w.env.Kavya, res.ID, h, lines)
	wantFieldError(t, err, doctype.FieldError{Field: "uom_id", Line: 1, Message: "Row 1: Item BOLT-M12 has no conversion for UoM BOX. Pick another UoM."})
}

// TestMstFixture_HookRefusals: the rules Hooks.Validate runs through the
// masters, each on the save path (M8, CD-199 item 2).
func TestMstFixture_HookRefusals(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()

	t.Run("vendor role", func(t *testing.T) {
		var id int64
		if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
			s, err := w.svc.Party.CreateTx(ctx, tx, w.env.Admin, party.Input{Code: "C-ONLY", Name: "Only Customer Ltd", IsCustomer: true})
			id = s.ID
			return err
		}); err != nil {
			t.Fatalf("create customer: %v", err)
		}
		h := w.shree()
		h.VendorID, h.AddressID = id, 0
		_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
		wantFieldError(t, err, doctype.FieldError{Message: "Only Customer Ltd is not marked as a vendor. Tick Vendor on the party first."})
	})
	t.Run("address of another party", func(t *testing.T) {
		h := w.shree()
		h.AddressID = w.m.Addresses["Rajkot"]
		_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
		wantFieldError(t, err, doctype.FieldError{Field: "address_id", Message: "That address belongs to another party. Pick one of this party's addresses."})
	})
	t.Run("whole number", func(t *testing.T) {
		_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{w.line("BOLT-M12", "NOS", 3400)})
		wantFieldError(t, err, doctype.FieldError{Field: "qty", Line: 1, Message: "Row 1: Quantity 3.4 can't be a fraction for UoM NOS. Enter a whole number."})
	})
	t.Run("no conversion", func(t *testing.T) {
		_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(),
			[]mstfixture.Line{w.line("WIRE-18G", "KGS", qtyUnit), w.line("BOLT-M12", "KGS", qtyUnit)})
		wantFieldError(t, err, doctype.FieldError{Field: "uom_id", Line: 2, Message: "Row 2: Item BOLT-M12 has no conversion for UoM KGS. Pick another UoM."})
	})
}

// TestMstFixture_BlankLinks: the hook leaves blank links to the engine: a draft
// may have no vendor, whose Required error comes at submit, and an expense
// line has no item to snapshot and submits.
func TestMstFixture_BlankLinks(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	expense := mstfixture.Line{ExpenseTypeID: w.m.ExpenseTypes["TRAVEL"], Qty: qtyUnit, Rate: 62_5000}

	h := w.shree()
	h.VendorID, h.AddressID = 0, 0
	draft, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, []mstfixture.Line{expense})
	if err != nil {
		t.Fatalf("Save with no vendor: %v", err)
	}
	_, err = w.env.Kit.Engine.Do(ctx, w.env.Kavya, mstfixture.DocType, draft.ID, doc.ActionSubmit, doc.ActionInput{Version: draft.Version})
	if fes := fieldMessages(t, err); len(fes) != 1 || fes[0].Field != "vendor_id" {
		t.Errorf("field errors = %+v; want one on vendor_id", fes)
	}

	res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{expense})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	want := []mstfixture.SnapLine{{}}
	if got := mstfixture.Snapshot(t, w.env, res.ID).Lines; !reflect.DeepEqual(got, want) {
		t.Errorf("Snapshot lines = %+v; want %+v", got, want)
	}
	mstfixture.Approve(t, w.env, res.ID)
}

// TestMstFixture_IncompleteLines: a draft may keep an unfinished line, but
// submit refuses it on its own row, after a good expense line.
func TestMstFixture_IncompleteLines(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	expense := mstfixture.Line{ExpenseTypeID: w.m.ExpenseTypes["TRAVEL"], Qty: qtyUnit, Rate: 62_5000}
	for _, c := range []struct {
		name string
		line mstfixture.Line
		want string
	}{
		{"item with no UoM", mstfixture.Line{ItemID: w.m.Items["MS-ANGLE-50"], Qty: qtyUnit}, "Row 2: UoM is required."},
		{"UoM with no item", mstfixture.Line{UoMID: w.m.UoMs["KGS"], Qty: qtyUnit}, "Row 2: Item is required."},
		{"neither item nor expense type", mstfixture.Line{Qty: qtyUnit}, "Row 2: Item is required."},
	} {
		t.Run(c.name, func(t *testing.T) {
			res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{expense, c.line})
			if err != nil {
				t.Fatalf("Save: %v", err)
			}
			_, err = w.env.Kit.Engine.Do(ctx, w.env.Kavya, mstfixture.DocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
			var de *doc.Error
			if !errors.As(err, &de) || de.Message != c.want {
				t.Errorf("submit = %v; want %q", err, c.want)
			}
		})
	}
}

// TestMstFixture_HookPanicFailsAction: Validate can't return an error, so one
// that isn't a *mstkit.ValidationError panics and the engine fails the action.
func TestMstFixture_HookPanicFailsAction(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	if err := w.env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `ALTER TABLE mst_order DROP COLUMN snap_vendor_name`)
		return err
	}); err != nil {
		t.Fatalf("drop column: %v", err)
	}
	_, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, w.shree(), []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
	if err == nil || !strings.Contains(err.Error(), "snap_vendor_name") {
		t.Errorf("Save = %v; want the failed action naming the column", err)
	}
}

// TestMstFixture_ProjectWarning: a Link to a master goes through its
// Lookup: On Hold reaches doc.Result.Warnings (CD-68).
func TestMstFixture_ProjectWarning(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	id := w.m.Projects["PRJ-METRO"]
	var in project.Input
	var version int
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		p, err := w.svc.Project.Get(ctx, tx, id)
		in = project.Input{Code: p.Code, Name: p.Name, BranchID: p.BranchID, Status: project.OnHold}
		version = p.Version
		return err
	})
	if _, err := w.svc.Project.Update(ctx, w.env.Admin, id, version, in); err != nil {
		t.Fatalf("Update: %v", err)
	}
	h := w.shree()
	h.ProjectID = id
	res, err := mstfixture.Save(ctx, w.env, w.env.Kavya, 0, h, []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)})
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if want := []string{"Project PRJ-METRO is On Hold."}; !reflect.DeepEqual(res.Warnings, want) {
		t.Errorf("Warnings = %v; want %v", res.Warnings, want)
	}
}

// TestMstFixture_Rights: Kavya and Priya work on orders through DefaultRights;
// Priya's project restriction holds, and Ravi has no right here.
func TestMstFixture_Rights(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	lines := []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)}
	h := w.shree()
	h.ProjectID = w.m.Projects["PRJ-METRO"]
	if _, err := mstfixture.Save(ctx, w.env, w.env.Priya, 0, h, lines); err != nil {
		t.Errorf("Priya Save on PRJ-METRO: %v", err)
	}
	// CD-245 (AUTH-099): a create on a disallowed project is AUTH's value
	// error (403), not the 404 of the proposed record.
	h.ProjectID = w.m.Projects["PRJ-TECHPARK"]
	var ve *auth.ValueError
	if _, err := mstfixture.Save(ctx, w.env, w.env.Priya, 0, h, lines); !errors.As(err, &ve) || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Priya Save on PRJ-TECHPARK = %v; want auth.ValueError (403)", err)
	}
	if _, err := mstfixture.Save(ctx, w.env, w.env.Ravi, 0, h, lines); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Ravi Save = %v; want auth.ErrForbidden", err)
	}
}

// TestMstFixture_DeleteAndAmend: msttest's engine has a Files service, so
// delete and amend run (Task 24's TestMaster_AmendWithDisabledMaster).
func TestMstFixture_DeleteAndAmend(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	lines := []mstfixture.Line{w.line("MS-ANGLE-50", "KGS", qtyUnit)}
	draft, err := mstfixture.Save(ctx, w.env, w.env.Admin, 0, w.shree(), lines)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := w.env.Kit.Engine.Do(ctx, w.env.Admin, mstfixture.DocType, draft.ID, doc.ActionDelete, doc.ActionInput{Version: draft.Version}); err != nil {
		t.Errorf("delete: %v", err)
	}

	res, err := mstfixture.Save(ctx, w.env, w.env.Admin, 0, w.shree(), lines)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	mstfixture.Approve(t, w.env, res.ID)
	mstfixture.Cancel(t, w.env, res.ID)
	amended, err := w.env.Kit.Engine.Do(ctx, w.env.Admin, mstfixture.DocType, res.ID, doc.ActionAmend, doc.ActionInput{Version: res.Version + 2})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if amended.ID == res.ID || amended.State != doctype.StateDraft {
		t.Errorf("amend = %+v; want a new draft", amended)
	}
}
