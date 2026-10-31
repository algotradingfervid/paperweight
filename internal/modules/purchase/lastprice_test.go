package purchase_test

import (
	"fmt"
	"maps"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for P7's tests (MOD-PO P7, last price). Every document row is
// written through Engine.Do or the purchasetest builders; the SQL here only
// reads ids.

// p7HSN is each fixture item's HSN/SAC (00-fixtures.md), so a line for
// another item doesn't keep the builder's default line's 7216.
var p7HSN = map[string]string{
	"MS-ANGLE-50": "7216", "BOLT-M12": "7318", "WIRE-18G": "7217", "CABLE-4C": "8544", "SVC-CRANE": "997313",
}

// p7Int parses a master id that masterID returned.
func p7Int(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("id %q: %v", s, err)
	}
	return n
}

// p7ID is the id of the master row with code in table.
func p7ID(t *testing.T, env *purchasetest.Env, table, code string) int64 {
	t.Helper()
	return p7Int(t, masterID(t, env.DB, table, code))
}

// p7Rate parses a rate literal; a bad literal is a test bug.
func p7Rate(t *testing.T, s string) types.Rate {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatalf("rate %q: %v", s, err)
	}
	return r
}

// p7Pct parses a percent literal; a bad literal is a test bug.
func p7Pct(t *testing.T, s string) types.Percent {
	t.Helper()
	p, err := types.ParsePercent(s)
	if err != nil {
		t.Fatalf("percent %q: %v", s, err)
	}
	return p
}

// p7Line is an order line of 10 (whole units: 1 for BOX) of item in unit
// at price less discount, tax GST18. A blank price or discount is posted
// blank.
func p7Line(t *testing.T, env *purchasetest.Env, itm, unit, price, discount string) map[string]string {
	t.Helper()
	qty := "10"
	if unit == "BOX" {
		qty = "1"
	}
	return map[string]string{
		"item_id": masterID(t, env.DB, "item", itm), "uom_id": masterID(t, env.DB, "uom", unit), "hsn_sac": p7HSN[itm],
		"qty": qty, "price": price, "discount_pct": discount, "tax_code_id": masterID(t, env.DB, "tax_code", "GST18"),
	}
}

// p7Priya builds a PO by Priya for vendor on METRO-SITE / PRJ-METRO, dated
// on (day-first), stopping where stop says.
func p7Priya(t *testing.T, env *purchasetest.Env, vendor, on string, stop func(*purchasetest.Spec), lines ...map[string]string) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, env, stop,
		purchasetest.OrderHeader(map[string]string{"vendor_id": masterID(t, env.DB, "party", vendor), "order_date": on}),
		purchasetest.OrderLines(lines...))
}

// p7Arjun builds a PO by Arjun (unrestricted, CD-257) for vendor on
// TECH-SITE / PRJ-TECHPARK, which Priya can't read (00-fixtures.md), dated
// on (ISO), through Engine.Do: saved, then submitted and approved when
// approve is true.
func p7Arjun(t *testing.T, env *purchasetest.Env, vendor, on string, approve bool, lines ...map[string]string) doc.Doc {
	t.Helper()
	ctx := t.Context()
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": masterID(t, env.DB, "party", vendor), "site_id": masterID(t, env.DB, "site", "TECH-SITE"),
			"project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK"), "order_date": on,
		},
		Lines: lines,
	})
	if err != nil {
		t.Fatalf("save Arjun's PRJ-TECHPARK PO: %v", err)
	}
	if approve {
		res = p7Step(t, env, env.Arjun, res, doctype.ActionSubmit)
		for res.State == doctype.StatePending {
			res = p7Step(t, env, p7Approver(t, env, res.ID), res, doctype.ActionApprove)
		}
		if res.State != doctype.StateApproved {
			t.Fatalf("Arjun's PO %d is %s, want approved", res.ID, res.State)
		}
	}
	return loadPO(t, env, res.ID)
}

