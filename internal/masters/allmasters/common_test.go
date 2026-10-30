package allmasters_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
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
	msgCodeRequired = "Code is required. Enter a short code such as SHREE." // MST-001
	msgCodeUsedItem = "Code MS-ANGLE-50 is already used by Item MS Angle 50x50x5. Choose another code."
	orderQty        = types.Qty(1200_000) // 1,200.000 KGS
	orderRate       = types.Rate(62_5000)
	gst12           = types.Percent(12_000)
)

var orderDate = types.Date{Year: 2026, Month: time.October, Day: 24}

// shreeHeader is an order for SHREE's Mumbai address at branch PUNE.
func shreeHeader(m msttest.Masters) mstfixture.Header {
	return mstfixture.Header{VendorID: m.Parties["SHREE"], AddressID: m.Addresses["Mumbai"], BranchID: m.Branches["PUNE"], Date: orderDate}
}

// itemLine is a KGS line of item code.
func itemLine(m msttest.Masters, code string) mstfixture.Line {
	return mstfixture.Line{ItemID: m.Items[code], UoMID: m.UoMs["KGS"], TaxCodeID: m.TaxCodes["GST18"], Qty: orderQty, Rate: orderRate}
}

// order saves a draft for SHREE with one line of item code.
func order(t *testing.T, env *msttest.Env, m msttest.Masters, code string) int64 {
	t.Helper()
	res, err := mstfixture.Save(context.Background(), env, env.Kavya, 0, shreeHeader(m), []mstfixture.Line{itemLine(m, code)})
	if err != nil {
		t.Fatalf("save draft with %s: %v", code, err)
	}
	return res.ID
}

// goods is an input for a KGS stock item.
func goods(m msttest.Masters, code, name string) item.Input {
	return item.Input{Code: code, Name: name, Type: item.TypeGoods, HSNSAC: "7216", StockUoMID: m.UoMs["KGS"], DefaultTaxCodeID: m.TaxCodes["GST18"], IsStock: true}
}

func createItem(t *testing.T, env *msttest.Env, svc *allmasters.Services, in item.Input) mstkit.Saved {
	t.Helper()
	saved, err := svc.Item.Create(context.Background(), env.Kavya, in)
	if err != nil {
		t.Fatalf("create item %s: %v", in.Code, err)
	}
	return saved
}

func getParty(t *testing.T, env *msttest.Env, svc *allmasters.Services, id int64) party.Party {
	t.Helper()
	var p party.Party
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		p, err = svc.Party.Get(ctx, tx, id)
		return err
	})
	return p
}

func getItem(t *testing.T, env *msttest.Env, svc *allmasters.Services, id int64) item.Item {
	t.Helper()
	var it item.Item
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		it, err = svc.Item.Get(ctx, tx, id)
		return err
	})
	return it
}

func partyInput(p party.Party) party.Input {
	return party.Input{
		Code: p.Code, Name: p.Name, IsVendor: p.IsVendor, IsCustomer: p.IsCustomer,
		PAN: p.PAN, GSTCategory: p.GSTCategory, DefaultPaymentTermsID: p.DefaultPaymentTermsID,
	}
}

func auditEntries(t *testing.T, env *msttest.Env, docType string, id int64) []audit.Entry {
	t.Helper()
	var es []audit.Entry
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		es, err = audit.ForDoc(ctx, tx, docType, id)
		return err
	})
	return es
}

// rowCount counts the rows of table with the given id.
func rowCount(t *testing.T, env *msttest.Env, table string, id int64) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE id = ?`, id).Scan(&n) //nolint:gosec // G202: fixed test table names
	})
	return n
}

// rowVersion reads the row's version, as a page would have posted it.
func rowVersion(t *testing.T, env *msttest.Env, table string, id int64) int {
	t.Helper()
	var v int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT version FROM `+table+` WHERE id = ?`, id).Scan(&v) //nolint:gosec // G202: fixed test table names
	})
	return v
}

func hasFieldError(err error, want doctype.FieldError) bool {
	var he doctype.HTTPError
	return errors.As(err, &he) && slices.Contains(he.FieldErrors(), want)
}

func wantRefused(t *testing.T, err error, msg string) {
	t.Helper()
	var refused *mstkit.DeleteRefusedError
	if !errors.As(err, &refused) || err.Error() != msg {
		t.Fatalf("delete = %v (%T); want *mstkit.DeleteRefusedError %q", err, err, msg)
	}
}

