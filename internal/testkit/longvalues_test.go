package testkit_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// One read-back per master: a row that holds the short canonical values proves
// nothing in the sweep.
func TestTestkit_SeedLongValues(t *testing.T) {
	d := testkit.NewDB(t)
	r := testkit.SeedLongValues(t, d, testkit.SeedMasters(t, d), testkit.LongValues{
		Name: uitest.LongName100, Devanagari: uitest.LongDevanagari, Code60: uitest.LongCode60, Email: uitest.LongEmail,
	})
	rows := []struct {
		table, column string
		id            int64
		want          string
	}{
		{"party", "name", r.Party, uitest.LongName100},
		{"address", "line1", r.Address, uitest.LongName100},
		{"address", "line2", r.Address, uitest.LongDevanagari},
		{"contact", "first_name", r.Contact, uitest.LongName100},
		{"item", "name", r.Item, uitest.LongName100},
		{"project", "name", r.Project, uitest.LongName100},
		{"site", "name", r.Site, uitest.LongName100},
		{"employee", "name", r.Employee, uitest.LongName100},
		{"expense_type", "name", r.ExpenseType, uitest.LongDevanagari},
		{"branch", "name", r.Branch, uitest.LongName100},
		{"payment_terms", "name", r.Terms, uitest.LongName100},
		{"uom", "name", r.UoM, uitest.LongName100},
		{"tax_code", "name", r.TaxCode, uitest.LongName100},
		{"users", "email", r.User, uitest.LongEmail},
	}
	for _, c := range rows {
		var got string
		err := d.SQL().QueryRowContext(context.Background(), `SELECT `+c.column+` FROM `+c.table+` WHERE id = ?`, c.id).Scan(&got) //nolint:gosec // G202: the table above
		if err != nil {
			t.Errorf("%s.%s: %v", c.table, c.column, err)
		} else if got != c.want {
			t.Errorf("%s.%s = %q, want %q", c.table, c.column, got, c.want)
		}
	}
	var email, phone int
	if err := d.SQL().QueryRowContext(context.Background(),
		`SELECT (SELECT count(*) FROM contact_email WHERE contact_id = ? AND email = ?), (SELECT count(*) FROM contact_phone WHERE contact_id = ?)`,
		r.Contact, uitest.LongEmail, r.Contact).Scan(&email, &phone); err != nil {
		t.Fatal(err)
	}
	if email != 1 || phone != 1 {
		t.Errorf("the contact has %d long emails and %d phones, want 1 and 1", email, phone)
	}
}