// p7Step runs action a on the PO as p at its current version.
func p7Step(t *testing.T, env *purchasetest.Env, p auth.Principal, res doc.Result, a doctype.Action) doc.Result {
	t.Helper()
	next, err := env.Engine.Do(t.Context(), p, purchase.DocType, res.ID, a, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("%s PO %d as %s: %v", a, res.ID, p.Name, err)
	}
	return next
}

// p7Approver is the first fixture user APR lets approve PO id.
func p7Approver(t *testing.T, env *purchasetest.Env, id int64) auth.Principal {
	t.Helper()
	var ids []types.UserID
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		ids, err = env.Approvals.EligibleApprovers(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("approvers of PO %d: %v", id, err)
	}
	for _, uid := range ids {
		for _, p := range []auth.Principal{env.Admin, env.Meena, env.Ravi, env.Kavya, env.Deepak} {
			if p.ID == uid {
				return p
			}
		}
	}
	t.Fatalf("no fixture user may approve PO %d (eligible %v)", id, ids)
	return auth.Principal{}
}

// p7Price is what defaultPrice answers.
type p7Price struct {
	Price    types.Rate
	Discount types.Percent
	OK       bool
}

func (p p7Price) String() string {
	return fmt.Sprintf("price %s discount %s ok %t", p.Price.Plain(), p.Discount.Plain(), p.OK)
}

// p7Default runs defaultPrice for the vendor, item, unit and project codes
// in a read tx, as Validate runs it inside its write.
func p7Default(t *testing.T, env *purchasetest.Env, vendor, itm, unit, project string) p7Price {
	t.Helper()
	var got p7Price
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		got.Price, got.Discount, got.OK, err = purchase.DefaultPrice(t.Context(), tx,
			p7ID(t, env, "party", vendor), p7ID(t, env, "item", itm), p7ID(t, env, "uom", unit), p7ID(t, env, "project", project))
		return err
	}); err != nil {
		t.Fatalf("defaultPrice(%s, %s, %s, %s): %v", vendor, itm, unit, project, err)
	}
	return got
}

// p7Want checks defaultPrice for the codes: the price and discount given, or
// no price (ok false, zeros) when price is "".
func p7Want(t *testing.T, env *purchasetest.Env, step, vendor, itm, unit, project, price, discount string) {
	t.Helper()
	want := p7Price{}
	if price != "" {
		want = p7Price{Price: p7Rate(t, price), Discount: p7Pct(t, discount), OK: true}
	}
	if got := p7Default(t, env, vendor, itm, unit, project); got != want {
		t.Errorf("%s: defaultPrice(%s, %s, %s, %s) = %v, want %v", step, vendor, itm, unit, project, got, want)
	}
}

// p7SetStandardRate sets itm's standard rate through the item service as
// the Administrator, leaving the rest of the item as stored.
func p7SetStandardRate(t *testing.T, env *purchasetest.Env, itm, rate string) {
	t.Helper()
	ctx := t.Context()
	svc := item.New(mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock})
	id := p7ID(t, env, "item", itm)
	var it item.Item
	var conv []item.Conversion
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if it, err = svc.Get(ctx, tx, id); err != nil {
			return err
		}
		conv, err = svc.Conversions(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatalf("read item %s: %v", itm, err)
	}
	if _, err := svc.Update(ctx, env.Admin, id, it.Version, item.Input{
		Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC,
		StockUoMID: it.StockUoMID, PurchaseUoMID: it.PurchaseUoMID, DefaultTaxCodeID: it.DefaultTaxCodeID,
		IsStock: it.IsStock, StandardRate: p7Rate(t, rate), Conversions: conv,
	}); err != nil {
		t.Fatalf("set the standard rate of %s to %s: %v", itm, rate, err)
	}
}

// p7WantLine checks the stored price and discount of line n (1-based) of PO
// id; a blank discount isn't checked.
func p7WantLine(t *testing.T, env *purchasetest.Env, step string, id int64, n int, price, discount string) {
	t.Helper()
	d := loadPO(t, env, id)
	if len(d.Lines) < n {
		t.Fatalf("%s: PO %d has %d lines, want at least %d", step, id, len(d.Lines), n)
	}
	v := d.Lines[n-1].Values
	if got, ok := v["price"].(types.Rate); !ok || got != p7Rate(t, price) {
		t.Errorf("%s: PO %d line %d price = %v (%T), want %s", step, id, n, v["price"], v["price"], price)
	}
	if discount == "" {
		return
	}
	if got, ok := v["discount_pct"].(types.Percent); !ok || got != p7Pct(t, discount) {
		t.Errorf("%s: PO %d line %d discount_pct = %v (%T), want %s", step, id, n, v["discount_pct"], v["discount_pct"], discount)
	}
}

