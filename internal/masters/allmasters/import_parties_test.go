package allmasters_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// The templates' columns (TestImport_Columns).
const (
	partiesPartyHeader   = "Code,Name,Vendor,Customer,PAN,GST Category,Payment Terms,Disabled"
	partiesAddressHeader = "Party,Title,Line 1,Line 2,City,State,Country,Pincode,GSTIN,Billing,Shipping,Disabled"
	partiesContactHeader = "Party,First Name,Middle Name,Last Name,Email,Mobile,Primary"
)

const (
	partiesImportReason = "created by import" // csvio's create entry (CD-60)
	partiesShreeGSTIN   = "27AABCS1429B1ZU"   // MST-044, Maharashtra, PAN AABCS1429B
	partiesShreePAN     = "AABCS1429B"
	partiesCoromGSTIN   = "33AAACT2727Q1Z3" // MST-049, Tamil Nadu, PAN AAACT2727Q
	partiesKeepPrimary  = "This is the party's primary billing address. Make another billing address primary first."
)

// partiesWorld is a fresh database with the importers on its services. Not
// parallel: RegisterImporters' Services are process-wide.
type partiesWorld struct {
	env *msttest.Env
	svc *allmasters.Services
	im  *csvio.Importer
}

func partiesSetup(t *testing.T) *partiesWorld {
	t.Helper()
	env := msttest.New(t)
	svc := allmasters.New(env.Kit)
	allmasters.RegisterImporters(svc)
	return &partiesWorld{env: env, svc: svc, im: csvio.NewImporter(env.DB, env.Kit.Auth, env.Kit.Clock, env.Kit.Auditor)}
}

// partiesCSV is a CSV file of header and rows.
func partiesCSV(header string, rows ...string) string {
	return strings.Join(append([]string{header}, rows...), "\n") + "\n"
}

// partiesAddressRow is one Address row; the title is the city.
func partiesAddressRow(code, city, state, pincode, gstinCell, billing, shipping, disabled string) string {
	return strings.Join([]string{code, city, "1 Main Road", "", city, state, "India", pincode, gstinCell, billing, shipping, disabled}, ",")
}

func (w *partiesWorld) importFile(t *testing.T, docType, body string) csvio.Report {
	t.Helper()
	rep, err := w.im.Import(context.Background(), w.env.Kavya, registered(t, docType), strings.NewReader(body))
	if err != nil {
		t.Fatalf("Import(%s): %v", docType, err)
	}
	return rep
}

func (w *partiesWorld) previewFile(t *testing.T, docType, body string) csvio.Report {
	t.Helper()
	rep, err := w.im.Preview(context.Background(), w.env.Kavya, registered(t, docType), strings.NewReader(body))
	if err != nil {
		t.Fatalf("Preview(%s): %v", docType, err)
	}
	return rep
}

// partiesWantErrors checks the row errors as a user reads them, in order.
func partiesWantErrors(t *testing.T, rep csvio.Report, want ...string) {
	t.Helper()
	got := make([]string, 0, len(rep.Errors))
	for _, e := range rep.Errors {
		got = append(got, e.String())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("row errors = %q\nwant %q", got, want)
	}
}

// partiesWantImported checks that every one of the file's rows was imported.
func partiesWantImported(t *testing.T, rep csvio.Report, rows int) {
	t.Helper()
	partiesWantErrors(t, rep)
	if len(rep.Warnings) != 0 {
		t.Fatalf("warnings = %q, want none", rep.Warnings)
	}
	if rep.Rows != rows || rep.Imported != rows {
		t.Fatalf("Rows = %d, Imported = %d; want %d of each", rep.Rows, rep.Imported, rows)
	}
}

// partiesWantNotImported checks a refused import's report.
func partiesWantNotImported(t *testing.T, rep csvio.Report, rows, ready int) {
	t.Helper()
	if rep.Rows != rows || rep.Ready != ready || rep.Imported != 0 {
		t.Fatalf("Rows = %d, Ready = %d, Imported = %d; want %d, %d, 0", rep.Rows, rep.Ready, rep.Imported, rows, ready)
	}
}

