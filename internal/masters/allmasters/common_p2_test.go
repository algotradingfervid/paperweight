package allmasters_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	msgCodeChange     = "Only a Masters Manager can change a code."
	msgVendorDisabled = "Vendor SHREE Shree Steel Traders is disabled. Pick an active vendor, or ask an admin to enable it."
	msgStaleItem      = "Someone else changed Item MS-ANGLE-50 after you opened it. Reload it and make your changes again."
	msgEnableAddress  = "Enable this address before making it primary."
	msgEnableContact  = "Enable this contact before making it primary."
)

// sqlExec runs one statement outside the services, to set up what they refuse.
func sqlExec(t *testing.T, env *msttest.Env, stmt string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, stmt, args...)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

func wantStale(t *testing.T, err error) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindStale {
		t.Errorf("save = %v (%T); want DOC's stale error", err, err)
	}
}

// TestMaster_CodeUniqueConcurrent is MST-005. The writes serialise (BEGIN
// IMMEDIATE), so the loser's check reads the winner's row; the unique index
// holds even past that check.
func TestMaster_CodeUniqueConcurrent(t *testing.T) {
	env, m, svc := seeded(t)
	const msg = "Code NEWPARTY is already used by Party New Party. Choose another code."
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			<-start
			_, errs[i] = svc.Party.Create(context.Background(), env.Kavya, party.Input{Code: "NEWPARTY", Name: "New Party", IsVendor: true})
		})
	}
	close(start)
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("creates = %v, %v; want exactly one success", errs[0], errs[1])
	}
	loser := errs[0]
	if loser == nil {
		loser = errs[1]
	}
	if !hasFieldError(loser, doctype.FieldError{Field: "code", Message: msg}) {
		t.Errorf("loser = %v; want %q", loser, msg)
	}
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM party WHERE code = 'NEWPARTY'`).Scan(&n)
	})
	if n != 1 {
		t.Errorf("rows with code NEWPARTY = %d; want 1", n)
	}
	ctx := context.Background()
	err := env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, `UPDATE party SET code = 'NEWPARTY' WHERE id = ?`, m.Parties["SHREE"])
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
		t.Errorf("duplicate code past the pre-check = %v; want the unique index's error", err)
	}
}

// TestMaster_RenameNeedsRight is MST-008.
func TestMaster_RenameNeedsRight(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	a, ok := env.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth = %T; want *auth.Auth", env.Kit.Auth)
	}
	if err := a.SetRights(ctx, env.Admin, "Purchase Manager", party.DocType, map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll,
	}); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	p := getParty(t, env, svc, m.Parties["SHREE"])
	in := partyInput(p)
	in.Code = "SHREE-STL"
	_, err := svc.Party.Update(ctx, env.Ravi, p.ID, p.Version, in)
	if !hasFieldError(err, doctype.FieldError{Field: "code", Message: msgCodeChange}) {
		t.Errorf("Ravi's rename = %v; want %q", err, msgCodeChange)
	}
	if got := getParty(t, env, svc, p.ID); got.Code != "SHREE" || got.Version != p.Version {
		t.Errorf("after refused rename: %+v; want SHREE at version %d", got, p.Version)
	}
	if _, err := svc.Party.Update(ctx, env.Kavya, p.ID, p.Version, in); err != nil {
		t.Fatalf("Kavya's rename: %v", err)
	}
	if got := getParty(t, env, svc, p.ID); got.Code != "SHREE-STL" {
		t.Errorf("code = %q; want SHREE-STL", got.Code)
	}
}

// TestMaster_AmendWithDisabledMaster is MST-012.
func TestMaster_AmendWithDisabledMaster(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	id := order(t, env, m, "MS-ANGLE-50")
	mstfixture.Approve(t, env, id)
	mstfixture.Cancel(t, env, id)
	shree := m.Parties["SHREE"]
	if err := svc.Party.SetDisabled(ctx, env.Kavya, shree, rowVersion(t, env, "party", shree), true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}
	res, err := env.Kit.Engine.Do(ctx, env.Admin, mstfixture.DocType, id, doc.ActionAmend, doc.ActionInput{Version: rowVersion(t, env, "mst_order", id)})
	if err != nil {
		t.Fatalf("amend: %v", err)
	}
	if res.ID == id || res.State != doc.StateDraft {
		t.Fatalf("amend = %+v; want a new draft, not order %d", res, id)
	}
	lines := []mstfixture.Line{itemLine(m, "MS-ANGLE-50")}
	_, err = mstfixture.Save(ctx, env, env.Kavya, res.ID, shreeHeader(m), lines)
	if !hasFieldError(err, doctype.FieldError{Field: "vendor_id", Message: msgVendorDisabled}) {
		t.Errorf("save with SHREE = %v; want %q", err, msgVendorDisabled)
	}
	header := shreeHeader(m)
	header.VendorID, header.AddressID = m.Parties["BHARAT"], m.Addresses["Bengaluru"]
	if _, err := mstfixture.Save(ctx, env, env.Kavya, res.ID, header, lines); err != nil {
		t.Errorf("save with BHARAT: %v", err)
	}
}

// TestMaster_AllFKsRestrict is MST-015: the only foreign keys into a master
// table that aren't RESTRICT are the owned rows of Global Constraints.
func TestMaster_AllFKsRestrict(t *testing.T) {
	env := msttest.New(t)
	cascade := map[string]bool{
		"address.party_id": true, "address.branch_id": true, "contact.party_id": true, "contact.branch_id": true,
		"contact_email.contact_id": true, "contact_phone.contact_id": true, "item_uom.parent_id": true, "payment_terms_row.parent_id": true,
	}
	masters := map[string]bool{}
	for _, dt := range doctype.All() {
		if dt.IsMaster() {
			masters[dt.Table] = true
		}
	}
	for _, table := range []string{"company", "branch", "party", "address", "contact", "payment_terms", "item", "uom", "tax_code", "expense_type", "project", "site", "employee"} {
		if !masters[table] {
			t.Errorf("master table %s is not registered", table)
		}
	}
	var fks int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `
			SELECT m.name, f."table", f."from", f.on_delete
			FROM sqlite_master m, pragma_foreign_key_list(m.name) f
			WHERE m.type = 'table'`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		seen := map[string]bool{}
		for rows.Next() {
			var table, target, column, onDelete string
			if err := rows.Scan(&table, &target, &column, &onDelete); err != nil {
				return err
			}
			if !masters[target] {
				continue
			}
			fks++
			fk := table + "." + column
			want := "RESTRICT"
			if cascade[fk] {
				want = "CASCADE"
				seen[fk] = true
			}
			if onDelete != want {
				t.Errorf("%s -> %s is ON DELETE %s; want %s", fk, target, onDelete, want)
			}
		}
		for fk := range cascade {
			if !seen[fk] {
				t.Errorf("no foreign key %s; the exception list is stale", fk)
			}
		}
		return rows.Err()
	})
	if fks == 0 {
		t.Fatal("found no foreign keys into master tables")
	}
}

// TestMaster_ConcurrentEditRejected is MST-018.
func TestMaster_ConcurrentEditRejected(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	id := m.Items["MS-ANGLE-50"]
	stored := getItem(t, env, svc, id)
	opened := itemInput(t, env, svc, id)
	kavya, admin := opened, opened
	kavya.Name, admin.Name = "Kavya's edit", "Admin's edit"
	if _, err := svc.Item.Update(ctx, env.Kavya, id, stored.Version, kavya); err != nil {
		t.Fatalf("first save: %v", err)
	}
	_, err := svc.Item.Update(ctx, env.Admin, id, stored.Version, admin)
	wantStale(t, err)
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != 409 || err.Error() != msgStaleItem {
		t.Errorf("second save = %v; want 409 %q", err, msgStaleItem)
	}
	if got := getItem(t, env, svc, id); got.Name != "Kavya's edit" || got.Version != stored.Version+1 {
		t.Errorf("row = %+v; want Kavya's edit at version %d", got, stored.Version+1)
	}
}

// rowState is what a save that changes nothing must leave alone.
type rowState struct {
	version   int
	updatedAt string
	updatedBy int64
}

func readRow(t *testing.T, env *msttest.Env, table string, id int64) rowState {
	t.Helper()
	q := `SELECT version, updated_at, COALESCE(updated_by, 0) FROM ` + table + ` WHERE id = ?` //nolint:gosec // G202: fixed test table names
	var s rowState
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, q, id).Scan(&s.version, &s.updatedAt, &s.updatedBy)
	})
	return s
}

// edit is one real change to a master's input.
type edit[I any] struct {
	field string
	apply func(*I)
}

// saveTarget is one record of a master, with what a page would post for it.
type saveTarget[I any] struct {
	table, docType string
	id             int64
	load           func(context.Context, *db.Tx) (I, error)
	update         func(context.Context, auth.Principal, int64, int, I) (mstkit.Saved, error)
	untidy         func(*I)                         // the same values as a person might type them
	edits          []edit[I]                        // one for every stored field and line, in order
	beforeEdit     func(t *testing.T, field string) // optional: what one edit needs elsewhere
}

func (c saveTarget[I]) stored(t *testing.T, env *msttest.Env) I {
	t.Helper()
	var in I
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		in, err = c.load(ctx, tx)
		return err
	})
	return in
}

// run is CD-208: an unchanged save writes nothing, so a second tab at the same
// version saves too; each real change still bumps; and a stale page is refused
// even when it would change nothing.
func (c saveTarget[I]) run(env *msttest.Env) func(*testing.T) {
	return func(t *testing.T) {
		ctx := context.Background()
		opened := readRow(t, env, c.table, c.id)
		entries := len(auditEntries(t, env, c.docType, c.id))
		in := c.stored(t, env)
		messy := in
		c.untidy(&messy)
		env.Clock.Advance(time.Hour)
		for tab, posted := range []I{in, messy} {
			saved, err := c.update(ctx, env.Kavya, c.id, opened.version, posted)
			if err != nil {
				t.Fatalf("tab %d: %v", tab+1, err)
			}
			if saved.ID != c.id || saved.Version != opened.version {
				t.Errorf("tab %d: saved = %+v; want id %d at version %d", tab+1, saved, c.id, opened.version)
			}
		}
		if got := readRow(t, env, c.table, c.id); got != opened {
			t.Errorf("row = %+v; want %+v", got, opened)
		}
		if got := auditEntries(t, env, c.docType, c.id); len(got) != entries {
			t.Errorf("audit entries = %d; want %d: %+v", len(got), entries, got)
		}
		for _, e := range c.edits {
			t.Run(e.field, func(t *testing.T) {
				if c.beforeEdit != nil {
					c.beforeEdit(t, e.field)
				}
				env.Clock.Advance(time.Hour)
				before := readRow(t, env, c.table, c.id)
				entries := len(auditEntries(t, env, c.docType, c.id))
				in := c.stored(t, env)
				e.apply(&in)
				saved, err := c.update(ctx, env.Kavya, c.id, before.version, in)
				if err != nil {
					t.Fatalf("save: %v", err)
				}
				after := readRow(t, env, c.table, c.id)
				if saved.Version != before.version+1 || after.version != before.version+1 || after.updatedAt == before.updatedAt {
					t.Errorf("saved %+v, row %+v; want version %d and a new updated_at (was %+v)", saved, after, before.version+1, before)
				}
				if got := auditEntries(t, env, c.docType, c.id); len(got) != entries+1 {
					t.Errorf("audit entries = %d; want %d: %+v", len(got), entries+1, got)
				}
			})
		}
		_, err := c.update(ctx, env.Kavya, c.id, opened.version, c.stored(t, env))
		wantStale(t, err)
	}
}

func untidyCode(code *string) { *code = " " + strings.ToLower(*code) + " " }

// companyTarget clears the branch GSTINs for its pan edit only, so the PAN can
// change (MST-023) and the no-op tabs still run with them in place.
func companyTarget(env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	const id = 1
	return saveTarget[company.CompanyInput]{
		table: "company", docType: company.CompanyDocType, id: id,
		load: func(ctx context.Context, tx *db.Tx) (company.CompanyInput, error) {
			c, _, err := svc.Company.Company(ctx, tx)
			return company.CompanyInput{LegalName: c.LegalName, PAN: c.PAN}, err
		},
		update: func(ctx context.Context, p auth.Principal, id int64, version int, in company.CompanyInput) (mstkit.Saved, error) {
			if err := svc.Company.SaveCompany(ctx, p, in, version); err != nil {
				return mstkit.Saved{}, err
			}
			var c company.Company
			err := env.DB.Read(ctx, func(tx *db.Tx) error {
				var err error
				c, _, err = svc.Company.Company(ctx, tx)
				return err
			})
			return mstkit.Saved{ID: id, Version: c.Version}, err
		},
		untidy: func(in *company.CompanyInput) { in.LegalName, in.PAN = " "+in.LegalName+" ", strings.ToLower(in.PAN) },
		edits: []edit[company.CompanyInput]{
			{"name", func(in *company.CompanyInput) { in.LegalName = "Vardhan Infra Limited" }},
			{"pan", func(in *company.CompanyInput) { in.PAN = "AAFCV2449H" }},
		},
		beforeEdit: func(t *testing.T, field string) {
			if field != "pan" {
				return
			}
			for _, code := range []string{"PUNE", "BLR"} {
				var b company.Branch
				read(t, env, func(ctx context.Context, tx *db.Tx) error {
					var err error
					b, err = svc.Company.Get(ctx, tx, m.Branches[code])
					return err
				})
				if _, err := svc.Company.Update(context.Background(), env.Kavya, b.ID, b.Version, company.BranchInput{
					Code: b.Code, Name: b.Name, StateCode: b.StateCode, IsHeadOffice: b.IsHeadOffice,
				}); err != nil {
					t.Fatalf("clear %s GSTIN: %v", code, err)
				}
			}
		},
	}.run(env)
}

// branchTarget is a new head office with no addresses, so its state can change
// (MST-029).
func branchTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.Company.Create(context.Background(), env.Kavya, company.BranchInput{
		Code: "NASHIK", Name: "Nashik Works", StateCode: env.Fx.Branches[0].StateCode, GSTIN: env.Fx.Branches[0].GSTIN, IsHeadOffice: true,
	})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	return saveTarget[company.BranchInput]{
		table: "branch", docType: company.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (company.BranchInput, error) {
			b, err := svc.Company.Get(ctx, tx, saved.ID)
			return company.BranchInput{Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: b.GSTIN, IsHeadOffice: b.IsHeadOffice}, err
		},
		update: svc.Company.Update,
		untidy: func(in *company.BranchInput) {
			untidyCode(&in.Code)
			in.Name = in.Name + " "
			in.GSTIN = strings.ToLower(in.GSTIN)
		},
		edits: []edit[company.BranchInput]{
			{"code", func(in *company.BranchInput) { in.Code = "NASHIK-1" }},
			{"name", func(in *company.BranchInput) { in.Name = "Nashik Plant" }},
			{"gstin", func(in *company.BranchInput) { in.GSTIN = "" }},
			{"state_code", func(in *company.BranchInput) { in.StateCode = "29" }},
			{"is_head_office", func(in *company.BranchInput) { in.IsHeadOffice = false }},
		},
	}.run(env)
}

func termsTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.Terms.Create(context.Background(), env.Kavya, paymentterms.Input{Code: "NET30", Name: "Net 30", Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
	if err != nil {
		t.Fatalf("create terms: %v", err)
	}
	return saveTarget[paymentterms.Input]{
		table: "payment_terms", docType: paymentterms.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (paymentterms.Input, error) {
			r, err := svc.Terms.Get(ctx, tx, saved.ID)
			return paymentterms.Input{Code: r.Code, Name: r.Name, Rows: r.Rows}, err
		},
		update: svc.Terms.Update,
		untidy: func(in *paymentterms.Input) { untidyCode(&in.Code); in.Name = " " + in.Name },
		edits: []edit[paymentterms.Input]{
			{"code", func(in *paymentterms.Input) { in.Code = "NET-30" }},
			{"name", func(in *paymentterms.Input) { in.Name = "Thirty days" }},
			{"row days", func(in *paymentterms.Input) { in.Rows = []paymentterms.Row{{Portion: 100_000, Days: 45}} }},
			{"row portion", func(in *paymentterms.Input) {
				in.Rows = []paymentterms.Row{{Portion: 40_000, Days: 0}, {Portion: 60_000, Days: 45}}
			}},
			{"row order", func(in *paymentterms.Input) {
				in.Rows = []paymentterms.Row{{Portion: 60_000, Days: 45}, {Portion: 40_000, Days: 0}}
			}},
		},
	}.run(env)
}

func partyTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services, termsID int64) func(*testing.T) {
	saved, err := svc.Party.Create(context.Background(), env.Kavya, party.Input{Code: "C-TATA", Name: "Tata Projects", IsCustomer: true, GSTCategory: gstin.Unregistered})
	if err != nil {
		t.Fatalf("create party: %v", err)
	}
	return saveTarget[party.Input]{
		table: "party", docType: party.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (party.Input, error) {
			p, err := svc.Party.Get(ctx, tx, saved.ID)
			return partyInput(p), err
		},
		update: svc.Party.Update,
		untidy: func(in *party.Input) { untidyCode(&in.Code); in.Name = in.Name + " "; in.GSTCategory = "" },
		edits: []edit[party.Input]{
			{"code", func(in *party.Input) { in.Code = "TATA" }},
			{"name", func(in *party.Input) { in.Name = "Tata Projects Ltd" }},
			{"is_vendor", func(in *party.Input) { in.IsVendor = true }},
			{"is_customer", func(in *party.Input) { in.IsCustomer = false }},
			{"pan", func(in *party.Input) { in.PAN = "AAACT2727Q" }},
			{"gst_category", func(in *party.Input) { in.GSTCategory = gstin.Overseas }},
			{"default_payment_terms_id", func(in *party.Input) { in.DefaultPaymentTermsID = termsID }},
		},
	}.run(env)
}

func addressInput(a address.Address) address.Input {
	return address.Input{
		PartyID: a.PartyID, BranchID: a.BranchID, Title: a.Title, Line1: a.Line1, Line2: a.Line2, City: a.City,
		StateCode: a.StateCode, Country: a.Country, Pincode: a.Pincode, GSTIN: a.GSTIN,
		IsBilling: a.IsBilling, IsShipping: a.IsShipping, PrimaryBilling: a.PrimaryBilling, PrimaryShipping: a.PrimaryShipping,
	}
}

func addressLoad(svc *allmasters.Services, id int64) func(context.Context, *db.Tx) (address.Input, error) {
	return func(ctx context.Context, tx *db.Tx) (address.Input, error) {
		a, err := svc.Address.Get(ctx, tx, id)
		return addressInput(a), err
	}
}

// addressTarget is a second billing address of a new Overseas party: it isn't
// primary yet, it may lose billing and go abroad, and a GSTIN that fits the
// category needs a country outside India, so the gstin edit comes last.
func addressTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services) func(*testing.T) {
	ctx := context.Background()
	owner, err := svc.Party.Create(ctx, env.Kavya, party.Input{Code: "SG-SUPPLY", Name: "Singapore Supply Pte", IsVendor: true, GSTCategory: gstin.Overseas})
	if err != nil {
		t.Fatalf("create party: %v", err)
	}
	in := address.Input{PartyID: owner.ID, Title: "Office", Line1: "1 Main Road", City: "Hyderabad", StateCode: "36", Pincode: "500001", IsBilling: true}
	if _, err := svc.Address.Create(ctx, env.Kavya, in); err != nil {
		t.Fatalf("create first address: %v", err)
	}
	in.Title = "Depot"
	saved, err := svc.Address.Create(ctx, env.Kavya, in)
	if err != nil {
		t.Fatalf("create address: %v", err)
	}
	return saveTarget[address.Input]{
		table: "address", docType: address.DocType, id: saved.ID,
		load:   addressLoad(svc, saved.ID),
		update: svc.Address.Update,
		untidy: func(in *address.Input) { in.Title, in.City, in.PartyID, in.BranchID = " "+in.Title, in.City+" ", 0, 0 },
		edits: []edit[address.Input]{
			{"title", func(in *address.Input) { in.Title = "Yard" }},
			{"line1", func(in *address.Input) { in.Line1 = "2 Main Road" }},
			{"line2", func(in *address.Input) { in.Line2 = "Near the gate" }},
			{"city", func(in *address.Input) { in.City = "Secunderabad" }},
			{"pincode", func(in *address.Input) { in.Pincode = "500003" }},
			{"state_code", func(in *address.Input) { in.StateCode, in.Pincode = "27", "411001" }},
			{"is_shipping", func(in *address.Input) { in.IsShipping = true }},
			{"is_primary_billing", func(in *address.Input) { in.PrimaryBilling = true }},
			{"is_primary_shipping", func(in *address.Input) { in.IsShipping, in.PrimaryShipping = false, false }},
			{"is_billing", func(in *address.Input) { in.IsBilling, in.PrimaryBilling, in.IsShipping = false, false, true }},
			{"country", func(in *address.Input) { in.Country, in.StateCode, in.Pincode = "Singapore", "", "118524" }},
			{"gstin", func(in *address.Input) { in.GSTIN = "9917SGP29001OST" }},
		},
	}.run(env)
}

// branchAddressTarget is a branch's own address: no party rule applies, and a
// save that changes nothing writes nothing here too.
func branchAddressTarget(env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	id := m.BranchAddresses["PUNE"]
	return saveTarget[address.Input]{
		table: "address", docType: address.DocType, id: id,
		load:   addressLoad(svc, id),
		update: svc.Address.Update,
		untidy: func(in *address.Input) { in.Title, in.City, in.PartyID, in.BranchID = " "+in.Title, in.City+" ", 0, 0 },
		edits: []edit[address.Input]{
			{"title", func(in *address.Input) { in.Title = "Works" }},
			{"pincode", func(in *address.Input) { in.Pincode = "411026" }},
		},
	}.run(env)
}

func contactInput(c address.Contact) address.ContactInput {
	return address.ContactInput{
		PartyID: c.PartyID, BranchID: c.BranchID, FirstName: c.FirstName, MiddleName: c.MiddleName, LastName: c.LastName,
		Emails: c.Emails, Phones: c.Phones, Primary: c.Primary,
	}
}

func contactLoad(svc *allmasters.Services, id int64) func(context.Context, *db.Tx) (address.ContactInput, error) {
	return func(ctx context.Context, tx *db.Tx) (address.ContactInput, error) {
		c, err := svc.Address.GetContact(ctx, tx, id)
		return contactInput(c), err
	}
}

func contactTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services, branchID int64) func(*testing.T) {
	const mobile = "+919876543210"
	saved, err := svc.Address.CreateContact(context.Background(), env.Kavya, address.ContactInput{
		BranchID: branchID, FirstName: "Sunil", LastName: "Mehta", Primary: true,
		Emails: []address.EmailInput{{Email: "sunil@example.com", Primary: true}, {Email: "accounts@example.com"}},
		Phones: []address.PhoneInput{{Number: "9876543210", Mobile: true, Primary: true}, {Number: "040-2345"}},
	})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	return saveTarget[address.ContactInput]{
		table: "contact", docType: address.ContactDocType, id: saved.ID,
		load:   contactLoad(svc, saved.ID),
		update: svc.Address.UpdateContact,
		untidy: func(in *address.ContactInput) {
			in.FirstName = " " + in.FirstName
			in.Emails = []address.EmailInput{{Email: " SUNIL@example.com", Primary: true}, {Email: "accounts@example.com "}}
			in.Phones = []address.PhoneInput{{Number: "98765 43210", Mobile: true, Primary: true}, {Number: " 040-2345 "}}
		},
		edits: []edit[address.ContactInput]{
			{"first_name", func(in *address.ContactInput) { in.FirstName = "Sunil K" }},
			{"middle_name", func(in *address.ContactInput) { in.MiddleName = "Kumar" }},
			{"last_name", func(in *address.ContactInput) { in.LastName = "Shah" }},
			{"email", func(in *address.ContactInput) { in.Emails[1].Email = "billing@example.com" }},
			{"email primary", func(in *address.ContactInput) { in.Emails[0].Primary, in.Emails[1].Primary = false, true }},
			{"email removed", func(in *address.ContactInput) { in.Emails = in.Emails[:1] }},
			{"phone", func(in *address.ContactInput) { in.Phones[1].Number = "040-9999" }},
			{"phone primary", func(in *address.ContactInput) { in.Phones[0].Primary, in.Phones[1].Primary = false, true }},
			{"phone mobile", func(in *address.ContactInput) { in.Phones[0] = address.PhoneInput{Number: mobile} }},
			{"phone removed", func(in *address.ContactInput) { in.Phones = in.Phones[:1] }},
			{"is_primary", func(in *address.ContactInput) { in.Primary = false }},
		},
	}.run(env)
}

func uomTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.UoM.Create(context.Background(), env.Kavya, uom.Input{Code: "PALLET", Name: "Pallet", UQC: "OTH"})
	if err != nil {
		t.Fatalf("create UoM: %v", err)
	}
	return saveTarget[uom.Input]{
		table: "uom", docType: uom.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (uom.Input, error) {
			u, err := svc.UoM.Get(ctx, tx, saved.ID)
			return uom.Input{Code: u.Code, Name: u.Name, UQC: u.UQC, WholeNumber: u.WholeNumber}, err
		},
		update: svc.UoM.Update,
		untidy: func(in *uom.Input) { untidyCode(&in.Code); in.Name = " " + in.Name; in.UQC = strings.ToLower(in.UQC) },
		edits: []edit[uom.Input]{
			{"code", func(in *uom.Input) { in.Code = "PLT" }},
			{"name", func(in *uom.Input) { in.Name = "Wooden pallet" }},
			{"uqc", func(in *uom.Input) { in.UQC = "NOS" }},
			{"whole_number", func(in *uom.Input) { in.WholeNumber = true }},
		},
	}.run(env)
}

func taxTarget(t *testing.T, env *msttest.Env, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.TaxCode.Create(context.Background(), env.Kavya, taxcode.Input{Code: "GST-TEST", Name: "Test rate", Treatment: taxcode.Taxable, Rate: gst12})
	if err != nil {
		t.Fatalf("create tax code: %v", err)
	}
	return saveTarget[taxcode.Input]{
		table: "tax_code", docType: taxcode.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (taxcode.Input, error) {
			c, err := svc.TaxCode.Get(ctx, tx, saved.ID)
			return taxcode.Input{Code: c.Code, Name: c.Name, Treatment: c.Treatment, Rate: c.Rate}, err
		},
		update: svc.TaxCode.Update,
		untidy: func(in *taxcode.Input) { untidyCode(&in.Code); in.Name = in.Name + " " },
		edits: []edit[taxcode.Input]{
			{"code", func(in *taxcode.Input) { in.Code = "GST-T" }},
			{"name", func(in *taxcode.Input) { in.Name = "Trial rate" }},
			{"rate", func(in *taxcode.Input) { in.Rate = 18_000 }},
			{"treatment", func(in *taxcode.Input) { in.Treatment, in.Rate = taxcode.Exempted, 0 }},
			{"treatment at rate 0", func(in *taxcode.Input) { in.Treatment = taxcode.NilRated }},
		},
	}.run(env)
}

func itemInput(t *testing.T, env *msttest.Env, svc *allmasters.Services, id int64) item.Input {
	t.Helper()
	var in item.Input
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		in, err = itemLoad(svc, id)(ctx, tx)
		return err
	})
	return in
}

func itemLoad(svc *allmasters.Services, id int64) func(context.Context, *db.Tx) (item.Input, error) {
	return func(ctx context.Context, tx *db.Tx) (item.Input, error) {
		it, err := svc.Item.Get(ctx, tx, id)
		if err != nil {
			return item.Input{}, err
		}
		conv, err := svc.Item.Conversions(ctx, tx, id)
		return item.Input{
			Code: it.Code, Name: it.Name, Type: it.Type, HSNSAC: it.HSNSAC, StockUoMID: it.StockUoMID, PurchaseUoMID: it.PurchaseUoMID,
			DefaultTaxCodeID: it.DefaultTaxCodeID, IsStock: it.IsStock, StandardRate: it.StandardRate, Conversions: conv,
		}, err
	}
}

func itemTarget(t *testing.T, env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	in := goods(m, "ROD-10", "Rod 10 mm")
	in.Conversions = []item.Conversion{{UoMID: m.UoMs["BOX"], Factor: 40 * uom.FactorOne}}
	saved := createItem(t, env, svc, in)
	return saveTarget[item.Input]{
		table: "item", docType: item.DocType, id: saved.ID,
		load:   itemLoad(svc, saved.ID),
		update: svc.Item.Update,
		untidy: func(in *item.Input) { untidyCode(&in.Code); in.Name = " " + in.Name; in.HSNSAC = " " + in.HSNSAC },
		edits: []edit[item.Input]{
			{"code", func(in *item.Input) { in.Code = "ROD-10MM" }},
			{"name", func(in *item.Input) { in.Name = "Rod, 10 mm" }},
			{"hsn_sac", func(in *item.Input) { in.HSNSAC = "7214" }},
			{"purchase_uom_id", func(in *item.Input) { in.PurchaseUoMID = m.UoMs["BOX"] }},
			{"default_tax_code_id", func(in *item.Input) { in.DefaultTaxCodeID = 0 }},
			{"is_stock", func(in *item.Input) { in.IsStock = false }},
			{"standard_rate", func(in *item.Input) { in.StandardRate = 62_5000 }},
			{"conversion factor", func(in *item.Input) { in.Conversions[0].Factor = 50 * uom.FactorOne }},
			{"conversion added", func(in *item.Input) {
				in.Conversions = append(in.Conversions, item.Conversion{UoMID: m.UoMs["BAG"], Factor: 25 * uom.FactorOne})
			}},
			{"conversion order", func(in *item.Input) {
				in.Conversions[0], in.Conversions[1] = in.Conversions[1], in.Conversions[0]
			}},
			{"conversion removed", func(in *item.Input) {
				in.Conversions = in.Conversions[:len(in.Conversions)-1]
			}},
			{"stock_uom_id", func(in *item.Input) { in.StockUoMID = m.UoMs["TON"] }},
			{"item_type", func(in *item.Input) { in.Type, in.HSNSAC = item.TypeService, "997313" }},
		},
	}.run(env)
}

func expenseTarget(env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	id := m.ExpenseTypes["TRAVEL"]
	return saveTarget[expensetype.Input]{
		table: "expense_type", docType: expensetype.DocType, id: id,
		load: func(ctx context.Context, tx *db.Tx) (expensetype.Input, error) {
			e, err := svc.ExpenseType.Get(ctx, tx, id)
			return expensetype.Input{Code: e.Code, Name: e.Name, DefaultHSNSAC: e.DefaultHSNSAC, DefaultTaxCodeID: e.DefaultTaxCodeID}, err
		},
		update: svc.ExpenseType.Update,
		untidy: func(in *expensetype.Input) { untidyCode(&in.Code); in.Name = in.Name + " " },
		edits: []edit[expensetype.Input]{
			{"code", func(in *expensetype.Input) { in.Code = "TRAVEL-1" }},
			{"name", func(in *expensetype.Input) { in.Name = "Travelling" }},
			{"default_hsn_sac", func(in *expensetype.Input) { in.DefaultHSNSAC = "996601" }},
			{"default_tax_code_id", func(in *expensetype.Input) { in.DefaultTaxCodeID = m.TaxCodes["GST18"] }},
		},
	}.run(env)
}

func projectTarget(t *testing.T, env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.Project.Create(context.Background(), env.Kavya, project.Input{Code: "PRJ-NEW", Name: "New Project", BranchID: m.Branches["PUNE"]})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	customer, err := svc.Party.Create(context.Background(), env.Kavya, party.Input{Code: "PRJ-CUSTOMER", Name: "Project Customer", IsCustomer: true})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	return saveTarget[project.Input]{
		table: "project", docType: project.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (project.Input, error) {
			p, err := svc.Project.Get(ctx, tx, saved.ID)
			return project.Input{Code: p.Code, Name: p.Name, BranchID: p.BranchID, CustomerID: p.CustomerID, Start: p.Start, End: p.End, Status: p.Status}, err
		},
		update: svc.Project.Update,
		untidy: func(in *project.Input) { untidyCode(&in.Code); in.Name = in.Name + " "; in.Status = "" },
		edits: []edit[project.Input]{
			{"code", func(in *project.Input) { in.Code = "PRJ-OLD" }},
			{"name", func(in *project.Input) { in.Name = "Old Project" }},
			{"branch_id", func(in *project.Input) { in.BranchID = m.Branches["BLR"] }},
			{"customer_id", func(in *project.Input) { in.CustomerID = customer.ID }},
			{"start_date", func(in *project.Input) { in.Start = orderDate }},
			{"end_date", func(in *project.Input) { in.End = orderDate }},
			{"status", func(in *project.Input) { in.Status = project.OnHold }},
		},
	}.run(env)
}

func siteTarget(t *testing.T, env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	saved, err := svc.Site.Create(context.Background(), env.Kavya, site.Input{Code: "NEW-WH", Name: "New store", BranchID: m.Branches["PUNE"], Type: site.Warehouse})
	if err != nil {
		t.Fatalf("create site: %v", err)
	}
	return saveTarget[site.Input]{
		table: "site", docType: site.DocType, id: saved.ID,
		load: func(ctx context.Context, tx *db.Tx) (site.Input, error) {
			s, err := svc.Site.Get(ctx, tx, saved.ID)
			return site.Input{Code: s.Code, Name: s.Name, BranchID: s.BranchID, ProjectID: s.ProjectID, Type: s.Type}, err
		},
		update: svc.Site.Update,
		untidy: func(in *site.Input) { untidyCode(&in.Code); in.Name = " " + in.Name },
		edits: []edit[site.Input]{
			{"code", func(in *site.Input) { in.Code = "OLD-WH" }},
			{"name", func(in *site.Input) { in.Name = "Old store" }},
			{"branch_id", func(in *site.Input) { in.BranchID = m.Branches["BLR"] }},
			{"project_id", func(in *site.Input) { in.ProjectID = m.Projects["PRJ-TECHPARK"] }},
			{"type", func(in *site.Input) { in.Type = site.SiteKind }},
		},
	}.run(env)
}

func employeeInput(e employee.Employee) employee.Input {
	return employee.Input{
		Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID,
		Joining: e.Joining, Birth: e.Birth, Relieving: e.Relieving, Status: e.Status,
	}
}

func employeeLoad(svc *allmasters.Services, id int64) func(context.Context, *db.Tx) (employee.Input, error) {
	return func(ctx context.Context, tx *db.Tx) (employee.Input, error) {
		e, err := svc.Employee.Get(ctx, tx, id)
		return employeeInput(e), err
	}
}

func employeeTarget(t *testing.T, env *msttest.Env, m msttest.Masters, svc *allmasters.Services) func(*testing.T) {
	const free = 90 // a user no employee has
	sqlExec(t, env, `INSERT INTO users (id, email, name, enabled, created_at, updated_at) VALUES (?, 'free@vardhan.test', 'Free User', 1, 'x', 'x')`, free)
	saved, err := svc.Employee.Create(context.Background(), env.Kavya, employee.Input{
		Code: "EMP-900", Name: "New Hire", BranchID: m.Branches["PUNE"], Joining: orderDate, Status: employee.Active,
	})
	if err != nil {
		t.Fatalf("create employee: %v", err)
	}
	later := types.Date{Year: 2026, Month: time.October, Day: 25}
	return saveTarget[employee.Input]{
		table: "employee", docType: employee.DocType, id: saved.ID,
		load:   employeeLoad(svc, saved.ID),
		update: svc.Employee.Update,
		untidy: func(in *employee.Input) { untidyCode(&in.Code); in.Name = in.Name + " " },
		edits: []edit[employee.Input]{
			{"code", func(in *employee.Input) { in.Code = "EMP-901" }},
			{"name", func(in *employee.Input) { in.Name = "Old Hire" }},
			{"branch_id", func(in *employee.Input) { in.BranchID = m.Branches["BLR"] }},
			{"manager_id", func(in *employee.Input) { in.ManagerID = m.Employees["EMP-004"] }},
			{"user_id", func(in *employee.Input) { in.UserID = free }},
			{"joining_date", func(in *employee.Input) { in.Joining = types.Date{Year: 2025, Month: time.April, Day: 1} }},
			{"birth_date", func(in *employee.Input) { in.Birth = types.Date{Year: 1990, Month: time.April, Day: 1} }},
			{"relieving_date", func(in *employee.Input) { in.Relieving = later }},
			{"status", func(in *employee.Input) { in.Status = employee.Left }},
		},
	}.run(env)
}

// TestMaster_NoOpSave is CD-208 on every master (DOC-029's rule).
func TestMaster_NoOpSave(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	branch, err := svc.Company.Create(ctx, env.Kavya, company.BranchInput{Code: "HYD", Name: "Hyderabad Office", StateCode: "36"})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	terms, err := svc.Terms.Create(ctx, env.Kavya, paymentterms.Input{Code: "NET60", Name: "Net 60", Rows: []paymentterms.Row{{Portion: 100_000, Days: 60}}})
	if err != nil {
		t.Fatalf("create terms: %v", err)
	}
	for _, c := range []struct {
		name string
		run  func(*testing.T)
	}{
		{"Branch", branchTarget(t, env, svc)},
		{"Payment Terms", termsTarget(t, env, svc)},
		{"Party", partyTarget(t, env, svc, terms.ID)},
		{"Address", addressTarget(t, env, svc)},
		{"Branch Address", branchAddressTarget(env, m, svc)},
		{"Contact", contactTarget(t, env, svc, branch.ID)},
		{"UoM", uomTarget(t, env, svc)},
		{"Tax Code", taxTarget(t, env, svc)},
		{"Item", itemTarget(t, env, m, svc)},
		{"Expense Type", expenseTarget(env, m, svc)},
		{"Project", projectTarget(t, env, m, svc)},
		{"Site", siteTarget(t, env, m, svc)},
		{"Employee", employeeTarget(t, env, m, svc)},
		{"Company", companyTarget(env, m, svc)}, // last: it clears the branch GSTINs for its PAN edit
	} {
		t.Run(c.name, c.run)
	}
}

// TestMaster_NoOpSaveUntouchedPrimary: a primary address saves unchanged
// whether the page posts the flag or not (a lone address stays primary), and
// the save leaves its party's row alone.
func TestMaster_NoOpSaveUntouchedPrimary(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	id := m.Addresses["Mumbai"]
	v := rowVersion(t, env, "address", id)
	in := addressInput(getAddress(t, env, svc, id))
	if !in.PrimaryBilling {
		t.Fatalf("Mumbai = %+v; want the primary billing address", in)
	}
	in.GSTIN = strings.ToLower(in.GSTIN)
	owner := readRow(t, env, "party", in.PartyID)
	env.Clock.Advance(time.Hour)
	for _, primary := range []bool{true, false} {
		in.PrimaryBilling = primary
		saved, err := svc.Address.Update(ctx, env.Kavya, id, v, in)
		if err != nil {
			t.Fatalf("save with primary %v: %v", primary, err)
		}
		if saved.Version != v {
			t.Errorf("saved version = %d; want %d", saved.Version, v)
		}
	}
	if got := getAddress(t, env, svc, id); !got.PrimaryBilling || got.Version != v {
		t.Errorf("address = %+v; want primary at version %d", got, v)
	}
	if got := readRow(t, env, "party", in.PartyID); got != owner {
		t.Errorf("party row = %+v; want %+v", got, owner)
	}
}

func getAddress(t *testing.T, env *msttest.Env, svc *allmasters.Services, id int64) address.Address {
	t.Helper()
	var a address.Address
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		a, err = svc.Address.Get(ctx, tx, id)
		return err
	})
	return a
}

// wantNoSaved: a refused save returns no version, as every refusal does.
func wantNoSaved(t *testing.T, saved mstkit.Saved) {
	t.Helper()
	if saved.ID != 0 || saved.Version != 0 {
		t.Errorf("refused save returned %+v; want none", saved)
	}
}

// TestMaster_NoOpSaveStillChecksState: the post-state checks every write calls
// last run on an unchanged save too (CD-180, CD-190), so one that today
// refuses a broken record still does. The records are broken by SQL because
// the services refuse to get there.
func TestMaster_NoOpSaveStillChecksState(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	sqlExec(t, env, `UPDATE address SET disabled = 1 WHERE id = ?`, m.Addresses["Mumbai"])

	t.Run("Party", func(t *testing.T) {
		p := getParty(t, env, svc, m.Parties["SHREE"])
		saved, err := svc.Party.Update(ctx, env.Kavya, p.ID, p.Version, partyInput(p))
		if !hasFieldError(err, doctype.FieldError{Field: "gst_category", Message: msgEnableAddress}) {
			t.Errorf("save = %v; want %q on gst_category", err, msgEnableAddress)
		}
		wantNoSaved(t, saved)
	})
	t.Run("Address", func(t *testing.T) {
		a := getAddress(t, env, svc, m.Addresses["Thane"])
		saved, err := svc.Address.Update(ctx, env.Kavya, a.ID, a.Version, addressInput(a))
		if !hasFieldError(err, doctype.FieldError{Field: "is_billing", Message: msgEnableAddress}) {
			t.Errorf("save = %v; want %q on is_billing", err, msgEnableAddress)
		}
		wantNoSaved(t, saved)
	})
	t.Run("Branch", func(t *testing.T) { // MST-029 on the stored address
		var b company.Branch
		read(t, env, func(ctx context.Context, tx *db.Tx) error {
			var err error
			b, err = svc.Company.Get(ctx, tx, m.Branches["BLR"])
			return err
		})
		sqlExec(t, env, `UPDATE address SET state_code = '27' WHERE id = ?`, m.BranchAddresses["BLR"])
		saved, err := svc.Company.Update(ctx, env.Kavya, b.ID, b.Version, company.BranchInput{
			Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: b.GSTIN, IsHeadOffice: b.IsHeadOffice,
		})
		if !hasFieldError(err, doctype.FieldError{Field: "state_code", Message: company.MsgAddressState}) {
			t.Errorf("save = %v; want %q on state_code", err, company.MsgAddressState)
		}
		wantNoSaved(t, saved)
	})
	t.Run("Company", func(t *testing.T) { // MST-023 on the stored branch GSTIN
		sqlExec(t, env, `UPDATE branch SET gstin = '27AABCS1429B1ZU' WHERE id = ?`, m.Branches["BLR"])
		var c company.Company
		read(t, env, func(ctx context.Context, tx *db.Tx) error {
			var err error
			c, _, err = svc.Company.Company(ctx, tx)
			return err
		})
		err := svc.Company.SaveCompany(ctx, env.Kavya, company.CompanyInput{LegalName: c.LegalName, PAN: c.PAN}, c.Version)
		want := "GSTIN 27AABCS1429B1ZU belongs to PAN AABCS1429B, not the company PAN " + c.PAN + "."
		if !hasFieldError(err, doctype.FieldError{Field: "pan", Message: want}) {
			t.Errorf("save = %v; want %q on pan", err, want)
		}
	})
	t.Run("Contact", func(t *testing.T) {
		owner := m.Parties["BHARAT"]
		primary, err := svc.Address.CreateContact(ctx, env.Kavya, address.ContactInput{PartyID: owner, FirstName: "Asha", Primary: true})
		if err != nil {
			t.Fatalf("create primary contact: %v", err)
		}
		other, err := svc.Address.CreateContact(ctx, env.Kavya, address.ContactInput{PartyID: owner, FirstName: "Vikram"})
		if err != nil {
			t.Fatalf("create contact: %v", err)
		}
		sqlExec(t, env, `UPDATE contact SET disabled = 1 WHERE id = ?`, primary.ID)
		saved, err := svc.Address.UpdateContact(ctx, env.Kavya, other.ID, other.Version, address.ContactInput{PartyID: owner, FirstName: "Vikram"})
		if !hasFieldError(err, doctype.FieldError{Field: "disabled", Message: msgEnableContact}) {
			t.Errorf("save = %v; want %q on disabled", err, msgEnableContact)
		}
		wantNoSaved(t, saved)
	})
}

// TestMaster_NoOpSaveLeavesUserAlone: an unchanged employee save runs no user
// sync, for a Left employee as for an Active one.
func TestMaster_NoOpSaveLeavesUserAlone(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	for _, code := range []string{"EMP-007", "EMP-008"} {
		t.Run(code, func(t *testing.T) {
			id := m.Employees[code]
			var e employee.Employee
			read(t, env, func(ctx context.Context, tx *db.Tx) error {
				var err error
				e, err = svc.Employee.Get(ctx, tx, id)
				return err
			})
			before := readRow(t, env, "users", int64(e.UserID))
			env.Clock.Advance(time.Hour)
			saved, err := svc.Employee.Update(ctx, env.Kavya, id, e.Version, employeeInput(e))
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			if saved.Version != e.Version {
				t.Errorf("saved version = %d; want %d", saved.Version, e.Version)
			}
			if got := readRow(t, env, "users", int64(e.UserID)); got != before {
				t.Errorf("user = %+v; want %+v", got, before)
			}
		})
	}
}