// p7Last runs LastPricesFor as p and fails the test on an error.
func p7Last(t *testing.T, env *purchasetest.Env, p auth.Principal, keys []purchase.ItemUoM) map[purchase.ItemUoM]purchase.LastPrice {
	t.Helper()
	got, err := env.Service.LastPricesFor(t.Context(), p, keys)
	if err != nil {
		t.Fatalf("LastPricesFor as %s: %v", p.Name, err)
	}
	return got
}

// PO-065 (PO spec §24.2, §26; CD-258 Q6 (a) newest only; AUTH-110 scope;
// MOD-PO P7): the default price of a line saved with a blank price is the
// price and discount of the newest line (latest order_date, then highest id)
// on an approved or closed PO with the same vendor, item, UoM and project,
// else the item's standard rate; LastPricesFor gives the newest price per
// item and UoM across vendors, read through Auth.Scope, so a PO the viewer
// can't read is never shown, and a key with no approved or closed PO is
// absent.
func TestPO_LastPrice(t *testing.T) {
	// Each of vendor, project and UoM misses on its own: a line that matches
	// on everything else is no default, and a newer line that differs only in
	// that one never replaces the matching one.
	t.Run("vendor, project and UoM each miss", func(t *testing.T) {
		env := purchasetest.New(t)
		p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "61.25", "2.5"), p7Line(t, env, "BOLT-M12", "NOS", "9", "0"))

		p7Want(t, env, "the matching line", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "61.25", "2.5")
		p7Want(t, env, "the matching line", "SHREE", "BOLT-M12", "NOS", "PRJ-METRO", "9", "0")
		p7Want(t, env, "vendor misses", "BHARAT", "MS-ANGLE-50", "KGS", "PRJ-METRO", "", "")
		p7Want(t, env, "project misses", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-TECHPARK", "", "")
		p7Want(t, env, "UoM misses", "SHREE", "BOLT-M12", "BOX", "PRJ-METRO", "", "")

		// Newer lines that differ in one key each.
		p7Priya(t, env, "BHARAT", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "64", "0"))
		p7Arjun(t, env, "SHREE", "2026-10-23", true, p7Line(t, env, "MS-ANGLE-50", "KGS", "66", "1"))
		p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "BOLT-M12", "BOX", "850", "0"))

		p7Want(t, env, "after newer lines of another vendor, project and UoM", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "61.25", "2.5")
		p7Want(t, env, "after newer lines of another UoM", "SHREE", "BOLT-M12", "NOS", "PRJ-METRO", "9", "0")
		p7Want(t, env, "the other vendor's own line", "BHARAT", "MS-ANGLE-50", "KGS", "PRJ-METRO", "64", "0")
		p7Want(t, env, "the other project's own line", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-TECHPARK", "66", "1")
		p7Want(t, env, "the other UoM's own line", "SHREE", "BOLT-M12", "BOX", "PRJ-METRO", "850", "0")
	})

	// A closed PO counts; a draft and a cancelled one don't (do and undo:
	// the approved PO counts until it is cancelled).
	t.Run("closed counts, draft and cancelled don't", func(t *testing.T) {
		env := purchasetest.New(t)
		p7Priya(t, env, "SHREE", "20-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "60", "0"))
		p7Want(t, env, "one approved PO", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "60", "0")

		closed := p7Priya(t, env, "SHREE", "21-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "62", "1"))
		p7Want(t, env, "a newer approved PO", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "62", "1")
		if d := terminal(t, env, closed, doctype.ActionClose); d.State != doctype.StateClosed {
			t.Fatalf("closed PO %d is %s, want closed", d.ID, d.State)
		}
		p7Want(t, env, "the newer PO closed", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "62", "1")

		p7Priya(t, env, "SHREE", "24-10-2026", purchasetest.StopAtDraft(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "70", "0"), p7Line(t, env, "WIRE-18G", "KGS", "96", "0"))
		p7Want(t, env, "a newer draft", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "62", "1")
		p7Want(t, env, "an item only on a draft", "SHREE", "WIRE-18G", "KGS", "PRJ-METRO", "", "")

		cancelled := p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "65", "3"), p7Line(t, env, "BOLT-M12", "NOS", "9", "0"))
		p7Want(t, env, "a newer approved PO, before its cancel", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "65", "3")
		p7Want(t, env, "an item only on that PO, before its cancel", "SHREE", "BOLT-M12", "NOS", "PRJ-METRO", "9", "0")
		if d := terminal(t, env, cancelled, doctype.ActionCancel); d.State != doctype.StateCancelled {
			t.Fatalf("cancelled PO %d is %s, want cancelled", d.ID, d.State)
		}
		p7Want(t, env, "that PO cancelled", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "62", "1")
		p7Want(t, env, "an item only on a cancelled PO", "SHREE", "BOLT-M12", "NOS", "PRJ-METRO", "", "")
	})

	// Newest is the latest order_date, then the highest id: a PO created
	// later with an earlier date loses, and on the same date the later one
	// (and on one PO the later line) wins.
	t.Run("tie-break", func(t *testing.T) {
		env := purchasetest.New(t)
		p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "63", "1"))
		p7Priya(t, env, "SHREE", "21-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "61", "0"))
		p7Want(t, env, "a later-created PO with an earlier date", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "63", "1")

		p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "64", "2"))
		p7Want(t, env, "a later-created PO on the same date", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "64", "2")

		p7Priya(t, env, "SHREE", "24-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "67", "0"), p7Line(t, env, "MS-ANGLE-50", "KGS", "68", "4"))
		p7Want(t, env, "two lines of the item on the newest PO", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "68", "4")
	})

	// Through Validate (P2 calls defaultPrice): a blank price takes the
	// newest matching line's price and discount, else the item's standard
	// rate; a price given is kept. Cancelling the newest PO brings back the
	// one before it.
	t.Run("a blank price on save", func(t *testing.T) {
		env := purchasetest.New(t)
		p7SetStandardRate(t, env, "MS-ANGLE-50", "58")
		p7SetStandardRate(t, env, "BOLT-M12", "8.5")
		p7Priya(t, env, "SHREE", "20-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "59", "0"))
		newest := p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "61.25", "2.5"), p7Line(t, env, "BOLT-M12", "BOX", "850", "0"))

		draft := p7Priya(t, env, "SHREE", "24-10-2026", purchasetest.StopAtDraft(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "", ""),
			p7Line(t, env, "BOLT-M12", "BOX", "", ""),
			p7Line(t, env, "BOLT-M12", "NOS", "", ""),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "70", "1"))
		p7WantLine(t, env, "blank price, last price on file", draft.ID, 1, "61.25", "2.5")
		p7WantLine(t, env, "blank price, the UoM's own last price", draft.ID, 2, "850", "0")
		p7WantLine(t, env, "blank price, no last price for the UoM", draft.ID, 3, "8.5", "")
		p7WantLine(t, env, "a price given", draft.ID, 4, "70", "1")

		other := p7Priya(t, env, "BHARAT", "24-10-2026", purchasetest.StopAtDraft(), p7Line(t, env, "MS-ANGLE-50", "KGS", "", ""))
		p7WantLine(t, env, "blank price, another vendor", other.ID, 1, "58", "")
		techpark := p7Arjun(t, env, "SHREE", "2026-10-24", false, p7Line(t, env, "MS-ANGLE-50", "KGS", "", ""))
		p7WantLine(t, env, "blank price, another project", techpark.ID, 1, "58", "")

		terminal(t, env, newest, doctype.ActionCancel)
		after := p7Priya(t, env, "SHREE", "24-10-2026", purchasetest.StopAtDraft(), p7Line(t, env, "MS-ANGLE-50", "KGS", "", ""))
		p7WantLine(t, env, "blank price after the newest PO is cancelled", after.ID, 1, "59", "0")
	})

	// LastPricesFor: the newest per item and UoM across vendors on approved
	// and closed POs, through Auth.Scope on the real auth.Service: Priya
	// (PRJ-METRO only) never sees Arjun's PRJ-TECHPARK PO, and a key whose
	// only POs are a draft or cancelled, or none, is absent.
	t.Run("LastPricesFor", func(t *testing.T) {
		env := purchasetest.New(t)
		key := func(itm, unit string) purchase.ItemUoM {
			return purchase.ItemUoM{ItemID: p7ID(t, env, "item", itm), UoMID: p7ID(t, env, "uom", unit)}
		}
		ms, box, nos, wire, cable, crane := key("MS-ANGLE-50", "KGS"), key("BOLT-M12", "BOX"), key("BOLT-M12", "NOS"),
			key("WIRE-18G", "KGS"), key("CABLE-4C", "MTR"), key("SVC-CRANE", "HRS")
		keys := []purchase.ItemUoM{ms, box, nos, wire, cable, crane}

		older := p7Priya(t, env, "SHREE", "20-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "60", "0"), p7Line(t, env, "BOLT-M12", "BOX", "900", "0"))
		older = terminal(t, env, older, doctype.ActionClose)
		bharat := p7Priya(t, env, "BHARAT", "22-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "64", "0"))
		hidden := p7Arjun(t, env, "RAJKOT", "2026-10-23", true,
			p7Line(t, env, "MS-ANGLE-50", "KGS", "66", "0"), p7Line(t, env, "CABLE-4C", "MTR", "150", "0"))
		p7Priya(t, env, "SHREE", "24-10-2026", purchasetest.StopAtDraft(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "70", "0"), p7Line(t, env, "BOLT-M12", "NOS", "9", "0"))
		cancelled := p7Priya(t, env, "COROM", "24-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "71", "0"), p7Line(t, env, "WIRE-18G", "KGS", "96", "0"))
		terminal(t, env, cancelled, doctype.ActionCancel)

		on := func(day int) types.Date { return types.Date{Year: 2026, Month: 10, Day: day} }
		closedBox := purchase.LastPrice{POID: older.ID, PODisplay: older.Number, VendorName: "Shree Steel Traders", Rate: p7Rate(t, "900"), OrderDate: on(20)}

		want := map[purchase.ItemUoM]purchase.LastPrice{
			ms:  {POID: bharat.ID, PODisplay: bharat.Number, VendorName: "Bharat Tools", Rate: p7Rate(t, "64"), OrderDate: on(22)},
			box: closedBox,
		}
		if got := p7Last(t, env, env.Priya, keys); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor as Priya (PRJ-METRO only) = %+v,\nwant %+v (Arjun's PRJ-TECHPARK PO hidden; NOS, WIRE-18G, CABLE-4C and SVC-CRANE absent)", got, want)
		}

		want = map[purchase.ItemUoM]purchase.LastPrice{
			ms:    {POID: hidden.ID, PODisplay: hidden.Number, VendorName: "Rajkot Castings", Rate: p7Rate(t, "66"), OrderDate: on(23)},
			box:   closedBox,
			cable: {POID: hidden.ID, PODisplay: hidden.Number, VendorName: "Rajkot Castings", Rate: p7Rate(t, "150"), OrderDate: on(23)},
		}
		if got := p7Last(t, env, env.Arjun, keys); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor as Arjun (unrestricted) = %+v,\nwant %+v (NOS, WIRE-18G and SVC-CRANE absent)", got, want)
		}

		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{cable}); len(got) != 0 {
			t.Errorf("LastPricesFor as Priya, only CABLE-4C (on a PO she can't read) = %+v, want an empty map", got)
		}
	})

	// AD-13: a line saved with no price (no last price, no standard rate) is
	// never the last price; both paths skip it for the older priced line.
	t.Run("a line with no price is skipped", func(t *testing.T) {
		env := purchasetest.New(t)
		wire := p7Key(t, env, "WIRE-18G", "KGS")
		unpriced := p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "WIRE-18G", "KGS", "", ""))
		if v := loadPO(t, env, unpriced.ID).Lines[0].Values["price"]; v != nil {
			t.Fatalf("setup: PO %d line 1 price = %v (%T), want none (WIRE-18G has no standard rate)", unpriced.ID, v, v)
		}
		priced := p7Priya(t, env, "SHREE", "20-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "WIRE-18G", "KGS", "90", "0"))

		p7Want(t, env, "a newer line with no price", "SHREE", "WIRE-18G", "KGS", "PRJ-METRO", "90", "0")
		want := map[purchase.ItemUoM]purchase.LastPrice{
			wire: {POID: priced.ID, PODisplay: priced.Number, VendorName: "Shree Steel Traders", Rate: p7Rate(t, "90"), OrderDate: p7On(20)},
		}
		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{wire}); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor with a newer line with no price = %+v,\nwant %+v", got, want)
		}
	})

	// On one date the higher PO id wins even when the other PO's line has the
	// higher line id: the earlier-created PO gets its MS-ANGLE-50 line in a
	// later save.
	t.Run("same date, the higher PO id beats a higher line id", func(t *testing.T) {
		env := purchasetest.New(t)
		ms := p7Key(t, env, "MS-ANGLE-50", "KGS")
		first := p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtDraft(), p7Line(t, env, "BOLT-M12", "NOS", "9", "0"))
		second := p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "64", "2"))
		res, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, first.ID, doctype.ActionSave, doc.ActionInput{
			Version: first.Version,
			Values: map[string]string{
				"vendor_id": masterID(t, env.DB, "party", "SHREE"), "site_id": masterID(t, env.DB, "site", "METRO-SITE"),
				"project_id": masterID(t, env.DB, "project", "PRJ-METRO"), "order_date": "2026-10-23",
			},
			Lines: []map[string]string{p7Line(t, env, "BOLT-M12", "NOS", "9", "0"), p7Line(t, env, "MS-ANGLE-50", "KGS", "63", "1")},
		})
		if err != nil {
			t.Fatalf("setup: add the MS-ANGLE-50 line to PO %d: %v", first.ID, err)
		}
		p7Approve(t, env, env.Priya, res)

		firstLine, secondLine := p7LineID(t, env, first.ID, ms), p7LineID(t, env, second.ID, ms)
		if first.ID >= second.ID || firstLine <= secondLine {
			t.Fatalf("setup: PO ids %d, %d and MS-ANGLE-50 line ids %d, %d; want the first PO's id lower and its line id higher",
				first.ID, second.ID, firstLine, secondLine)
		}

		p7Want(t, env, "same date, higher PO id", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "64", "2")
		want := map[purchase.ItemUoM]purchase.LastPrice{
			ms: {POID: second.ID, PODisplay: second.Number, VendorName: "Shree Steel Traders", Rate: p7Rate(t, "64"), OrderDate: p7On(23)},
		}
		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{ms}); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor, same date = %+v,\nwant %+v (the higher PO id)", got, want)
		}
	})

	// LastPricesFor's order: a PO created later with an earlier date loses
	// to the newer date, and on one PO the higher line id wins.
	t.Run("LastPricesFor order", func(t *testing.T) {
		env := purchasetest.New(t)
		ms := p7Key(t, env, "MS-ANGLE-50", "KGS")
		newer := p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "63", "0"))
		p7Priya(t, env, "BHARAT", "21-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "61", "0"))
		want := map[purchase.ItemUoM]purchase.LastPrice{
			ms: {POID: newer.ID, PODisplay: newer.Number, VendorName: "Shree Steel Traders", Rate: p7Rate(t, "63"), OrderDate: p7On(23)},
		}
		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{ms}); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor, a later-created PO with an earlier date = %+v,\nwant %+v (the newer date)", got, want)
		}

		two := p7Priya(t, env, "COROM", "24-10-2026", purchasetest.StopAtApproved(),
			p7Line(t, env, "MS-ANGLE-50", "KGS", "67", "0"), p7Line(t, env, "MS-ANGLE-50", "KGS", "68", "4"))
		want = map[purchase.ItemUoM]purchase.LastPrice{
			ms: {POID: two.ID, PODisplay: two.Number, VendorName: "Coromandel Cables", Rate: p7Rate(t, "68"), OrderDate: p7On(24)},
		}
		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{ms}); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor, two lines of the item on one PO = %+v,\nwant %+v (the higher line id)", got, want)
		}
	})

	// Only approved and closed count: a newer pending PO and a newer
	// rejected one (Ravi rejects) are ignored by both paths.
	t.Run("pending and rejected don't count", func(t *testing.T) {
		env := purchasetest.New(t)
		ms := p7Key(t, env, "MS-ANGLE-50", "KGS")
		base := p7Priya(t, env, "SHREE", "20-10-2026", purchasetest.StopAtApproved(), p7Line(t, env, "MS-ANGLE-50", "KGS", "60", "0"))
		// ₹50,000 before tax: the Purchase Manager rule applies.
		p7Priya(t, env, "SHREE", "22-10-2026", purchasetest.StopAtPending(), p7Line(t, env, "MS-ANGLE-50", "KGS", "5000", "1"))
		pending := p7Priya(t, env, "SHREE", "23-10-2026", purchasetest.StopAtPending(), p7Line(t, env, "MS-ANGLE-50", "KGS", "5100", "2"))
		if d := hoAct(t, env, env.Ravi, pending, doctype.ActionReject, "Rate too high"); d.State != doctype.StateRejected {
			t.Fatalf("setup: PO %d is %s, want rejected", d.ID, d.State)
		}

		p7Want(t, env, "newer pending and rejected POs", "SHREE", "MS-ANGLE-50", "KGS", "PRJ-METRO", "60", "0")
		want := map[purchase.ItemUoM]purchase.LastPrice{
			ms: {POID: base.ID, PODisplay: base.Number, VendorName: "Shree Steel Traders", Rate: p7Rate(t, "60"), OrderDate: p7On(20)},
		}
		if got := p7Last(t, env, env.Priya, []purchase.ItemUoM{ms}); !maps.Equal(got, want) {
			t.Errorf("LastPricesFor with newer pending and rejected POs = %+v,\nwant %+v", got, want)
		}
	})
}