func (w *partiesWorld) createParty(t *testing.T, in party.Input) int64 {
	t.Helper()
	saved, err := w.svc.Party.Create(context.Background(), w.env.Kavya, in)
	if err != nil {
		t.Fatalf("Party.Create(%s): %v", in.Code, err)
	}
	return saved.ID
}

// createVendor adds a vendor with no PAN, so category Unregistered.
func (w *partiesWorld) createVendor(t *testing.T, code string) int64 {
	t.Helper()
	return w.createParty(t, party.Input{Code: code, Name: "Vendor " + code, IsVendor: true})
}

func (w *partiesWorld) partyByCode(t *testing.T, code string) party.Party {
	t.Helper()
	var p party.Party
	var found bool
	read(t, w.env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		p, found, err = w.svc.Party.ByCode(ctx, tx, code)
		return err
	})
	if !found {
		t.Fatalf("party %s not stored", code)
	}
	return p
}

// addresses lists the party's addresses, disabled ones too, in import order.
func (w *partiesWorld) addresses(t *testing.T, partyID int64) []address.Address {
	t.Helper()
	var out []address.Address
	read(t, w.env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		out, err = w.svc.Address.ForParty(ctx, tx, partyID, true)
		return err
	})
	return out
}

// contact reads the contact with the given first name through address.Service.
func (w *partiesWorld) contact(t *testing.T, firstName string) address.Contact {
	t.Helper()
	var c address.Contact
	read(t, w.env, func(ctx context.Context, tx *db.Tx) error {
		var id int64
		err := tx.SQL().QueryRowContext(ctx, `SELECT id FROM contact WHERE first_name = ?`, firstName).Scan(&id)
		if err != nil {
			return fmt.Errorf("contact %s: %w", firstName, err)
		}
		c, err = w.svc.Address.GetContact(ctx, tx, id)
		return err
	})
	return c
}

// partiesCount counts the rows of table.
func partiesCount(t *testing.T, env *msttest.Env, table string) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&n) //nolint:gosec // G202: fixed test table names
	})
	return n
}

func partiesAuditCount(t *testing.T, env *msttest.Env) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		n, err = audit.Count(ctx, tx)
		return err
	})
	return n
}

