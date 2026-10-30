package testkit

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The longest values the masters' own rules let through. Codes and GSTINs
// stop at the field's MaxLen; names are uitest's, under mstkit.MaxNameLen.
var (
	longCode  = uitest.LongCode60[:20]
	longGSTIN = uitest.LongCode60[:15]
)

const (
	longPhone   = "+91 98765 43210 98765"
	longPincode = "411001"
	longHSN     = "72169999"
	longTaxRate = 2800 // 28%, as tax_code stores it
)

// LongRows are the ids of SeedLongValues' rows, one per master.
type LongRows struct {
	Party, Address, Contact int64
	Item, Project, Site     int64
	Employee, ExpenseType   int64
	Branch, Terms           int64
	UoM, TaxCode            int64
	User                    int64
}

// SeedLongValues adds one row per master holding uitest's worst-case strings
// (U6), for the overflow sweep. Like SeedMasters it writes plain SQL, after
// SeedMasters. The user has a 253-character email and no roles.
func SeedLongValues(t testing.TB, d *db.DB, fx Fixtures) LongRows {
	t.Helper()
	ctx := context.Background()
	var r LongRows
	err := d.Write(ctx, func(tx *db.Tx) error {
		s := seeder{ctx: ctx, q: tx.SQL(), fx: &fx, at: types.TimestampOf(Clock().Now()).String()}
		return s.longValues(&r)
	})
	if err != nil {
		t.Fatalf("testkit.SeedLongValues: %v", err)
	}
	return r
}

func (s seeder) longValues(r *LongRows) error {
	const long, devanagari = uitest.LongName100, uitest.LongDevanagari
	var err error
	add := func(id *int64, table, columns string, values ...any) {
		if err == nil {
			*id, err = s.insert(table, columns, values...)
		}
	}
	home, stock := s.fx.Branches[0], s.fx.Units[0].ID
	add(&r.Terms, "payment_terms", "code, name", longCode, long)
	add(&r.UoM, "uom", "code, name, uqc", longCode, long, longCode[:3])
	add(&r.TaxCode, "tax_code", "code, name, treatment, rate", longCode, long, "Taxable", longTaxRate)
	add(&r.Branch, "branch", "code, name, state_code, gstin, is_head_office", longCode, long, home.StateCode, longGSTIN, false)
	add(&r.Party, "party", "code, name, is_vendor, is_customer, pan, gst_category, default_payment_terms_id",
		longCode, long, true, true, longCode[:10], "Registered Regular", r.Terms)
	add(&r.Address, "address", "party_id, name, title, line1, line2, city, state_code, pincode, gstin, is_billing, is_primary_billing",
		r.Party, long, long, long, devanagari, long, home.StateCode, longPincode, longGSTIN, true, true)
	add(&r.Contact, "contact", "party_id, name, first_name, last_name, is_primary", r.Party, long, long, devanagari, true)
	add(&r.Item, "item", "code, name, item_type, hsn_sac, stock_uom_id, purchase_uom_id, default_tax_code_id, is_stock",
		longCode, long, "Goods", longHSN, stock, stock, r.TaxCode, true)
	add(&r.Project, "project", "code, name, branch_id, customer_id", longCode, long, r.Branch, r.Party)
	add(&r.Site, "site", "code, name, branch_id, project_id, type", longCode, long, r.Branch, r.Project, "site")
	add(&r.ExpenseType, "expense_type", "code, name, default_hsn_sac, default_tax_code_id", longCode, devanagari, longHSN, r.TaxCode)
	add(&r.User, "users", "email, name, mobile, enabled", uitest.LongEmail, long, longPhone, true)
	add(&r.Employee, "employee", "code, name, branch_id, user_id, joining_date, status", longCode, long, r.Branch, r.User, seedJoiningDate, "Active")
	if err != nil {
		return err
	}
	if err := s.itemUoM(r.Item, 1, stock, factorOne); err != nil {
		return err
	}
	if _, err := s.q.ExecContext(s.ctx, `INSERT INTO contact_email (contact_id, email, is_primary) VALUES (?, ?, 1)`, r.Contact, uitest.LongEmail); err != nil {
		return err
	}
	_, err = s.q.ExecContext(s.ctx, `INSERT INTO contact_phone (contact_id, number, kind, is_primary) VALUES (?, ?, 'mobile', 1)`, r.Contact, longPhone)
	return err
}