// TestMaster_CodeRequired is MST-001.
func TestMaster_CodeRequired(t *testing.T) {
	const name, pan = "Shree Steel Traders", "AAFCV2449G"
	for _, c := range []struct {
		master   string
		validate func() error
	}{
		{"Party", func() error { _, err := party.Validate(party.Input{Name: name, IsVendor: true}); return err }},
		{"Item", func() error {
			_, err := item.Validate(item.Input{Name: name, Type: item.TypeGoods, HSNSAC: "7216", StockUoMID: 1})
			return err
		}},
		{"UoM", func() error { _, err := uom.Validate(uom.Input{Name: name, UQC: "NOS"}); return err }},
		{"Tax Code", func() error {
			_, err := taxcode.Validate(taxcode.Input{Name: name, Treatment: taxcode.Taxable, Rate: gst12})
			return err
		}},
		{"Project", func() error { _, err := project.Validate(project.Input{Name: name, BranchID: 1}); return err }},
		{"Site", func() error {
			_, err := site.Validate(site.Input{Name: name, BranchID: 1, Type: site.Warehouse})
			return err
		}},
		{"Employee", func() error {
			_, err := employee.Validate(employee.Input{Name: name, BranchID: 1, Joining: orderDate, Status: employee.Active})
			return err
		}},
		{"Expense Type", func() error { _, err := expensetype.Validate(expensetype.Input{Name: name}); return err }},
		{"Payment Terms", func() error {
			_, err := paymentterms.Validate(paymentterms.Input{Name: name, Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
			return err
		}},
		{"Branch", func() error {
			_, err := company.ValidateBranch(company.BranchInput{Name: name, StateCode: "27"}, pan)
			return err
		}},
	} {
		t.Run(c.master, func(t *testing.T) {
			err := c.validate()
			if !hasFieldError(err, doctype.FieldError{Field: "code", Message: msgCodeRequired}) {
				t.Errorf("Validate(blank code) = %v; want code error %q", err, msgCodeRequired)
			}
		})
	}
}

// TestMaster_CodeUniqueCaseInsensitive is MST-003.
func TestMaster_CodeUniqueCaseInsensitive(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	_, err := svc.Party.Create(ctx, env.Kavya, party.Input{Code: "shree", Name: "Another Shree", IsVendor: true})
	const msg = "Code SHREE is already used by Party Shree Steel Traders. Choose another code."
	if !hasFieldError(err, doctype.FieldError{Field: "code", Message: msg}) {
		t.Errorf("create party shree = %v; want %q", err, msg)
	}
	createItem(t, env, svc, goods(m, "SHREE", "Shree steel"))
}

// TestMaster_RenameCodeKeepsLinks is MST-006.
func TestMaster_RenameCodeKeepsLinks(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	id := order(t, env, m, "MS-ANGLE-50")
	if number := mstfixture.Approve(t, env, id); number != "PO/26-27/00001" {
		t.Fatalf("number = %q; want PO/26-27/00001", number)
	}
	shree := m.Parties["SHREE"]
	p := getParty(t, env, svc, shree)
	in := partyInput(p)
	in.Code = "SHREE-STL"
	if _, err := svc.Party.Update(ctx, env.Kavya, p.ID, p.Version, in); err != nil {
		t.Fatalf("rename: %v", err)
	}
	var vendor int64
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT vendor_id FROM mst_order WHERE id = ?`, id).Scan(&vendor)
	})
	if vendor != shree {
		t.Errorf("order vendor_id = %d; want %d", vendor, shree)
	}
	if s := mstfixture.Snapshot(t, env, id); s.VendorName != "Shree Steel Traders" {
		t.Errorf("snapshot vendor = %q; want Shree Steel Traders", s.VendorName)
	}
	es := auditEntries(t, env, party.DocType, shree)
	found := slices.ContainsFunc(es, func(e audit.Entry) bool {
		return slices.ContainsFunc(e.Changes, func(c audit.Change) bool {
			return c.Field == "code" && c.Before == "SHREE" && c.After == "SHREE-STL"
		})
	})
	if !found {
		t.Errorf("audit entries = %+v; want a change code: SHREE → SHREE-STL", es)
	}
}

// TestMaster_RenameSameCodeNoop is Review Focus 2 and CD-208: no error against
// itself, no audit entry, and no version or updated_at change.
func TestMaster_RenameSameCodeNoop(t *testing.T) {
	env, m, svc := seeded(t)
	shree := m.Parties["SHREE"]
	p := getParty(t, env, svc, shree)
	in := partyInput(p)
	in.Code = "shree "
	before := len(auditEntries(t, env, party.DocType, shree))
	row := readRow(t, env, "party", shree)
	env.Clock.Advance(time.Hour)
	saved, err := svc.Party.Update(context.Background(), env.Kavya, p.ID, p.Version, in)
	if err != nil {
		t.Fatalf("Update(shree ) = %v; want nil", err)
	}
	if saved.Version != p.Version {
		t.Errorf("saved version = %d; want %d", saved.Version, p.Version)
	}
	if after := auditEntries(t, env, party.DocType, shree); len(after) != before {
		t.Errorf("audit entries = %d; want %d: %+v", len(after), before, after)
	}
	if got := getParty(t, env, svc, shree); got.Code != "SHREE" {
		t.Errorf("code = %q; want SHREE", got.Code)
	}
	if got := readRow(t, env, "party", shree); got != row {
		t.Errorf("row = %+v; want %+v", got, row)
	}
}

// TestMaster_RenameToExistingCodeRefused is MST-007.
func TestMaster_RenameToExistingCodeRefused(t *testing.T) {
	env, m, svc := seeded(t)
	in := goods(m, "MS-ANGLE-65", "MS Angle 65x65x6")
	saved := createItem(t, env, svc, in)
	in.Code = "ms-angle-50"
	_, err := svc.Item.Update(context.Background(), env.Kavya, saved.ID, saved.Version, in)
	if !hasFieldError(err, doctype.FieldError{Field: "code", Message: msgCodeUsedItem}) {
		t.Errorf("rename to ms-angle-50 = %v; want %q", err, msgCodeUsedItem)
	}
	if got := getItem(t, env, svc, saved.ID); got.Code != "MS-ANGLE-65" || got.Version != saved.Version {
		t.Errorf("renamed item = %+v; want MS-ANGLE-65 unchanged", got)
	}
	if got := getItem(t, env, svc, m.Items["MS-ANGLE-50"]); got.Code != "MS-ANGLE-50" || got.Name != "MS Angle 50x50x5" {
		t.Errorf("existing item = %+v; want unchanged", got)
	}
}

// TestMaster_DisabledHiddenFromSearch is MST-009.
func TestMaster_DisabledHiddenFromSearch(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	bharat := getParty(t, env, svc, m.Parties["BHARAT"])
	if err := svc.Party.SetDisabled(ctx, env.Kavya, bharat.ID, bharat.Version, true); err != nil {
		t.Fatalf("disable BHARAT: %v", err)
	}
	byCode := func(includeDisabled bool) map[string]party.Choice {
		t.Helper()
		got, err := svc.Party.Search(ctx, env.Kavya, party.Vendor, "", includeDisabled)
		if err != nil {
			t.Fatalf("Search(%v): %v", includeDisabled, err)
		}
		out := map[string]party.Choice{}
		for _, c := range got {
			out[c.Code] = c
		}
		return out
	}
	picker := byCode(false)
	if _, ok := picker["SHREE"]; !ok {
		t.Errorf("picker = %v; want SHREE", picker)
	}
	if _, ok := picker["BHARAT"]; ok {
		t.Errorf("picker = %v; want no BHARAT", picker)
	}
	all := byCode(true)
	if c, ok := all["SHREE"]; !ok || c.Disabled {
		t.Errorf("list SHREE = %+v, %v; want enabled", c, ok)
	}
	if c, ok := all["BHARAT"]; !ok || !c.Disabled {
		t.Errorf("list BHARAT = %+v, %v; want Disabled", c, ok)
	}
}

// saveUnchanged posts the draft back exactly as stored, as a page does: no
// value differs, so only CD-199(1) can refuse it.
func saveUnchanged(ctx context.Context, env *msttest.Env, p auth.Principal, id int64) (doc.Result, error) {
	var rec doc.Record
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		rec, err = env.Kit.Engine.Load(ctx, tx, mstfixture.DocType, id)
		return err
	}); err != nil {
		return doc.Result{}, err
	}
	return env.Kit.Engine.Do(ctx, p, mstfixture.DocType, id, doc.ActionSave, doc.ActionInput{Version: rec.Version, Values: rec.Values, Lines: rec.Lines})
}

// TestMaster_DisabledBlocksDraftSave is MST-010, on one draft for every master
// it links (Branch is created here, CD-199; MST-108 needs the seeded ones).
func TestMaster_DisabledBlocksDraftSave(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	branch, err := svc.Company.Create(ctx, env.Kavya, company.BranchInput{Code: "HYD", Name: "Hyderabad Office", StateCode: "36"})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	terms, err := svc.Terms.Create(ctx, env.Kavya, paymentterms.Input{Code: "NET30", Name: "Net 30", Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
	if err != nil {
		t.Fatalf("create terms: %v", err)
	}
	header := shreeHeader(m)
	header.BranchID, header.ProjectID, header.SiteID, header.TermsID = branch.ID, m.Projects["PRJ-METRO"], m.Sites["PUNE-WH"], terms.ID
	line := itemLine(m, "MS-ANGLE-50")
	line.ExpenseTypeID = m.ExpenseTypes["TRAVEL"]
	lines := []mstfixture.Line{line}
	res, err := mstfixture.Save(ctx, env, env.Kavya, 0, header, lines)
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	draft := res.ID

	for _, c := range []struct {
		master string
		table  string
		id     int64
		set    func(context.Context, auth.Principal, int64, int, bool) error
		want   doctype.FieldError
	}{
		{"Vendor", "party", m.Parties["SHREE"], svc.Party.SetDisabled, doctype.FieldError{Field: "vendor_id",
			Message: "Vendor SHREE Shree Steel Traders is disabled. Pick an active vendor, or ask an admin to enable it."}},
		{"Branch", "branch", branch.ID, svc.Company.SetDisabled, doctype.FieldError{Field: "branch_id",
			Message: "Branch HYD Hyderabad Office is disabled. Pick an active branch, or ask an admin to enable it."}},
		{"Project", "project", m.Projects["PRJ-METRO"], svc.Project.SetDisabled, doctype.FieldError{Field: "project_id",
			Message: "Project PRJ-METRO Pune Metro Depot is disabled. Pick an active project, or ask an admin to enable it."}},
		{"Site", "site", m.Sites["PUNE-WH"], svc.Site.SetDisabled, doctype.FieldError{Field: "site_id",
			Message: "Site PUNE-WH PUNE-WH is disabled. Pick an active site, or ask an admin to enable it."}},
		{"Payment Terms", "payment_terms", terms.ID, svc.Terms.SetDisabled, doctype.FieldError{Field: "terms_id",
			Message: "Payment Terms NET30 Net 30 is disabled. Pick an active payment terms, or ask an admin to enable it."}},
		{"Item", "item", m.Items["MS-ANGLE-50"], svc.Item.SetDisabled, doctype.FieldError{Field: "item_id", Line: 1,
			Message: "Line 1: Item MS-ANGLE-50 MS Angle 50x50x5 is disabled. Pick an active item, or ask an admin to enable it."}},
		{"UoM", "uom", m.UoMs["KGS"], svc.UoM.SetDisabled, doctype.FieldError{Field: "uom_id", Line: 1,
			Message: "Line 1: UoM KGS Kilograms is disabled. Pick an active uom, or ask an admin to enable it."}},
		{"Tax Code", "tax_code", m.TaxCodes["GST18"], svc.TaxCode.SetDisabled, doctype.FieldError{Field: "tax_code_id", Line: 1,
			Message: "Line 1: Tax Code GST18 GST 18% is disabled. Pick an active tax code, or ask an admin to enable it."}},
		{"Expense Type", "expense_type", m.ExpenseTypes["TRAVEL"], svc.ExpenseType.SetDisabled, doctype.FieldError{Field: "expense_type_id", Line: 1,
			Message: "Line 1: Expense Type TRAVEL Travel is disabled. Pick an active expense type, or ask an admin to enable it."}},
	} {
		t.Run(c.master, func(t *testing.T) {
			if err := c.set(ctx, env.Kavya, c.id, rowVersion(t, env, c.table, c.id), true); err != nil {
				t.Fatalf("disable: %v", err)
			}
			if _, err := saveUnchanged(ctx, env, env.Kavya, draft); !hasFieldError(err, c.want) {
				t.Errorf("unchanged save = %v; want %+v (CD-199)", err, c.want)
			}
			if _, err := mstfixture.Save(ctx, env, env.Kavya, draft, header, lines); !hasFieldError(err, c.want) {
				t.Errorf("save = %v; want %+v", err, c.want)
			}
			if err := c.set(ctx, env.Kavya, c.id, rowVersion(t, env, c.table, c.id), false); err != nil {
				t.Fatalf("enable: %v", err)
			}
			before := rowVersion(t, env, "mst_order", draft)
			res, err := saveUnchanged(ctx, env, env.Kavya, draft)
			if err != nil {
				t.Fatalf("unchanged save after enable: %v", err)
			}
			if res.Version != before {
				t.Errorf("unchanged save version = %d; want %d: the save must take DOC's no-change path", res.Version, before)
			}
			if _, err := mstfixture.Save(ctx, env, env.Kavya, draft, header, lines); err != nil {
				t.Errorf("save after enable: %v", err)
			}
		})
	}
}

// TestMaster_DisabledVisibleOnOldDocs is MST-011.
func TestMaster_DisabledVisibleOnOldDocs(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	id := order(t, env, m, "MS-ANGLE-50")
	mstfixture.Approve(t, env, id)
	if err := svc.Party.SetDisabled(ctx, env.Kavya, m.Parties["SHREE"], rowVersion(t, env, "party", m.Parties["SHREE"]), true); err != nil {
		t.Fatalf("disable SHREE: %v", err)
	}
	if err := svc.Item.SetDisabled(ctx, env.Kavya, m.Items["MS-ANGLE-50"], rowVersion(t, env, "item", m.Items["MS-ANGLE-50"]), true); err != nil {
		t.Fatalf("disable MS-ANGLE-50: %v", err)
	}
	s := mstfixture.Snapshot(t, env, id)
	if s.VendorName != "Shree Steel Traders" || len(s.Lines) != 1 || s.Lines[0].ItemCode != "MS-ANGLE-50" {
		t.Errorf("snapshot = %+v; want Shree Steel Traders and MS-ANGLE-50", s)
	}
	mstfixture.Cancel(t, env, id)
}

// TestMaster_DeleteUnreferenced is MST-013.
func TestMaster_DeleteUnreferenced(t *testing.T) {
	env, m, svc := seeded(t)
	saved := createItem(t, env, svc, goods(m, "TEST-ONLY", "Test only"))
	if err := svc.Item.Delete(context.Background(), env.Kavya, saved.ID, saved.Version); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n := rowCount(t, env, "item", saved.ID); n != 0 {
		t.Errorf("item rows = %d; want 0", n)
	}
	es := auditEntries(t, env, item.DocType, saved.ID)
	if !slices.ContainsFunc(es, func(e audit.Entry) bool { return e.Action == "delete" }) {
		t.Errorf("audit entries = %+v; want a delete entry", es)
	}
}

// TestMaster_DeleteReferencedRefused is MST-014: a cancelled order, a draft
// order, and another master, which only mstkit's scan can name (CD-167). The
// tax code is created here because every seeded item uses GST18.
func TestMaster_DeleteReferencedRefused(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()

	cancelled := order(t, env, m, "MS-ANGLE-50")
	number := mstfixture.Approve(t, env, cancelled)
	mstfixture.Cancel(t, env, cancelled)
	angle := m.Items["MS-ANGLE-50"]
	err := svc.Item.Delete(ctx, env.Kavya, angle, rowVersion(t, env, "item", angle))
	wantRefused(t, err, "Cannot delete Item MS-ANGLE-50: it is used in "+number+". Disable it instead.")

	wire := m.Items["WIRE-18G"]
	draft := order(t, env, m, "WIRE-18G")
	err = svc.Item.Delete(ctx, env.Kavya, wire, rowVersion(t, env, "item", wire))
	wantRefused(t, err, fmt.Sprintf("Cannot delete Item WIRE-18G: it is used in Draft %d. Disable it instead.", draft))

	tax, err := svc.TaxCode.Create(ctx, env.Kavya, taxcode.Input{Code: "GST-TEST", Name: "Test rate", Treatment: taxcode.Taxable, Rate: gst12})
	if err != nil {
		t.Fatalf("create tax code: %v", err)
	}
	travel := m.ExpenseTypes["TRAVEL"]
	if _, err := svc.ExpenseType.Update(ctx, env.Kavya, travel, rowVersion(t, env, "expense_type", travel),
		expensetype.Input{Code: "TRAVEL", Name: "Travel", DefaultTaxCodeID: tax.ID}); err != nil {
		t.Fatalf("link expense type: %v", err)
	}
	err = svc.TaxCode.Delete(ctx, env.Kavya, tax.ID, tax.Version)
	wantRefused(t, err, "Cannot delete Tax Code GST-TEST: it is used in Expense Type TRAVEL. Disable it instead.")
	if n := rowCount(t, env, "tax_code", tax.ID); n != 1 {
		t.Errorf("tax_code rows = %d; want 1", n)
	}

	// A disabled master still references it (D-MST-05).
	if err := svc.ExpenseType.SetDisabled(ctx, env.Kavya, travel, rowVersion(t, env, "expense_type", travel), true); err != nil {
		t.Fatalf("disable TRAVEL: %v", err)
	}
	err = svc.TaxCode.Delete(ctx, env.Kavya, tax.ID, tax.Version)
	wantRefused(t, err, "Cannot delete Tax Code GST-TEST: it is used in Expense Type TRAVEL. Disable it instead.")
}

// TestMaster_DeleteNamesLineReference has no case ID (CD-167): a UoM used only
// on another item's conversion row, a line field through LinesTable.
func TestMaster_DeleteNamesLineReference(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	pallet, err := svc.UoM.Create(ctx, env.Kavya, uom.Input{Code: "PALLET", Name: "Pallet", UQC: "OTH"})
	if err != nil {
		t.Fatalf("create UoM: %v", err)
	}
	in := goods(m, "ROD-10", "Rod 10 mm")
	in.Conversions = []item.Conversion{{UoMID: pallet.ID, Factor: 40 * uom.FactorOne}}
	createItem(t, env, svc, in)
	err = svc.UoM.Delete(ctx, env.Kavya, pallet.ID, pallet.Version)
	wantRefused(t, err, "Cannot delete UoM PALLET: it is used in Item ROD-10. Disable it instead.")
}

// TestMaster_DeleteNamesSelfReference has no case ID (CD-167): a master's link
// to its own table counts.
func TestMaster_DeleteNamesSelfReference(t *testing.T) {
	env, m, svc := seeded(t)
	id := m.Employees["EMP-004"]
	err := svc.Employee.Delete(context.Background(), env.Kavya, id, rowVersion(t, env, "employee", id))
	wantRefused(t, err, "Cannot delete Employee EMP-004: it is used in Employee EMP-007. Disable it instead.")
}

// TestMaster_DeleteSkipsOwnedRows has no case ID (CD-167, MST-046): the rows a
// master owns go with it, on both owner kinds.
func TestMaster_DeleteSkipsOwnedRows(t *testing.T) {
	env, _, svc := seeded(t)
	ctx := context.Background()
	line := address.Input{Title: "Office", Line1: "1 Main Road", City: "Hyderabad", StateCode: "36", Pincode: "500001", IsBilling: true}
	contact := address.ContactInput{FirstName: "Sunil", LastName: "Mehta"}
	own := func(t *testing.T, a address.Input, c address.ContactInput) (int64, int64) {
		t.Helper()
		ad, err := svc.Address.Create(ctx, env.Kavya, a)
		if err != nil {
			t.Fatalf("create address: %v", err)
		}
		co, err := svc.Address.CreateContact(ctx, env.Kavya, c)
		if err != nil {
			t.Fatalf("create contact: %v", err)
		}
		return ad.ID, co.ID
	}
	gone := func(t *testing.T, addressID, contactID int64) {
		t.Helper()
		if n := rowCount(t, env, "address", addressID) + rowCount(t, env, "contact", contactID); n != 0 {
			t.Errorf("owned rows left = %d; want 0", n)
		}
	}
	t.Run("Party", func(t *testing.T) {
		saved, err := svc.Party.Create(ctx, env.Kavya, party.Input{Code: "C-TATA", Name: "Tata Projects", IsCustomer: true, GSTCategory: gstin.Unregistered})
		if err != nil {
			t.Fatalf("create party: %v", err)
		}
		a, c := line, contact
		a.PartyID, c.PartyID = saved.ID, saved.ID
		addressID, contactID := own(t, a, c)
		if err := svc.Party.Delete(ctx, env.Kavya, saved.ID, rowVersion(t, env, "party", saved.ID)); err != nil {
			t.Fatalf("delete party: %v", err)
		}
		gone(t, addressID, contactID)
	})
	t.Run("Branch", func(t *testing.T) {
		saved, err := svc.Company.Create(ctx, env.Kavya, company.BranchInput{Code: "HYD", Name: "Hyderabad Office", StateCode: "36"})
		if err != nil {
			t.Fatalf("create branch: %v", err)
		}
		a, c := line, contact
		a.BranchID, c.BranchID = saved.ID, saved.ID
		addressID, contactID := own(t, a, c)
		if err := svc.Company.Delete(ctx, env.Kavya, saved.ID, rowVersion(t, env, "branch", saved.ID)); err != nil {
			t.Fatalf("delete branch: %v", err)
		}
		gone(t, addressID, contactID)
	})
}

// branchWithRows creates branch HYD with an address and a contact of its own.
func branchWithRows(t *testing.T, env *msttest.Env, svc *allmasters.Services) (branch, addressID, contactID int64) {
	t.Helper()
	ctx := context.Background()
	saved, err := svc.Company.Create(ctx, env.Kavya, company.BranchInput{Code: "HYD", Name: "Hyderabad Office", StateCode: "36"})
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}
	ad, err := svc.Address.Create(ctx, env.Kavya, address.Input{
		BranchID: saved.ID, Title: "Office", Line1: "1 Main Road", City: "Hyderabad", StateCode: "36", Pincode: "500001", IsBilling: true,
	})
	if err != nil {
		t.Fatalf("create address: %v", err)
	}
	co, err := svc.Address.CreateContact(ctx, env.Kavya, address.ContactInput{BranchID: saved.ID, FirstName: "Sunil", LastName: "Mehta"})
	if err != nil {
		t.Fatalf("create contact: %v", err)
	}
	return saved.ID, ad.ID, co.ID
}

// TestMaster_DeleteBranchRefusedByOwnedRow has no case ID (CD-167, MST-014 for
// a branch): a document on one of the branch's own rows blocks the delete,
// through the branch_id children. The draft's own branch is PUNE, so only the
// owned row can name it.
func TestMaster_DeleteBranchRefusedByOwnedRow(t *testing.T) {
	ctx := context.Background()
	wantKept := func(t *testing.T, env *msttest.Env, branch, addressID, contactID int64) {
		t.Helper()
		if b, a, c := rowCount(t, env, "branch", branch), rowCount(t, env, "address", addressID), rowCount(t, env, "contact", contactID); b != 1 || a != 1 || c != 1 {
			t.Errorf("rows after refused delete: branch %d, address %d, contact %d; want 1 each", b, a, c)
		}
	}
	t.Run("Address on a draft", func(t *testing.T) {
		env, m, svc := seeded(t)
		branch, addressID, contactID := branchWithRows(t, env, svc)
		// No vendor: the fixture's hook takes a party's addresses only.
		res, err := mstfixture.Save(ctx, env, env.Kavya, 0,
			mstfixture.Header{AddressID: addressID, BranchID: m.Branches["PUNE"], Date: orderDate}, []mstfixture.Line{itemLine(m, "MS-ANGLE-50")})
		if err != nil {
			t.Fatalf("save draft: %v", err)
		}
		err = svc.Company.Delete(ctx, env.Kavya, branch, rowVersion(t, env, "branch", branch))
		wantRefused(t, err, fmt.Sprintf("Cannot delete Branch HYD: it is used in Draft %d. Disable it instead.", res.ID))
		wantKept(t, env, branch, addressID, contactID)
	})
	t.Run("Contact on a document", func(t *testing.T) {
		env, _, _ := seeded(t)
		docs := docfake.New()
		kit := env.Kit
		kit.Engine = docs
		svc := allmasters.New(kit)
		branch, addressID, contactID := branchWithRows(t, env, svc)
		docs.AddReference("contact", contactID, doc.Reference{
			DocType: "Purchase Order", Field: "contact_id", ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved,
		})
		err := svc.Company.Delete(ctx, env.Kavya, branch, rowVersion(t, env, "branch", branch))
		wantRefused(t, err, "Cannot delete Branch HYD: it is used in PO/26-27/00001. Disable it instead.")
		wantKept(t, env, branch, addressID, contactID)
	})
}

// TestMaster_FieldExtras has no case ID (CD-42, M13).
func TestMaster_FieldExtras(t *testing.T) {
	selects := map[string][]string{
		party.DocType + ".gst_category": {"Registered Regular", "Registered Composition", "Unregistered", "SEZ", "Overseas"},
		item.DocType + ".item_type":     {"Goods", "Service"},
		taxcode.DocType + ".treatment":  {"Taxable", "Nil-Rated", "Exempted", "Non-GST", "Zero-Rated"},
		project.DocType + ".status":     {"Open", "On Hold", "Completed", "Cancelled"},
		site.DocType + ".type":          {"warehouse", "site"},
		employee.DocType + ".status":    {"Active", "Left"},
	}
	masters := map[string]bool{}
	seen := map[string]bool{}
	for _, dt := range doctype.All() {
		if !dt.IsMaster() {
			continue
		}
		masters[dt.Name] = true
		for _, f := range dt.Fields {
			if f.Name == "name" && f.ListPriority != 1 || f.Name != "name" && f.ListPriority == 1 {
				t.Errorf("%s.%s ListPriority = %d; want 1 on name only", dt.Name, f.Name, f.ListPriority)
			}
			if f.Name == "code" && (f.ListPriority != 4 || f.MaxLen != 20) {
				t.Errorf("%s.code ListPriority = %d, MaxLen = %d; want 4, 20", dt.Name, f.ListPriority, f.MaxLen)
			}
			if f.Step != "" && dt.Name != employee.DocType {
				t.Errorf("%s.%s Step = %q; only Employee has steps", dt.Name, f.Name, f.Step)
			}
			if f.Type == doctype.Select {
				key := dt.Name + "." + f.Name
				seen[key] = true
				if want := selects[key]; len(f.Options) == 0 || !slices.Equal(f.Options, want) {
					t.Errorf("%s Options = %q; want %q", key, f.Options, want)
				}
			}
		}
	}
	for key := range selects {
		if !seen[key] {
			t.Errorf("Select %s is not registered", key)
		}
	}
	for _, name := range []string{company.CompanyDocType, company.DocType, party.DocType, address.DocType, address.ContactDocType, paymentterms.DocType,
		item.DocType, uom.DocType, taxcode.DocType, project.DocType, site.DocType, employee.DocType, expensetype.DocType} {
		if !masters[name] {
			t.Errorf("master %s is not registered", name)
		}
	}
}

// TestMaster_DisableAudited is MST-016.
func TestMaster_DisableAudited(t *testing.T) {
	env, m, svc := seeded(t)
	ctx := context.Background()
	saved := createItem(t, env, svc, goods(m, "AUDIT-ME", "Audited item"))
	if err := svc.Item.SetDisabled(ctx, env.Kavya, saved.ID, saved.Version, true); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := svc.Item.SetDisabled(ctx, env.Kavya, saved.ID, saved.Version+1, false); err != nil {
		t.Fatalf("enable: %v", err)
	}
	var got []audit.Entry
	for _, e := range auditEntries(t, env, item.DocType, saved.ID) {
		if e.Action == "disable" || e.Action == "enable" {
			got = append(got, e)
		}
	}
	if len(got) != 2 {
		t.Fatalf("entries = %+v; want 2", got)
	}
	for i, want := range []struct{ action, before, after string }{
		{"disable", "false", "true"}, {"enable", "true", "false"},
	} {
		e := got[i]
		if e.Action != want.action || e.By != env.Kavya.ID || !e.At.Equal(env.Clock.Now()) || e.At.Location() != time.UTC {
			t.Errorf("entry %d = %+v; want %s by Kavya at %v UTC", i, e, want.action, env.Clock.Now())
		}
		if len(e.Changes) != 1 || e.Changes[0].Field != "disabled" || e.Changes[0].Before != want.before || e.Changes[0].After != want.after {
			t.Errorf("entry %d changes = %+v; want disabled: %s → %s", i, e.Changes, want.before, want.after)
		}
	}
}