// partiesCreateCount counts the create entries of docType.
func partiesCreateCount(t *testing.T, env *msttest.Env, docType string) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT count(*) FROM audit_log WHERE doc_type = ? AND action = 'create'`, docType).Scan(&n)
	})
	return n
}

// partiesWantOneCreate checks that the record has exactly one create entry and
// that it is csvio's: a master-written one would have no reason, or be a second.
func partiesWantOneCreate(t *testing.T, env *msttest.Env, docType string, id int64) {
	t.Helper()
	var creates []audit.Entry
	for _, e := range auditEntries(t, env, docType, id) {
		if e.Action == "create" {
			creates = append(creates, e)
		}
	}
	if len(creates) != 1 || creates[0].Reason != partiesImportReason {
		t.Errorf("%s %d: create entries = %+v, want exactly one with reason %q", docType, id, creates, partiesImportReason)
	}
}

// partiesActions lists "action|reason" for the record's entries, sorted.
func partiesActions(t *testing.T, env *msttest.Env, docType string, id int64) []string {
	t.Helper()
	got := []string{}
	for _, e := range auditEntries(t, env, docType, id) {
		got = append(got, e.Action+"|"+e.Reason)
	}
	slices.Sort(got)
	return got
}

func TestImport_PartyUsesFormValidation(t *testing.T) {
	w := partiesSetup(t)
	rep := w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader,
		"SHREE,Shree Steel Traders,Yes,No,AABCS1429,Registered Regular,,No"))
	partiesWantErrors(t, rep, "Row 2: PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G).")
	partiesWantNotImported(t, rep, 1, 0)
	if n := partiesCount(t, w.env, "party"); n != 0 {
		t.Errorf("%d parties stored, want 0", n)
	}
}

// partiesHundredAddresses is a 100-row address file, rows 2 to 101: shipping
// addresses without a GSTIN, and on row 57 the billing address with gstinCell.
func partiesHundredAddresses(code, gstinCell string) string {
	rows := make([]string, 0, 100)
	for num := 2; num <= 101; num++ {
		if num == 57 {
			rows = append(rows, partiesAddressRow(code, "Mumbai", "27", "400001", gstinCell, "Yes", "No", "No"))
			continue
		}
		rows = append(rows, partiesAddressRow(code, "Thane", "27", "400601", "", "No", "Yes", "No"))
	}
	return partiesCSV(partiesAddressHeader, rows...)
}

func TestImport_AddressGSTINCheckDigit(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "V-NEW")
	auditBefore := partiesAuditCount(t, w.env)

	rep := w.importFile(t, address.DocType, partiesHundredAddresses("V-NEW", "27AABCS1429B1Z5"))
	partiesWantErrors(t, rep, "Row 57: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes.")
	partiesWantNotImported(t, rep, 100, 99)
	if n := partiesCount(t, w.env, "address"); n != 0 {
		t.Fatalf("%d addresses stored after the refused file, want 0", n)
	}
	if n := partiesAuditCount(t, w.env); n != auditBefore {
		t.Fatalf("audit entries %d after the refused file, want %d", n, auditBefore)
	}

	partiesWantImported(t, w.importFile(t, address.DocType, partiesHundredAddresses("V-NEW", partiesShreeGSTIN)), 100)
	stored := w.addresses(t, id)
	if len(stored) != 100 {
		t.Fatalf("%d addresses stored, want 100", len(stored))
	}
	for _, a := range stored {
		partiesWantOneCreate(t, w.env, address.DocType, a.ID)
	}
	if n := partiesCreateCount(t, w.env, address.DocType); n != 100 {
		t.Errorf("%d Address create entries, want 100", n)
	}

	// MST-049: the GSTIN back-fills the party, audited as one save.
	got := getParty(t, w.env, w.svc, id)
	if got.PAN != partiesShreePAN || got.GSTCategory != gstin.Regular {
		t.Errorf("party PAN = %q, category = %q; want %s, %s", got.PAN, got.GSTCategory, partiesShreePAN, gstin.Regular)
	}
	var saves []audit.Entry
	for _, e := range auditEntries(t, w.env, party.DocType, id) {
		if e.Action == "save" {
			saves = append(saves, e)
		}
	}
	want := []audit.Change{
		{Op: audit.OpSet, Field: "gst_category", Before: string(gstin.Unregistered), After: string(gstin.Regular)},
		{Op: audit.OpSet, Field: "pan", Before: "", After: partiesShreePAN},
	}
	if len(saves) != 1 || !slices.Equal(saves[0].Changes, want) {
		t.Errorf("party save entries = %+v, want one with changes %+v", saves, want)
	}
}

// A row must see the rows before it in the same file (Review Focus 5). Neither
// party has the GSTIN until row 2's address, which nothing has committed yet.
func TestImport_DuplicateGSTINWithinFile(t *testing.T) {
	w := partiesSetup(t)
	alpha := w.createVendor(t, "ALPHA")
	w.createVendor(t, "BETA")
	auditBefore := partiesAuditCount(t, w.env)
	file := partiesCSV(partiesAddressHeader,
		partiesAddressRow("ALPHA", "Chennai", "33", "600001", partiesCoromGSTIN, "Yes", "No", "No"),
		partiesAddressRow("BETA", "Chennai", "33", "600001", partiesCoromGSTIN, "Yes", "No", "No"))
	const want = "Row 3: GSTIN " + partiesCoromGSTIN + " is already used by party ALPHA. Use that party instead."

	for name, run := range map[string]func(*testing.T, string, string) csvio.Report{"Preview": w.previewFile, "Import": w.importFile} {
		rep := run(t, address.DocType, file)
		partiesWantErrors(t, rep, want)
		partiesWantNotImported(t, rep, 2, 1)
		if n := partiesCount(t, w.env, "address"); n != 0 {
			t.Errorf("%s stored %d addresses, want 0", name, n)
		}
		if n := partiesAuditCount(t, w.env); n != auditBefore {
			t.Errorf("%s left %d audit entries, want %d", name, n, auditBefore)
		}
		if p := getParty(t, w.env, w.svc, alpha); p.PAN != "" || p.GSTCategory != gstin.Unregistered {
			t.Errorf("%s changed ALPHA: PAN %q, category %q", name, p.PAN, p.GSTCategory)
		}
	}
}

func TestImport_AddressStateByNameOrCode(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "LOCAL")
	partiesWantImported(t, w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		partiesAddressRow("LOCAL", "Mumbai", "maharashtra", "400001", "", "Yes", "No", "No"),
		partiesAddressRow("LOCAL", "Pune", "MAHARASHTRA", "411001", "", "Yes", "No", "No"),
		partiesAddressRow("LOCAL", "Thane", "27", "400601", "", "Yes", "No", "No"))), 3)
	stored := w.addresses(t, id)
	if len(stored) != 3 {
		t.Fatalf("%d addresses stored, want 3", len(stored))
	}
	for _, a := range stored {
		if a.StateCode != "27" {
			t.Errorf("%s: state = %q, want 27", a.City, a.StateCode)
		}
	}

	rep := w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		partiesAddressRow("LOCAL", "Mumbai", "Atlantis", "400001", "", "Yes", "No", "No")))
	partiesWantErrors(t, rep, "Row 2: State is required for an Indian address.")
	partiesWantNotImported(t, rep, 1, 0)
}

func TestImport_SampleFilesLoad(t *testing.T) {
	w := partiesSetup(t)
	files := []struct {
		name, docType, table string
		rows                 int
	}{
		{"parties.csv", party.DocType, "party", 4},
		{"addresses.csv", address.DocType, "address", 5},
		{"contacts.csv", address.ContactDocType, "contact", 4},
	}
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "masters", f.name))
		if err != nil {
			t.Fatal(err)
		}
		partiesWantImported(t, w.importFile(t, f.docType, string(data)), f.rows)
		if n := partiesCount(t, w.env, f.table); n != f.rows {
			t.Errorf("%s: %d rows stored, want %d", f.name, n, f.rows)
		}
	}
	// Task 21's projects.csv names this customer.
	if p := w.partyByCode(t, "CUST-SAMPLE"); !p.IsCustomer {
		t.Error("CUST-SAMPLE is not a customer")
	}
}

func TestImport_ContactEmailMobile(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "SHREE")
	partiesWantImported(t, w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"SHREE,Sunil,,Mehta,Accounts@ShreeSteel.test,98200 12345,No",
		"SHREE,Kiran,,Joshi,,,No")), 2)

	sunil := w.contact(t, "Sunil")
	if sunil.PartyID != id {
		t.Errorf("Sunil's party = %d, want %d", sunil.PartyID, id)
	}
	if want := []address.EmailInput{{Email: "accounts@shreesteel.test", Primary: true}}; !slices.Equal(sunil.Emails, want) {
		t.Errorf("emails = %+v, want %+v", sunil.Emails, want)
	}
	if want := []address.PhoneInput{{Number: "+919820012345", Mobile: true, Primary: true}}; !slices.Equal(sunil.Phones, want) {
		t.Errorf("phones = %+v, want %+v", sunil.Phones, want)
	}
	if kiran := w.contact(t, "Kiran"); len(kiran.Emails) != 0 || len(kiran.Phones) != 0 {
		t.Errorf("blank cells stored emails %+v, phones %+v", kiran.Emails, kiran.Phones)
	}

	rep := w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"SHREE,Anita,,Rao,accounts@shreesteel,,No",
		"SHREE,Vijay,,Nair,,5820012345,No"))
	partiesWantErrors(t, rep,
		"Row 2: Enter a valid email address, e.g. accounts@shreesteel.test.",
		"Row 3: Enter a 10-digit Indian mobile number starting 6–9, optionally with +91.")
	partiesWantNotImported(t, rep, 2, 0)
}

func TestImport_ContactPrimary(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "SHREE")
	if _, err := w.svc.Address.CreateContact(context.Background(), w.env.Kavya,
		address.ContactInput{PartyID: id, FirstName: "Sunil", LastName: "Mehta", Primary: true}); err != nil {
		t.Fatal(err)
	}

	partiesWantImported(t, w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"SHREE,Anita,,Rao,,,No")), 1)
	if !w.contact(t, "Sunil").Primary || w.contact(t, "Anita").Primary {
		t.Fatal("Primary No changed the party's primary contact")
	}

	partiesWantImported(t, w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"SHREE,Kiran,,Joshi,,,Yes")), 1)
	if !w.contact(t, "Kiran").Primary {
		t.Error("Kiran is not primary after Primary Yes")
	}
	if w.contact(t, "Sunil").Primary {
		t.Error("Sunil is still primary (MST-066)")
	}
}

func TestImport_ContactNeedsParty(t *testing.T) {
	w := partiesSetup(t)
	rep := w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader, ",Kiran,,Joshi,,,No"))
	partiesWantErrors(t, rep, "Row 2: A contact must belong to one party or one branch.")
	partiesWantNotImported(t, rep, 1, 0)
	if n := partiesCount(t, w.env, "contact"); n != 0 {
		t.Errorf("%d contacts stored, want 0", n)
	}
}

func TestImport_PartyDisabledCell(t *testing.T) {
	w := partiesSetup(t)
	cases := []struct {
		code, cell string
		disabled   bool
	}{
		{"D-TRUE", "true", true},
		{"D-YES", "Yes", true},
		{"D-FALSE", "false", false},
		{"D-BLANK", "", false},
	}
	rows := make([]string, 0, len(cases))
	for _, c := range cases {
		rows = append(rows, fmt.Sprintf("%s,Party %s,Yes,,,,,%s", c.code, c.code, c.cell))
	}
	partiesWantImported(t, w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader, rows...)), len(cases))

	for _, c := range cases {
		p := w.partyByCode(t, c.code)
		if p.Disabled != c.disabled {
			t.Errorf("%s: disabled = %v, want %v", c.code, p.Disabled, c.disabled)
		}
		want := []string{"create|" + partiesImportReason}
		if c.disabled {
			want = append(want, "disable|")
		}
		if got := partiesActions(t, w.env, party.DocType, p.ID); !slices.Equal(got, want) {
			t.Errorf("%s: audit entries = %q, want %q", c.code, got, want)
		}
	}
}

func TestImport_AddressDisabledCell(t *testing.T) {
	w := partiesSetup(t)
	id := w.createVendor(t, "LOCAL")
	cases := []struct {
		city, pincode, cell string
		disabled            bool
	}{
		{"Mumbai", "400001", "true", true},
		{"Thane", "400601", "Yes", true},
		{"Pune", "411001", "false", false},
		{"Nashik", "422001", "", false},
	}
	rows := make([]string, 0, len(cases))
	for _, c := range cases {
		rows = append(rows, partiesAddressRow("LOCAL", c.city, "27", c.pincode, "", "Yes", "No", c.cell))
	}
	partiesWantImported(t, w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader, rows...)), len(cases))

	stored := w.addresses(t, id)
	if len(stored) != len(cases) {
		t.Fatalf("%d addresses stored, want %d", len(stored), len(cases))
	}
	for i, c := range cases {
		a := stored[i]
		if a.City != c.city || a.Disabled != c.disabled {
			t.Errorf("row %d: %s disabled = %v, want %s %v", i+2, a.City, a.Disabled, c.city, c.disabled)
		}
		if a.Disabled && a.PrimaryBilling {
			t.Errorf("%s: a disabled address is primary (CD-180)", a.City)
		}
		partiesWantOneCreate(t, w.env, address.DocType, a.ID)
	}
}

// A Disabled cell on a registered party's first billing address makes
// SetDisabledTx refuse (keepPrimaryBilling, CD-177): a row error, not an
// aborted import.
func TestImport_AddressDisableRefused(t *testing.T) {
	w := partiesSetup(t)
	w.createParty(t, party.Input{Code: "SHREE", Name: "Shree Steel Traders", IsVendor: true, PAN: partiesShreePAN, GSTCategory: gstin.Regular})
	w.createVendor(t, "LOCAL")
	auditBefore := partiesAuditCount(t, w.env)
	file := partiesCSV(partiesAddressHeader,
		partiesAddressRow("SHREE", "Mumbai", "27", "400001", partiesShreeGSTIN, "Yes", "No", "true"),
		partiesAddressRow("LOCAL", "Pune", "27", "411001", "", "Yes", "No", "No"))

	rep := w.previewFile(t, address.DocType, file)
	partiesWantErrors(t, rep, "Row 2: "+partiesKeepPrimary)
	partiesWantNotImported(t, rep, 2, 1)

	rep = w.importFile(t, address.DocType, file)
	partiesWantErrors(t, rep, "Row 2: "+partiesKeepPrimary)
	partiesWantNotImported(t, rep, 2, 1)
	if n := partiesCount(t, w.env, "address"); n != 0 {
		t.Errorf("%d addresses stored, want 0", n)
	}
	if n := partiesAuditCount(t, w.env); n != auditBefore {
		t.Errorf("audit entries %d, want %d", n, auditBefore)
	}
}

func TestImport_SelectCellsIgnoreCaseParties(t *testing.T) {
	w := partiesSetup(t)
	cases := []struct {
		code, cell string
		want       gstin.Category
	}{
		{"G-REGULAR", "registered regular", gstin.Regular},
		{"G-COMPOSITION", "REGISTERED COMPOSITION", gstin.Composition},
		{"G-UNREG", "UNREGISTERED", gstin.Unregistered},
		{"G-SEZ", "sez", gstin.SEZ},
		{"G-OVERSEAS", "OverSeas", gstin.Overseas},
	}
	rows := make([]string, 0, len(cases))
	for _, c := range cases {
		rows = append(rows, fmt.Sprintf("%s,Party %s,Yes,,,%s,,", c.code, c.code, c.cell))
	}
	partiesWantImported(t, w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader, rows...)), len(cases))
	for _, c := range cases {
		if got := w.partyByCode(t, c.code).GSTCategory; got != c.want {
			t.Errorf("%s: category = %q, want %q", c.code, got, c.want)
		}
	}

	rep := w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader, "G-BAD,Party G-BAD,Yes,,,Exempt,,"))
	partiesWantErrors(t, rep, "Row 2: GST category must be Registered Regular, Registered Composition, Unregistered, SEZ or Overseas.")
	partiesWantNotImported(t, rep, 1, 0)
}

func TestImport_PartyPaymentTerms(t *testing.T) {
	w := partiesSetup(t)
	ctx := context.Background()
	if _, err := w.svc.Terms.Create(ctx, w.env.Kavya, paymentterms.Input{Code: "NET15", Name: "Net 15", Rows: []paymentterms.Row{{Portion: 100_000, Days: 15}}}); err != nil {
		t.Fatal(err)
	}
	terms, err := w.svc.Terms.Create(ctx, w.env.Kavya, paymentterms.Input{Code: "NET30", Name: "Net 30", Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}})
	if err != nil {
		t.Fatal(err)
	}

	partiesWantImported(t, w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader,
		"P-TERMS,Vendor With Terms,Yes,,,,NET30,",
		"P-NONE,Vendor Without Terms,Yes,,,,,")), 2)
	if got := w.partyByCode(t, "P-TERMS").DefaultPaymentTermsID; got != terms.ID {
		t.Errorf("P-TERMS terms id = %d, want %d", got, terms.ID)
	}
	if got := w.partyByCode(t, "P-NONE").DefaultPaymentTermsID; got != 0 {
		t.Errorf("P-NONE terms id = %d, want 0", got)
	}

	rep := w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader, "P-BAD,Vendor Bad Terms,Yes,,,,NET99,"))
	partiesWantErrors(t, rep, "Row 2: Payment Terms 'NET99' not found. Use an existing Payment Terms code such as NET15.")
	partiesWantNotImported(t, rep, 1, 0)
}

func TestImport_OneCreateAuditParties(t *testing.T) {
	w := partiesSetup(t)
	const n = 3
	partiesWantImported(t, w.importFile(t, party.DocType, partiesCSV(partiesPartyHeader,
		"V-ONE,Vendor One,Yes,,,,,",
		"V-TWO,Vendor Two,Yes,,,,,",
		"C-ONE,Customer One,,Yes,,,,")), n)
	partiesWantImported(t, w.importFile(t, address.DocType, partiesCSV(partiesAddressHeader,
		partiesAddressRow("V-ONE", "Mumbai", "27", "400001", "", "Yes", "No", "No"),
		partiesAddressRow("V-TWO", "Pune", "27", "411001", "", "Yes", "No", "No"),
		partiesAddressRow("C-ONE", "Thane", "27", "400601", "", "Yes", "No", "No"))), n)
	partiesWantImported(t, w.importFile(t, address.ContactDocType, partiesCSV(partiesContactHeader,
		"V-ONE,Sunil,,Mehta,,,No",
		"V-TWO,Kiran,,Joshi,,,No",
		"C-ONE,Anita,,Rao,,,No")), n)

	for _, docType := range []string{party.DocType, address.DocType, address.ContactDocType} {
		if got := partiesCreateCount(t, w.env, docType); got != n {
			t.Errorf("%s: %d create entries, want %d", docType, got, n)
		}
	}
	for _, code := range []string{"V-ONE", "V-TWO", "C-ONE"} {
		p := w.partyByCode(t, code)
		partiesWantOneCreate(t, w.env, party.DocType, p.ID)
		for _, a := range w.addresses(t, p.ID) {
			partiesWantOneCreate(t, w.env, address.DocType, a.ID)
		}
	}
	for _, first := range []string{"Sunil", "Kiran", "Anita"} {
		partiesWantOneCreate(t, w.env, address.ContactDocType, w.contact(t, first).ID)
	}
}

func TestImport_PreviewStoresNothing(t *testing.T) {
	w := partiesSetup(t)
	auditBefore := partiesAuditCount(t, w.env)
	rep := w.previewFile(t, party.DocType, partiesCSV(partiesPartyHeader,
		"V-ONE,Vendor One,Yes,,,,,",
		"V-TWO,Vendor Two,Yes,,,,,No",
		"V-OFF,Vendor Off,Yes,,,,,Yes"))
	partiesWantErrors(t, rep)
	if rep.Rows != 3 || rep.Ready != 3 || rep.Imported != 0 {
		t.Fatalf("Rows = %d, Ready = %d, Imported = %d; want 3, 3, 0", rep.Rows, rep.Ready, rep.Imported)
	}
	if n := partiesCount(t, w.env, "party"); n != 0 {
		t.Errorf("Preview stored %d parties, want 0", n)
	}
	if n := partiesAuditCount(t, w.env); n != auditBefore {
		t.Errorf("Preview left %d audit entries, want %d", n, auditBefore)
	}
}