// p7Key is the LastPricesFor key of the item and unit codes.
func p7Key(t *testing.T, env *purchasetest.Env, itm, unit string) purchase.ItemUoM {
	t.Helper()
	return purchase.ItemUoM{ItemID: p7ID(t, env, "item", itm), UoMID: p7ID(t, env, "uom", unit)}
}

// p7On is a day of October 2026.
func p7On(day int) types.Date { return types.Date{Year: 2026, Month: 10, Day: day} }

// p7Approve submits the saved PO as owner and approves it as each eligible
// fixture user until it is approved.
func p7Approve(t *testing.T, env *purchasetest.Env, owner auth.Principal, res doc.Result) {
	t.Helper()
	res = p7Step(t, env, owner, res, doctype.ActionSubmit)
	for res.State == doctype.StatePending {
		res = p7Step(t, env, p7Approver(t, env, res.ID), res, doctype.ActionApprove)
	}
	if res.State != doctype.StateApproved {
		t.Fatalf("PO %d is %s, want approved", res.ID, res.State)
	}
}

// p7LineID is the id of PO id's only line of key k.
func p7LineID(t *testing.T, env *purchasetest.Env, id int64, k purchase.ItemUoM) int64 {
	t.Helper()
	var found int64
	for _, l := range loadPO(t, env, id).Lines {
		if fmt.Sprint(l.Values["item_id"]) == strconv.FormatInt(k.ItemID, 10) && fmt.Sprint(l.Values["uom_id"]) == strconv.FormatInt(k.UoMID, 10) {
			if found != 0 {
				t.Fatalf("PO %d has two lines of item %d unit %d", id, k.ItemID, k.UoMID)
			}
			found = l.ID
		}
	}
	if found == 0 {
		t.Fatalf("PO %d has no line of item %d unit %d", id, k.ItemID, k.UoMID)
	}
	return found
}
