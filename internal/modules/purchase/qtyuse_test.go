package purchase_test

import (
	"errors"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// masterID is the id of the master row with code in table.
func masterID(t *testing.T, d *db.DB, table, code string) string {
	t.Helper()
	var id int64
	if err := d.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil {
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// PO-016 (PO spec §3.2, §18, §20.2; 06-masters.md MST-092): the package init
// calls uom.RegisterQtyUse for purchase_order_line and purchase_request_line,
// so turning whole-number on for a UoM that an approved PO line or a submitted
// request line holds a fractional quantity in is refused, and the UoM is left
// as it was.
func TestPO_QtyUseRegistered(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := uom.New(mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock})

	get := func(code string) uom.UoM {
		t.Helper()
		id, err := strconv.ParseInt(masterID(t, env.DB, "uom", code), 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		var u uom.UoM
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			u, err = svc.Get(ctx, tx, id)
			return err
		}); err != nil {
			t.Fatalf("get UoM %s: %v", code, err)
		}
		return u
	}

	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{
		"item_id": masterID(t, env.DB, "item", "CABLE-4C"), "uom_id": masterID(t, env.DB, "uom", "MTR"), "qty": "12.500",
	}))
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtApproved(), purchasetest.RequestLines(map[string]string{
		"item_id": masterID(t, env.DB, "item", "MS-ANGLE-50"), "uom_id": masterID(t, env.DB, "uom", "KGS"), "qty": "2.500",
	}))
	if po.Number != "PO/26-27/00001" || pr.Number != "PR/26-27/00001" {
		t.Fatalf("numbers = %q, %q; want PO/26-27/00001, PR/26-27/00001", po.Number, pr.Number)
	}

	for _, c := range []struct{ code, msg string }{
		{"MTR", "MTR is used with fractional quantities (e.g. PO/26-27/00001). Create a new UoM instead."},
		{"KGS", "KGS is used with fractional quantities (e.g. PR/26-27/00001). Create a new UoM instead."},
	} {
		before := get(c.code)
		if before.WholeNumber {
			t.Fatalf("%s is whole-number before the test", c.code)
		}
		_, err := svc.Update(ctx, env.Admin, before.ID, before.Version,
			uom.Input{Code: before.Code, Name: before.Name, UQC: before.UQC, WholeNumber: true})
		want := doctype.FieldError{Field: "whole_number", Message: c.msg}
		var ve *mstkit.ValidationError
		if !errors.As(err, &ve) || len(ve.Fields) != 1 || ve.Fields[0] != want {
			t.Errorf("turn whole-number on for %s: err = %v; want %+v (MST-092)", c.code, err, want)
		}
		if after := get(c.code); after != before {
			t.Errorf("%s after the refusal = %+v, want unchanged %+v", c.code, after, before)
		}
	}

	// A UoM no purchase line uses in a fraction still turns whole-number on.
	ltr := get("LTR")
	if _, err := svc.Update(ctx, env.Admin, ltr.ID, ltr.Version,
		uom.Input{Code: ltr.Code, Name: ltr.Name, UQC: ltr.UQC, WholeNumber: true}); err != nil {
		t.Errorf("turn whole-number on for unused LTR: %v, want ok", err)
	}
}
