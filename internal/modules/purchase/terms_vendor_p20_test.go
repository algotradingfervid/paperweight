package purchase_test

import (
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-PO P20 (CD-309 (4), mirroring the address reset of CD-280 D7 (d)):
// when a draft's vendor changes, payment_terms_id resets to the new vendor's
// default (blank when it has none) if the stored terms equal the old
// vendor's default; terms the user chose stay. snap_terms_name follows the
// terms. Each case runs on a full save (the form posts the stored terms
// back with the new vendor) and on a step save of the main step (Only
// vendor_id; the terms are not posted).

// p20Terms creates payment terms code (100% at 30 days) and returns its id.
func p20Terms(t *testing.T, env *purchasetest.Env, code, name string) int64 {
	t.Helper()
	rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := paymentterms.New(p2Kit(env)).Create(t.Context(), env.Admin, paymentterms.Input{Code: code, Name: name, Rows: rows})
	if err != nil {
		t.Fatalf("create terms %s: %v", code, err)
	}
	return saved.ID
}

// p20SetDefaultTerms sets party code's default payment terms through the
// party service, as TestPO_PaymentTermsDefault does.
func p20SetDefaultTerms(t *testing.T, env *purchasetest.Env, code string, termsID int64) {
	t.Helper()
	ctx := t.Context()
	parties := party.New(p2Kit(env))
	var pt party.Party
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		pt, err = parties.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "party", code)))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := parties.Update(ctx, env.Admin, pt.ID, pt.Version, party.Input{Code: pt.Code, Name: pt.Name, IsVendor: pt.IsVendor,
		IsCustomer: pt.IsCustomer, PAN: pt.PAN, GSTCategory: pt.GSTCategory, DefaultPaymentTermsID: termsID}); err != nil {
		t.Fatalf("set %s's default terms: %v", code, err)
	}
}

// p20WantTerms asserts order id's payment_terms_id and snap_terms_name
// (wantID 0 = both blank).
func p20WantTerms(t *testing.T, what string, env *purchasetest.Env, id, wantID int64, wantName string) {
	t.Helper()
	want := ""
	if wantID != 0 {
		want = strconv.FormatInt(wantID, 10)
	}
	got := p2Snap(t, env, id, "payment_terms_id", "snap_terms_name")
	if got["payment_terms_id"] != want || got["snap_terms_name"] != wantName {
		t.Errorf("%s: payment_terms_id %q snap_terms_name %q, want %q %q", what, got["payment_terms_id"], got["snap_terms_name"], want, wantName)
	}
}

// p20SwitchVendor switches draft id to vendor code, by a full save (posting
// the stored terms back, as the form does) or a step save of the main step,
// and checks the switch committed.
func p20SwitchVendor(t *testing.T, env *purchasetest.Env, id int64, code string, step bool) {
	t.Helper()
	vendor := masterID(t, env.DB, "party", code)
	if step {
		if _, err := p2Step(t, env, env.Priya, id, []string{"vendor_id"}, map[string]string{"vendor_id": vendor}, nil); err != nil {
			t.Fatalf("step save to %s: %v", code, err)
		}
	} else {
		stored := p2Snap(t, env, id, "payment_terms_id")["payment_terms_id"]
		p2Must(t, "save to "+code)(p2Save(t, env, env.Priya, id, map[string]string{"vendor_id": vendor, "payment_terms_id": stored}, nil))
	}
	if got := p2Snap(t, env, id, "snap_vendor_id")["snap_vendor_id"]; got != vendor {
		t.Fatalf("switch to %s: snap_vendor_id %q, want %s", code, got, vendor)
	}
}

var p20SavePaths = []struct {
	name string
	step bool
}{{"full save", false}, {"step save", true}}

// Tests 1 and 4: a SHREE draft holding SHREE's default terms (defaulted on
// save) switched to BHARAT, whose default differs, takes BHARAT's default.
func TestPO_VendorChangeResetsDefaultTerms(t *testing.T) {
	for _, p := range p20SavePaths {
		t.Run(p.name, func(t *testing.T) {
			env := purchasetest.New(t)
			net30, net45 := p20Terms(t, env, "NET30", "Net 30 days"), p20Terms(t, env, "NET45", "Net 45 days")
			p20SetDefaultTerms(t, env, "SHREE", net30)
			p20SetDefaultTerms(t, env, "BHARAT", net45)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
			p20WantTerms(t, "SHREE draft", env, po.ID, net30, "Net 30 days")

			p20SwitchVendor(t, env, po.ID, "BHARAT", p.step)
			p20WantTerms(t, "switched to BHARAT", env, po.ID, net45, "Net 45 days")
		})
	}
}

// Tests 2 and 4: the same switch to a vendor with no default (BHARAT, left
// without one) blanks the terms and snap_terms_name.
func TestPO_VendorChangeClearsDefaultTerms(t *testing.T) {
	for _, p := range p20SavePaths {
		t.Run(p.name, func(t *testing.T) {
			env := purchasetest.New(t)
			net30 := p20Terms(t, env, "NET30", "Net 30 days")
			p20SetDefaultTerms(t, env, "SHREE", net30)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderHeader(map[string]string{"payment_terms_id": ""}))
			p20WantTerms(t, "SHREE draft", env, po.ID, net30, "Net 30 days")

			p20SwitchVendor(t, env, po.ID, "BHARAT", p.step)
			p20WantTerms(t, "switched to BHARAT (no default)", env, po.ID, 0, "")
		})
	}
}

// Tests 3 and 4: terms the user chose (not SHREE's default) stay through the
// switch to BHARAT, which has its own default.
func TestPO_VendorChangeKeepsChosenTerms(t *testing.T) {
	for _, p := range p20SavePaths {
		t.Run(p.name, func(t *testing.T) {
			env := purchasetest.New(t)
			net30, net45 := p20Terms(t, env, "NET30", "Net 30 days"), p20Terms(t, env, "NET45", "Net 45 days")
			net60 := p20Terms(t, env, "NET60", "Net 60 days")
			p20SetDefaultTerms(t, env, "SHREE", net30)
			p20SetDefaultTerms(t, env, "BHARAT", net45)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(),
				purchasetest.OrderHeader(map[string]string{"payment_terms_id": strconv.FormatInt(net60, 10)}))
			p20WantTerms(t, "SHREE draft with chosen terms", env, po.ID, net60, "Net 60 days")

			p20SwitchVendor(t, env, po.ID, "BHARAT", p.step)
			p20WantTerms(t, "switched to BHARAT", env, po.ID, net60, "Net 60 days")
		})
	}
}
