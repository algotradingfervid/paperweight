package purchase_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-PO P2 (PO spec §4 "Vendor's state unknown"; NUM-038): a vendor with
// neither a GSTIN nor an address state (an unregistered vendor with no
// address) is refused on save with NUM-038's text, and nothing is written.
func TestPO_VendorStateUnknown(t *testing.T) {
	env := purchasetest.New(t)
	if _, err := party.New(p2Kit(env)).Create(t.Context(), env.Admin, party.Input{Code: "NOADDR", Name: "Sai Fabricators", IsVendor: true}); err != nil {
		t.Fatalf("create vendor NOADDR: %v", err)
	}
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "NOADDR", "METRO-SITE", "PRJ-METRO"), p2RefLines(t, env))
	p2WantInvalid(t, "vendor with no state", err, doctype.FieldError{Field: "vendor_address_id|vendor_id",
		Message: "Set the state on the supplier's address so GST can be worked out."})
	p2SameCounts(t, "vendor with no state", before, env)
}

// MOD-PO P2 (PO spec §3.1 payment_terms_id, §3.1 snap_terms_name; MST-050):
// blank payment terms take the vendor's default terms and their name is
// snapshotted; terms chosen on the PO are kept; cleared, the vendor's come
// back. A vendor with no default leaves them blank.
func TestPO_PaymentTermsDefault(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	kit := p2Kit(env)
	terms := func(code, name string) int64 {
		t.Helper()
		rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
		if err != nil {
			t.Fatal(err)
		}
		saved, err := paymentterms.New(kit).Create(ctx, env.Admin, paymentterms.Input{Code: code, Name: name, Rows: rows})
		if err != nil {
			t.Fatalf("create terms %s: %v", code, err)
		}
		return saved.ID
	}
	net30, net45 := terms("NET30", "Net 30 days"), terms("NET45", "Net 45 days")

	// No vendor default yet: blank stays blank.
	none := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
	if got := p2Snap(t, env, none.ID, "payment_terms_id", "snap_terms_name"); got["payment_terms_id"] != "" || got["snap_terms_name"] != "" {
		t.Errorf("vendor without default terms: %v, want both blank", got)
	}

	parties := party.New(kit)
	var pt party.Party
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		pt, err = parties.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "party", "SHREE")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := parties.Update(ctx, env.Admin, pt.ID, pt.Version, party.Input{Code: pt.Code, Name: pt.Name, IsVendor: pt.IsVendor,
		IsCustomer: pt.IsCustomer, PAN: pt.PAN, GSTCategory: pt.GSTCategory, DefaultPaymentTermsID: net30}); err != nil {
		t.Fatalf("set SHREE's default terms: %v", err)
	}

	check := func(what string, id, wantID int64, wantName string) {
		t.Helper()
		got := p2Snap(t, env, id, "payment_terms_id", "snap_terms_name")
		if got["payment_terms_id"] != strconv.FormatInt(wantID, 10) || got["snap_terms_name"] != wantName {
			t.Errorf("%s: payment_terms_id %q snap_terms_name %q, want %d %q", what, got["payment_terms_id"], got["snap_terms_name"], wantID, wantName)
		}
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
	check("blank terms", po.ID, net30, "Net 30 days")
	p2Must(t, "choose NET45")(p2Save(t, env, env.Priya, po.ID, map[string]string{"payment_terms_id": strconv.FormatInt(net45, 10)}, nil))
	check("chosen terms", po.ID, net45, "Net 45 days")
	p2Must(t, "clear the terms")(p2Save(t, env, env.Priya, po.ID, map[string]string{"payment_terms_id": ""}, nil))
	check("cleared terms", po.ID, net30, "Net 30 days")
}

// MOD-PO P2 (PO spec §3.2 amount = gstcalc.LineAmount(qty, rate); NUM): a
// line whose amount passes the money limit is refused with types' text,
// prefixed with its line, and nothing is written.
func TestPO_LineAmountTooLarge(t *testing.T) {
	env := purchasetest.New(t)
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"),
		p2Line(t, env, "MS-ANGLE-50", "KGS", "1000000", "100000"), // ₹10,00,00,00,00,000.00
	})
	p2WantInvalid(t, "line amount over the limit", err, doctype.FieldError{Field: "qty|price|amount", Line: 2,
		Message: "Line 2: Line amount is too large. The most a line can be is ₹10,00,00,00,000.00."})
	p2SameCounts(t, "line amount over the limit", before, env)
}

// MOD-PO P2 (CD-178; PO spec §3.2 R8): Required isn't checked on a draft, so
// a draft with no order date, vendor, site or project (by an unrestricted
// user) still saves, with no lines or with a line; Validate's checks skip
// what is blank.
func TestPO_BlankDraftSaves(t *testing.T) {
	env := purchasetest.New(t)
	blank := map[string]string{"vendor_id": "", "site_id": "", "project_id": "", "order_date": ""}
	for _, c := range []struct {
		what  string
		lines []map[string]string
	}{{"no lines", nil}, {"one line", []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")}}} {
		res, err := p2Create(t, env, env.Arjun, blank, c.lines)
		if err != nil {
			t.Errorf("blank draft, %s: %v, want saved", c.what, err)
			continue
		}
		if d := p2Load(t, env, res.ID); d.State != doctype.StateDraft || len(d.Lines) != len(c.lines) {
			t.Errorf("blank draft, %s: state %q with %d lines, want draft with %d", c.what, d.State, len(d.Lines), len(c.lines))
		}
	}
}
