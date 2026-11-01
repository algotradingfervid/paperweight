package address_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/vardhan/internaltools/internal/platform/db"
	appmigrations "github.com/vardhan/internaltools/migrations"
)

// m058Version is the migration that lets a site own an address and a contact.
const m058Version = 10380

const m058Now = "2026-10-01T00:00:00.000Z"

// TestMigration_SiteOwnerKeepsData migrates a database holding party and
// branch addresses and contacts (with emails and phones) across the owner
// CHECK rebuild (CD-337 (1), M0-58): every row, child row, id and counter is
// kept, the foreign keys that point at the tables still resolve and still
// act, and the new CHECK takes exactly one of party, branch or site.
func TestMigration_SiteOwnerKeepsData(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(filepath.Join(t.TempDir(), "m058.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	// One connection: the checks below run on the one the migration used, so
	// they see its foreign_keys setting.
	d.SQL().SetMaxOpenConns(1)
	p, err := goose.NewProvider(goose.DialectSQLite3, d.SQL(), appmigrations.FS, goose.WithTableName("goose_db_version"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.UpTo(ctx, m058Version-1); err != nil {
		t.Fatalf("migrate to %d: %v", m058Version-1, err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.SQL().ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO party (id, code, name, is_vendor, created_at, updated_at) VALUES (1, 'SHREE', 'Shree Steel', 1, ?, ?)`, m058Now, m058Now)
	exec(`INSERT INTO branch (id, code, name, state_code, created_at, updated_at) VALUES (1, 'PUNE', 'Pune', '27', ?, ?)`, m058Now, m058Now)
	addr := `INSERT INTO address (id, party_id, branch_id, name, line1, city, state_code, pincode, gstin, is_billing, is_shipping,
		is_primary_billing, version, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, '27', ?, ?, 1, 0, 1, ?, ?, ?)`
	addr2 := strings.Replace(addr, "1, 0, 1, ?", "1, 0, 0, ?", 1)
	exec(addr, 3, 1, nil, "Mumbai", "Plot 12", "Mumbai", "400093", "27AABCS1429B1ZU", 4, m058Now, m058Now)
	exec(addr, 7, nil, 1, "Pune", "Works Road", "Pune", "411001", "", 2, m058Now, m058Now)
	contact := `INSERT INTO contact (id, party_id, branch_id, name, first_name, is_primary, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	exec(contact, 5, 1, nil, "Ramesh", "Ramesh", 1, m058Now, m058Now)
	exec(contact, 9, nil, 1, "Kavya", "Kavya", 1, m058Now, m058Now)
	exec(`INSERT INTO contact_email (contact_id, email, is_primary) VALUES (5, 'r@shree.test', 1)`)
	exec(`INSERT INTO contact_phone (contact_id, number, kind, is_primary) VALUES (5, '+919800000001', 'mobile', 1), (9, '+919800000002', 'mobile', 1)`)
	// Deleted rows above the kept ones: their ids must never come back.
	exec(contact, 12, 1, nil, "Gone", "Gone", 0, m058Now, m058Now)
	exec(`DELETE FROM contact WHERE id = 12`)
	exec(addr2, 15, 1, nil, "Gone", "x", "Mumbai", "", "", 1, m058Now, m058Now)
	exec(`DELETE FROM address WHERE id = 15`)

	if _, err := p.UpTo(ctx, m058Version); err != nil {
		t.Fatalf("migrate to %d: %v", m058Version, err)
	}

	query := func(q string) string {
		t.Helper()
		rows, err := d.SQL().QueryContext(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer func() { _ = rows.Close() }()
		cols, _ := rows.Columns()
		var out []string
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			var cells []string
			for _, v := range vals {
				if v == nil {
					cells = append(cells, "NULL")
					continue
				}
				switch x := v.(type) {
				case []byte:
					cells = append(cells, string(x))
				default:
					cells = append(cells, strings.TrimSpace(strings.ReplaceAll(fmt.Sprint(x), "\n", " ")))
				}
			}
			out = append(out, strings.Join(cells, ","))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return strings.Join(out, ";")
	}
	checks := []struct{ q, want string }{
		{`SELECT id, party_id, branch_id, site_id, name, line1, city, state_code, pincode, gstin, is_billing, is_primary_billing, version FROM address ORDER BY id`,
			"3,1,NULL,NULL,Mumbai,Plot 12,Mumbai,27,400093,27AABCS1429B1ZU,1,1,4;7,NULL,1,NULL,Pune,Works Road,Pune,27,411001,,1,1,2"},
		{`SELECT id, party_id, branch_id, site_id, name, is_primary FROM contact ORDER BY id`, "5,1,NULL,NULL,Ramesh,1;9,NULL,1,NULL,Kavya,1"},
		{`SELECT contact_id, email FROM contact_email ORDER BY id`, "5,r@shree.test"},
		{`SELECT contact_id, number FROM contact_phone ORDER BY id`, "5,+919800000001;9,+919800000002"},
		{`SELECT name, seq FROM sqlite_sequence WHERE name IN ('address', 'contact', 'address_old', 'contact_old') ORDER BY name`, "address,15;contact,12"},
		{`SELECT name FROM sqlite_schema WHERE name LIKE '%_old'`, ""},
		{`SELECT name FROM sqlite_schema WHERE type = 'index' AND tbl_name IN ('address', 'contact') AND sql IS NOT NULL ORDER BY name`,
			"address_branch_primary_billing;address_branch_primary_shipping;address_gstin;address_party_primary_billing;address_party_primary_shipping;address_site;" +
				"contact_branch_primary;contact_party_primary;contact_site"},
		{`SELECT DISTINCT "table" FROM pragma_foreign_key_list('contact_phone')`, "contact"},
		{`SELECT DISTINCT "table" FROM pragma_foreign_key_list('contact_email')`, "contact"},
		{`SELECT "table" FROM pragma_foreign_key_list('purchase_order') WHERE "from" = 'vendor_address_id'`, "address"},
		{`PRAGMA foreign_key_check`, ""},
		{`PRAGMA foreign_keys`, "1"},
	}
	for _, c := range checks {
		if got := query(c.q); got != c.want {
			t.Errorf("%s\n got %q\nwant %q", c.q, got, c.want)
		}
	}
	// The cascade from contact to its phones still acts.
	exec(`DELETE FROM contact WHERE id = 9`)
	if got := query(`SELECT contact_id FROM contact_phone ORDER BY id`); got != "5" {
		t.Errorf("phones after deleting contact 9 = %q; want only contact 5's", got)
	}
	// A new row gets an id past every id ever used.
	exec(`INSERT INTO contact (party_id, name, first_name, created_at, updated_at) VALUES (1, 'New', 'New', ?, ?)`, m058Now, m058Now)
	if got := query(`SELECT max(id) FROM contact`); got != "13" {
		t.Errorf("new contact id = %s; want 13", got)
	}

	// The CHECK: a site may own a row; none or two owners are refused; a site has at most one of each.
	exec(`INSERT INTO site (id, code, name, branch_id, type, created_at, updated_at) VALUES (1, 'S1', 'Site 1', 1, 'site', ?, ?)`, m058Now, m058Now)
	siteAddr := `INSERT INTO address (party_id, branch_id, site_id, name, line1, city, state_code, is_shipping, created_at, updated_at)
		VALUES (?, ?, ?, 'Pune', 'Gate 2', 'Pune', '27', 1, ?, ?)`
	exec(siteAddr, nil, nil, 1, m058Now, m058Now)
	siteContact := `INSERT INTO contact (party_id, branch_id, site_id, name, first_name, created_at, updated_at) VALUES (?, ?, ?, 'Raj', 'Raj', ?, ?)`
	exec(siteContact, nil, nil, 1, m058Now, m058Now)
	refused := []struct {
		name string
		q    string
		args []any
	}{
		{"address with no owner", siteAddr, []any{nil, nil, nil, m058Now, m058Now}},
		{"address of party and site", siteAddr, []any{1, nil, 1, m058Now, m058Now}},
		{"address of branch and site", siteAddr, []any{nil, 1, 1, m058Now, m058Now}},
		{"address of party and branch", siteAddr, []any{1, 1, nil, m058Now, m058Now}},
		{"second address of a site", siteAddr, []any{nil, nil, 1, m058Now, m058Now}},
		{"contact with no owner", siteContact, []any{nil, nil, nil, m058Now, m058Now}},
		{"contact of party and site", siteContact, []any{1, nil, 1, m058Now, m058Now}},
		{"contact of branch and site", siteContact, []any{nil, 1, 1, m058Now, m058Now}},
		{"contact of party and branch", siteContact, []any{1, 1, nil, m058Now, m058Now}},
		{"second contact of a site", siteContact, []any{nil, nil, 1, m058Now, m058Now}},
		{"address of a missing site", siteAddr, []any{nil, nil, 99, m058Now, m058Now}},
	}
	for _, r := range refused {
		if _, err := d.SQL().ExecContext(ctx, r.q, r.args...); err == nil {
			t.Errorf("%s: inserted; want refused", r.name)
		}
	}
	// A site's rows go with it.
	exec(`DELETE FROM site WHERE id = 1`)
	if got := query(`SELECT count(*) FROM address WHERE site_id IS NOT NULL`) + "/" + query(`SELECT count(*) FROM contact WHERE site_id IS NOT NULL`); got != "0/0" {
		t.Errorf("site rows after deleting the site = %s; want 0/0", got)
	}

	// Down drops the site rows, their phones with them, and keeps the rest.
	exec(`INSERT INTO site (id, code, name, branch_id, type, created_at, updated_at) VALUES (2, 'S2', 'Site 2', 1, 'site', ?, ?)`, m058Now, m058Now)
	exec(siteAddr, nil, nil, 2, m058Now, m058Now)
	exec(siteContact, nil, nil, 2, m058Now, m058Now)
	exec(`INSERT INTO contact_phone (contact_id, number, kind, is_primary) SELECT id, '+919800000003', 'mobile', 1 FROM contact WHERE site_id = 2`)
	if _, err := p.DownTo(ctx, m058Version-1); err != nil {
		t.Fatalf("down to %d: %v", m058Version-1, err)
	}
	for q, want := range map[string]string{
		`SELECT id FROM address ORDER BY id`:           "3;7",
		`SELECT id FROM contact ORDER BY id`:           "5;13",
		`SELECT number FROM contact_phone ORDER BY id`: "+919800000001",
		`PRAGMA foreign_key_check`:                     "",
		`PRAGMA foreign_keys`:                          "1",
	} {
		if got := query(q); got != want {
			t.Errorf("after down: %s = %q; want %q", q, got, want)
		}
	}
}
