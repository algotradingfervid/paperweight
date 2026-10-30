package allmasters_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The templates' header rows.
const (
	othersUoMHeader      = "Code,Name,Whole number,UQC,Disabled"
	othersTaxHeader      = "Code,Name,Treatment,Rate,Disabled"
	othersItemHeader     = "Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled"
	othersExpenseHeader  = "Code,Name,Default HSN/SAC,Default tax code,Disabled"
	othersProjectHeader  = "Code,Name,Branch,Customer,Start date,End date,Status,Disabled"
	othersSiteHeader     = "Code,Name,Branch,Type,Project,Disabled"
	othersEmployeeHeader = "Code,Name,Branch,User Email,Reporting manager,Date of joining,Date of birth,Status,Relieving date"

	othersImportReason = "created by import" // csvio's entry (CD-60)
	othersNoUser       = "Pick an existing, enabled user."
)

// othersCSV is a file body: the header, then the rows.
func othersCSV(header string, rows ...string) string {
	return strings.Join(append([]string{header}, rows...), "\n") + "\n"
}

// othersEmployeeRow is an Active employee of branch PUNE joining 01-04-2024.
func othersEmployeeRow(code, name, email, manager string) string {
	return code + "," + name + ",PUNE," + email + "," + manager + ",01-04-2024,,Active,"
}

// othersFresh is a database with no employees, projects, sites or items: what
// isn't importable (the company, its branches, the fixture users) and the
// migrations' UoMs and tax codes. RegisterImporters' Services are process-wide
// and read the latest call, so no test here is parallel.
func othersFresh(t *testing.T) (*msttest.Env, *allmasters.Services) {
	t.Helper()
	env := msttest.New(t)
	svc := allmasters.New(env.Kit)
	allmasters.RegisterImporters(svc)
	ctx := context.Background()
	err := env.DB.Write(ctx, func(tx *db.Tx) error {
		c := env.Fx.Company
		if err := svc.Company.SaveCompanyTx(ctx, tx, env.Admin, company.CompanyInput{LegalName: c.LegalName, PAN: c.PAN}, 0); err != nil {
			return err
		}
		for _, b := range env.Fx.Branches {
			if _, err := svc.Company.CreateTx(ctx, tx, env.Admin, company.BranchInput{
				Code: b.Code, Name: b.Name, StateCode: b.StateCode, GSTIN: b.GSTIN, IsHeadOffice: b.HeadOffice,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	return env, svc
}

// othersSeeded is the full seed, for tests that compare links to known ids.
func othersSeeded(t *testing.T) (*msttest.Env, msttest.Masters, *allmasters.Services) {
	t.Helper()
	env, m, svc := seeded(t)
	allmasters.RegisterImporters(svc)
	return env, m, svc
}

// othersCustomer creates a customer party; the fixture parties are all
// vendor-only, which MST-103 refuses on a project.
func othersCustomer(t *testing.T, env *msttest.Env, svc *allmasters.Services, code string) int64 {
	t.Helper()
	saved, err := svc.Party.Create(context.Background(), env.Kavya, party.Input{
		Code: code, Name: "Customer " + code, IsCustomer: true, GSTCategory: gstin.Unregistered,
	})
	if err != nil {
		t.Fatalf("create customer %s: %v", code, err)
	}
	return saved.ID
}

func othersImporter(env *msttest.Env) *csvio.Importer {
	return csvio.NewImporter(env.DB, env.Kit.Auth, env.Clock, audit.Default)
}

// othersImport runs Import as Kavya. An error (a stub's "not implemented", a
// file-level refusal) fails the test: no report came back.
func othersImport(t *testing.T, env *msttest.Env, docType, body string) csvio.Report {
	t.Helper()
	rep, err := othersImporter(env).Import(context.Background(), env.Kavya, registered(t, docType), strings.NewReader(body))
	if err != nil {
		t.Fatalf("Import(%s): %v", docType, err)
	}
	return rep
}

// othersPreview runs Preview as Kavya; see othersImport.
func othersPreview(t *testing.T, env *msttest.Env, docType, body string) csvio.Report {
	t.Helper()
	rep, err := othersImporter(env).Preview(context.Background(), env.Kavya, registered(t, docType), strings.NewReader(body))
	if err != nil {
		t.Fatalf("Preview(%s): %v", docType, err)
	}
	return rep
}

// othersMessages is the report's errors as the user reads them.
func othersMessages(rep csvio.Report) []string {
	msgs := make([]string, 0, len(rep.Errors))
	for _, e := range rep.Errors {
		msgs = append(msgs, e.String())
	}
	return msgs
}

// othersLoaded fails unless an Import took all n rows, with no warning.
func othersLoaded(t *testing.T, rep csvio.Report, n int) {
	t.Helper()
	if msgs := othersMessages(rep); len(msgs) != 0 || rep.Imported != n || len(rep.Warnings) != 0 {
		t.Fatalf("Imported = %d, errors %q, warnings %q; want %d, none, none", rep.Imported, msgs, rep.Warnings, n)
	}
}

// othersRefused fails unless the report holds exactly want and imported nothing.
func othersRefused(t *testing.T, rep csvio.Report, want ...string) {
	t.Helper()
	if got := othersMessages(rep); !slices.Equal(got, want) || rep.Imported != 0 {
		t.Errorf("Imported = %d, errors %q; want 0, %q", rep.Imported, got, want)
	}
}

// othersGet reads the record with code, which must exist.
func othersGet[T any](t *testing.T, env *msttest.Env, byCode func(context.Context, *db.Tx, string) (T, bool, error), code string) T {
	t.Helper()
	var got T
	var ok bool
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		got, ok, err = byCode(ctx, tx, code)
		return err
	})
	if !ok {
		t.Fatalf("%s was not stored", code)
	}
	return got
}

// othersAbsent fails if a record with code exists.
func othersAbsent[T any](t *testing.T, env *msttest.Env, byCode func(context.Context, *db.Tx, string) (T, bool, error), code string) {
	t.Helper()
	var ok bool
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		var err error
		_, ok, err = byCode(ctx, tx, code)
		return err
	})
	if ok {
		t.Errorf("%s was stored, want nothing imported", code)
	}
}

// othersAuditCount counts every audit entry of docType.
func othersAuditCount(t *testing.T, env *msttest.Env, docType string) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM audit_log WHERE doc_type = ?`, docType).Scan(&n)
	})
	return n
}

// othersUserID is the fixture user's id.
func othersUserID(t *testing.T, env *msttest.Env, email string) auth.UserID {
	t.Helper()
	for _, u := range env.Fx.Users {
		if u.Email == email {
			return u.ID
		}
	}
	t.Fatalf("no fixture user %s", email)
	return 0
}

// othersItemRow is a Goods row in KGS with no tax code.
func othersItemRow(code, hsn, rate string) string {
	return code + ",Item " + code + ",Goods,KGS," + hsn + ",," + rate + ","
}

func TestImport_ItemUsesFormValidation(t *testing.T) {
	env, svc := othersFresh(t)
	body := othersCSV(othersItemHeader, othersItemRow("ANGLE-50", "", ""))
	const want = "Row 2: HSN code is required for goods. Enter a 4, 6 or 8 digit HSN code."
	othersRefused(t, othersPreview(t, env, item.DocType, body), want)
	othersRefused(t, othersImport(t, env, item.DocType, body), want)
	othersAbsent(t, env, svc.Item.ByCode, "ANGLE-50")
}

func TestImport_ItemStandardRate(t *testing.T) { // CD-37
	env, svc := othersFresh(t)
	othersLoaded(t, othersImport(t, env, item.DocType, othersCSV(othersItemHeader, othersItemRow("ANGLE-50", "7216", "62.5"))), 1)
	if got := othersGet(t, env, svc.Item.ByCode, "ANGLE-50").StandardRate; got != types.Rate(625000) {
		t.Errorf("StandardRate = %d, want 625000", got)
	}
}

func TestImport_ItemNegativeStandardRate(t *testing.T) { // CD-220
	env, svc := othersFresh(t)
	body := othersCSV(othersItemHeader, othersItemRow("ANGLE-50", "7216", "-1"))
	const want = "Row 2: Standard Rate can't be negative."
	othersRefused(t, othersPreview(t, env, item.DocType, body), want)
	othersRefused(t, othersImport(t, env, item.DocType, body), want)
	othersAbsent(t, env, svc.Item.ByCode, "ANGLE-50")
}

// The CD-37 template has no stock column, so Type decides.
func TestImport_ItemIsStock(t *testing.T) {
	env, svc := othersFresh(t)
	othersLoaded(t, othersImport(t, env, item.DocType, othersCSV(othersItemHeader,
		"ANGLE-50,MS Angle 50x50x5,Goods,KGS,7216,GST18,,",
		"LABOUR-HR,Labour Charges,Service,HRS,998513,GST18,,",
	)), 2)
	if !othersGet(t, env, svc.Item.ByCode, "ANGLE-50").IsStock {
		t.Error("Goods item: IsStock = false, want true")
	}
	if othersGet(t, env, svc.Item.ByCode, "LABOUR-HR").IsStock {
		t.Error("Service item: IsStock = true, want false")
	}
}

func TestImport_EmployeeManagerFromEarlierRow(t *testing.T) {
	env, svc := othersFresh(t)
	body := othersCSV(othersEmployeeHeader,
		othersEmployeeRow("EMP-101", "Asha Menon", "", ""),
		othersEmployeeRow("EMP-102", "Imran Shaikh", "", "EMP-101"),
	)
	preview := othersPreview(t, env, employee.DocType, body)
	if msgs := othersMessages(preview); len(msgs) != 0 || preview.Ready != 2 || preview.Imported != 0 {
		t.Fatalf("Preview: Ready = %d, Imported = %d, errors %q; want 2, 0, none", preview.Ready, preview.Imported, msgs)
	}
	othersAbsent(t, env, svc.Employee.ByCode, "EMP-101")

	othersLoaded(t, othersImport(t, env, employee.DocType, body), 2)
	manager := othersGet(t, env, svc.Employee.ByCode, "EMP-101")
	if got := othersGet(t, env, svc.Employee.ByCode, "EMP-102").ManagerID; got != manager.ID {
		t.Errorf("ManagerID = %d, want %d", got, manager.ID)
	}
}

// CD-199 (8): a code later in the file isn't in the database yet, so csvio's
// not-found text answers; row 2 makes "nothing imported" mean a rollback, and
// is the code the suggestion names. MST-114 stays on updates.
func TestImport_EmployeeCycleInFile(t *testing.T) {
	env, svc := othersFresh(t)
	body := othersCSV(othersEmployeeHeader,
		othersEmployeeRow("EMP-101", "Asha Menon", "", ""),
		othersEmployeeRow("EMP-201", "Imran Shaikh", "", "EMP-202"),
		othersEmployeeRow("EMP-202", "Lata Deshmukh", "", "EMP-201"),
	)
	want := []string{
		"Row 3: Reporting manager 'EMP-202' not found. Use an existing Reporting manager code such as EMP-101.",
		"Row 4: Reporting manager 'EMP-201' not found. Use an existing Reporting manager code such as EMP-101.",
	}
	preview := othersPreview(t, env, employee.DocType, body)
	othersRefused(t, preview, want...)
	if preview.Ready != 1 {
		t.Errorf("Preview Ready = %d, want 1", preview.Ready)
	}
	rep := othersImport(t, env, employee.DocType, body)
	othersRefused(t, rep, want...)
	if got, want := rep.Summary(), "Nothing was imported. Fix 2 rows and upload again."; got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
	for _, code := range []string{"EMP-101", "EMP-201", "EMP-202"} {
		othersAbsent(t, env, svc.Employee.ByCode, code)
	}
	if n := othersAuditCount(t, env, employee.DocType); n != 0 {
		t.Errorf("audit entries = %d, want 0", n)
	}
}

// CD-199: User Email goes through the real auth.Service. All fixture users are
// unlinked here: othersFresh has no employees, so MST-111 can't answer.
func TestImport_EmployeeUserEmail(t *testing.T) {
	env, svc := othersFresh(t)
	if _, ok := env.Kit.Auth.(*auth.Auth); !ok {
		t.Fatalf("Kit.Auth is %T, want the real *auth.Auth", env.Kit.Auth)
	}
	for _, c := range []struct {
		name, code, email string
		wantUser          string // fixture email; "" = no linked user
		refused           bool
	}{
		{name: "blank", code: "EMP-101"},
		{name: "unknown", code: "EMP-102", email: "nobody@vardhan.test", refused: true},
		{name: "disabled", code: "EMP-103", email: "old@vardhan.test", refused: true},
		{name: "enabled", code: "EMP-104", email: "deepak@vardhan.test", wantUser: "deepak@vardhan.test"},
		{name: "upper case", code: "EMP-105", email: "PRIYA@Vardhan.TEST", wantUser: "priya@vardhan.test"},
	} {
		t.Run(c.name, func(t *testing.T) {
			body := othersCSV(othersEmployeeHeader, othersEmployeeRow(c.code, "Employee "+c.code, c.email, ""))
			if c.refused {
				othersRefused(t, othersPreview(t, env, employee.DocType, body), "Row 2: "+othersNoUser)
				othersRefused(t, othersImport(t, env, employee.DocType, body), "Row 2: "+othersNoUser)
				othersAbsent(t, env, svc.Employee.ByCode, c.code)
				return
			}
			othersLoaded(t, othersImport(t, env, employee.DocType, body), 1)
			var want auth.UserID
			if c.wantUser != "" {
				want = othersUserID(t, env, c.wantUser)
			}
			if got := othersGet(t, env, svc.Employee.ByCode, c.code).UserID; got != want {
				t.Errorf("UserID = %d, want %d", got, want)
			}
		})
	}
}

// The ruling: Select cells map case-insensitively in the RowFunc. Site Type's
// Options are lower case, the others title case.
func TestImport_SelectCellsIgnoreCase(t *testing.T) {
	env, svc := othersFresh(t)
	for _, c := range []struct {
		name, docType, body string
		got                 func(t *testing.T, code string) string
		want                map[string]string
	}{
		{
			name: "Site Type", docType: site.DocType,
			body: othersCSV(othersSiteHeader,
				"S-1,One,PUNE,warehouse,,", "S-2,Two,PUNE,WAREHOUSE,,", "S-3,Three,PUNE,Warehouse,,",
				"S-4,Four,PUNE,site,,", "S-5,Five,PUNE,SITE,,", "S-6,Six,PUNE,Site,,"),
			got: func(t *testing.T, code string) string {
				return string(othersGet(t, env, svc.Site.ByCode, code).Type)
			},
			want: map[string]string{"S-1": "warehouse", "S-2": "warehouse", "S-3": "warehouse", "S-4": "site", "S-5": "site", "S-6": "site"},
		},
		{
			name: "Project Status", docType: project.DocType,
			body: othersCSV(othersProjectHeader,
				"P-1,One,PUNE,,,,open,", "P-2,Two,PUNE,,,,on hold,", "P-3,Three,PUNE,,,,COMPLETED,", "P-4,Four,PUNE,,,,cAnCeLlEd,"),
			got: func(t *testing.T, code string) string {
				return string(othersGet(t, env, svc.Project.ByCode, code).Status)
			},
			want: map[string]string{"P-1": "Open", "P-2": "On Hold", "P-3": "Completed", "P-4": "Cancelled"},
		},
		{
			name: "Employee Status", docType: employee.DocType,
			body: othersCSV(othersEmployeeHeader,
				"E-1,One,PUNE,,,01-04-2024,,ACTIVE,", "E-2,Two,PUNE,,,01-04-2024,,left,31-03-2026"),
			got: func(t *testing.T, code string) string {
				return string(othersGet(t, env, svc.Employee.ByCode, code).Status)
			},
			want: map[string]string{"E-1": "Active", "E-2": "Left"},
		},
		{
			name: "Tax Code Treatment", docType: taxcode.DocType,
			body: othersCSV(othersTaxHeader,
				"T-1,One,TAXABLE,18,", "T-2,Two,nil-rated,,", "T-3,Three,EXEMPTED,,", "T-4,Four,non-gst,,", "T-5,Five,zero-RATED,,"),
			got: func(t *testing.T, code string) string {
				return string(othersGet(t, env, svc.TaxCode.ByCode, code).Treatment)
			},
			want: map[string]string{"T-1": "Taxable", "T-2": "Nil-Rated", "T-3": "Exempted", "T-4": "Non-GST", "T-5": "Zero-Rated"},
		},
		{
			name: "Item Type", docType: item.DocType,
			body: othersCSV(othersItemHeader, "I-1,One,goods,KGS,7216,,,", "I-2,Two,SERVICE,HRS,,,,"),
			got: func(t *testing.T, code string) string {
				return string(othersGet(t, env, svc.Item.ByCode, code).Type)
			},
			want: map[string]string{"I-1": "Goods", "I-2": "Service"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			othersLoaded(t, othersImport(t, env, c.docType, c.body), len(c.want))
			for code, want := range c.want {
				if got := c.got(t, code); got != want {
					t.Errorf("%s = %q, want %q", code, got, want)
				}
			}
		})
	}

	for _, c := range []struct{ name, docType, body, want string }{
		{"Site Type", site.DocType, othersCSV(othersSiteHeader, "X-1,One,PUNE,Depot,,"), "Row 2: Type must be Warehouse or Site."},
		{"Project Status", project.DocType, othersCSV(othersProjectHeader, "X-1,One,PUNE,,,,Paused,"), "Row 2: Status must be Open, On Hold, Completed or Cancelled."},
		{"Employee Status", employee.DocType, othersCSV(othersEmployeeHeader, "X-1,One,PUNE,,,01-04-2024,,Retired,"), "Row 2: Status must be Active or Left."},
		{"Tax Code Treatment", taxcode.DocType, othersCSV(othersTaxHeader, "X-1,One,Taxed,18,"), "Row 2: Treatment must be Taxable, Nil-Rated, Exempted, Non-GST or Zero-Rated."},
		{"Item Type", item.DocType, othersCSV(othersItemHeader, "X-1,One,Product,KGS,7216,,,"), "Row 2: Item type must be Goods or Service."},
	} {
		t.Run("unknown "+c.name, func(t *testing.T) {
			othersRefused(t, othersImport(t, env, c.docType, c.body), c.want)
		})
	}
}

// Employee has no Disabled column: its disabled mirrors Status.
func TestImport_DisabledCell(t *testing.T) {
	env, svc := othersFresh(t)
	cells := []struct {
		cell string
		want bool
	}{{"Yes", true}, {"true", true}, {"1", true}, {"No", false}, {"false", false}, {"0", false}, {"", false}}
	for _, c := range []struct {
		docType, header string
		row             func(code, cell string) string
		disabled        func(t *testing.T, code string) bool
	}{
		{
			uom.DocType, othersUoMHeader,
			func(code, cell string) string { return code + ",Unit " + code + ",No,NOS," + cell },
			func(t *testing.T, code string) bool { return othersGet(t, env, svc.UoM.ByCode, code).Disabled },
		},
		{
			taxcode.DocType, othersTaxHeader,
			func(code, cell string) string { return code + ",Tax " + code + ",Nil-Rated,," + cell },
			func(t *testing.T, code string) bool { return othersGet(t, env, svc.TaxCode.ByCode, code).Disabled },
		},
		{
			item.DocType, othersItemHeader,
			func(code, cell string) string { return code + ",Item " + code + ",Goods,KGS,7216,,," + cell },
			func(t *testing.T, code string) bool { return othersGet(t, env, svc.Item.ByCode, code).Disabled },
		},
		{
			expensetype.DocType, othersExpenseHeader,
			func(code, cell string) string { return code + ",Expense " + code + ",,," + cell },
			func(t *testing.T, code string) bool {
				return othersGet(t, env, svc.ExpenseType.ByCode, code).Disabled
			},
		},
		{
			project.DocType, othersProjectHeader,
			func(code, cell string) string { return code + ",Project " + code + ",PUNE,,,,," + cell },
			func(t *testing.T, code string) bool { return othersGet(t, env, svc.Project.ByCode, code).Disabled },
		},
		{
			site.DocType, othersSiteHeader,
			func(code, cell string) string { return code + ",Site " + code + ",PUNE,warehouse,," + cell },
			func(t *testing.T, code string) bool { return othersGet(t, env, svc.Site.ByCode, code).Disabled },
		},
	} {
		t.Run(c.docType, func(t *testing.T) {
			var rows []string
			for i, cell := range cells {
				rows = append(rows, c.row(fmt.Sprintf("D-%d", i), cell.cell))
			}
			othersLoaded(t, othersImport(t, env, c.docType, othersCSV(c.header, rows...)), len(cells))
			for i, cell := range cells {
				if got := c.disabled(t, fmt.Sprintf("D-%d", i)); got != cell.want {
					t.Errorf("Disabled cell %q: Disabled = %v, want %v", cell.cell, got, cell.want)
				}
			}
		})
	}

	// csvio writes its create entry after every row, so the order isn't
	// the point: one of each, and nothing else.
	disabled := auditEntries(t, env, uom.DocType, othersGet(t, env, svc.UoM.ByCode, "D-0").ID)
	if len(disabled) != 2 {
		t.Fatalf("disabled row: entries = %+v, want a create and a disable", disabled)
	}
	create := slices.IndexFunc(disabled, func(e audit.Entry) bool { return e.Action == "create" })
	off := slices.IndexFunc(disabled, func(e audit.Entry) bool { return e.Action == "disable" })
	if create < 0 || off < 0 {
		t.Fatalf("disabled row: entries = %+v, want a create and a disable", disabled)
	}
	if e := disabled[create]; e.Reason != othersImportReason || len(e.Changes) != 0 || e.By != env.Kavya.ID {
		t.Errorf("create entry = %+v, want reason %q, no changes, by Kavya", e, othersImportReason)
	}
	e := disabled[off]
	if e.By != env.Kavya.ID || len(e.Changes) != 1 || e.Changes[0].Field != "disabled" || e.Changes[0].Before != "false" || e.Changes[0].After != "true" {
		t.Errorf("disable entry = %+v, want disabled: false → true, by Kavya", e)
	}
	enabled := auditEntries(t, env, uom.DocType, othersGet(t, env, svc.UoM.ByCode, "D-6").ID)
	if len(enabled) != 1 || enabled[0].Action != "create" {
		t.Errorf("enabled row: entries = %+v, want one create", enabled)
	}
}

// M12, CD-60: csvio's entry is the only create entry. The master's own would
// carry field changes and no reason.
func TestImport_OneCreateAudit(t *testing.T) {
	env, svc := othersFresh(t)
	for _, c := range []struct {
		docType string
		body    string
		codes   []string
		id      func(t *testing.T, code string) int64
	}{
		{
			uom.DocType, othersCSV(othersUoMHeader, "A-1,One,No,NOS,", "A-2,Two,Yes,NOS,", "A-3,Three,No,OTH,"),
			[]string{"A-1", "A-2", "A-3"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.UoM.ByCode, code).ID },
		},
		{
			taxcode.DocType, othersCSV(othersTaxHeader, "A-1,One,Taxable,18,", "A-2,Two,Exempted,,"),
			[]string{"A-1", "A-2"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.TaxCode.ByCode, code).ID },
		},
		{
			item.DocType, othersCSV(othersItemHeader, othersItemRow("A-1", "7216", ""), othersItemRow("A-2", "7214", "10"), othersItemRow("A-3", "2523", "")),
			[]string{"A-1", "A-2", "A-3"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.Item.ByCode, code).ID },
		},
		{
			expensetype.DocType, othersCSV(othersExpenseHeader, "A-1,One,,,", "A-2,Two,996411,GST5,"),
			[]string{"A-1", "A-2"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.ExpenseType.ByCode, code).ID },
		},
		{
			project.DocType, othersCSV(othersProjectHeader, "A-1,One,PUNE,,,,,", "A-2,Two,BLR,,,,On Hold,"),
			[]string{"A-1", "A-2"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.Project.ByCode, code).ID },
		},
		{
			site.DocType, othersCSV(othersSiteHeader, "A-1,One,PUNE,warehouse,,", "A-2,Two,BLR,site,,"),
			[]string{"A-1", "A-2"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.Site.ByCode, code).ID },
		},
		{
			employee.DocType, othersCSV(othersEmployeeHeader,
				othersEmployeeRow("A-1", "One", "deepak@vardhan.test", ""), othersEmployeeRow("A-2", "Two", "", "A-1")),
			[]string{"A-1", "A-2"},
			func(t *testing.T, code string) int64 { return othersGet(t, env, svc.Employee.ByCode, code).ID },
		},
	} {
		t.Run(c.docType, func(t *testing.T) {
			othersPreview(t, env, c.docType, c.body)
			if n := othersAuditCount(t, env, c.docType); n != 0 {
				t.Fatalf("audit entries after Preview = %d, want 0", n)
			}
			othersLoaded(t, othersImport(t, env, c.docType, c.body), len(c.codes))
			if n := othersAuditCount(t, env, c.docType); n != len(c.codes) {
				t.Errorf("audit entries = %d, want %d", n, len(c.codes))
			}
			for _, code := range c.codes {
				es := auditEntries(t, env, c.docType, c.id(t, code))
				if len(es) != 1 {
					t.Fatalf("%s: entries = %+v, want 1", code, es)
				}
				e := es[0]
				if e.Action != "create" || e.Reason != othersImportReason || len(e.Changes) != 0 || e.By != env.Kavya.ID {
					t.Errorf("%s: entry = %+v, want create, reason %q, no changes, by Kavya", code, e, othersImportReason)
				}
			}
		})
	}
}

func TestImport_LinksResolved(t *testing.T) {
	env, m, svc := othersSeeded(t)
	customer := othersCustomer(t, env, svc, "CUST-LINK")

	// Each file gives one row, and each link a value that isn't the first
	// row of its table where the fixtures allow it, so a wrong column shows.
	othersLoaded(t, othersImport(t, env, item.DocType, othersCSV(othersItemHeader, "L-1,Link Item,Goods,BOX,7216,GST12,,")), 1)
	othersLoaded(t, othersImport(t, env, expensetype.DocType, othersCSV(othersExpenseHeader, "L-1,Link Expense,,GST5,")), 1)
	othersLoaded(t, othersImport(t, env, project.DocType, othersCSV(othersProjectHeader, "L-1,Link Project,BLR,CUST-LINK,,,,")), 1)
	othersLoaded(t, othersImport(t, env, site.DocType, othersCSV(othersSiteHeader, "L-1,Link Site,BLR,site,PRJ-TECHPARK,")), 1)
	othersLoaded(t, othersImport(t, env, employee.DocType, othersCSV(othersEmployeeHeader, "L-1,Link Employee,BLR,,,01-04-2024,,Active,")), 1)

	it := othersGet(t, env, svc.Item.ByCode, "L-1")
	et := othersGet(t, env, svc.ExpenseType.ByCode, "L-1")
	pr := othersGet(t, env, svc.Project.ByCode, "L-1")
	st := othersGet(t, env, svc.Site.ByCode, "L-1")
	emp := othersGet(t, env, svc.Employee.ByCode, "L-1")
	for _, c := range []struct {
		name      string
		got, want int64
	}{
		{"Item UoM", it.StockUoMID, m.UoMs["BOX"]},
		{"Item Tax Code", it.DefaultTaxCodeID, m.TaxCodes["GST12"]},
		{"Expense Type Default Tax Code", et.DefaultTaxCodeID, m.TaxCodes["GST5"]},
		{"Project Branch", pr.BranchID, m.Branches["BLR"]},
		{"Project Customer", pr.CustomerID, customer},
		{"Site Branch", st.BranchID, m.Branches["BLR"]},
		{"Site Project", st.ProjectID, m.Projects["PRJ-TECHPARK"]},
		{"Employee Branch", emp.BranchID, m.Branches["BLR"]},
	} {
		if c.want == 0 {
			t.Errorf("%s: the fixture has no such id", c.name)
		}
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

// sampleOrder is testdata/masters in dependency order.
var sampleOrder = []string{
	"parties.csv", "addresses.csv", "contacts.csv", "uoms.csv", "tax_codes.csv", "items.csv", "expense_types.csv",
	"projects.csv", "sites.csv", "employees.csv",
}

func TestImport_SampleFilesLoadAll(t *testing.T) {
	env, svc := othersFresh(t)

	want := map[string]struct {
		docType string
		rows    int
	}{
		"parties.csv":       {party.DocType, 4},
		"addresses.csv":     {address.DocType, 5},
		"contacts.csv":      {address.ContactDocType, 4},
		"uoms.csv":          {uom.DocType, 5},
		"tax_codes.csv":     {taxcode.DocType, 6},
		"items.csv":         {item.DocType, 7},
		"expense_types.csv": {expensetype.DocType, 6},
		"projects.csv":      {project.DocType, 3},
		"sites.csv":         {site.DocType, 4},
		"employees.csv":     {employee.DocType, 6},
	}
	samples := os.DirFS("../../../testdata/masters")
	for _, file := range sampleOrder {
		w, ok := want[file]
		if !ok {
			t.Fatalf("%s has no expected row count", file)
		}
		body, err := fs.ReadFile(samples, file)
		if err != nil {
			t.Fatal(err)
		}
		preview := othersPreview(t, env, w.docType, string(body))
		if msgs := othersMessages(preview); len(msgs) != 0 || preview.Ready != w.rows {
			t.Fatalf("%s Preview: Ready = %d, errors %q; want %d, none", file, preview.Ready, msgs, w.rows)
		}
		rep := othersImport(t, env, w.docType, string(body))
		if msgs := othersMessages(rep); len(msgs) != 0 {
			t.Fatalf("%s: errors %q", file, msgs)
		}
		othersLoaded(t, rep, w.rows)
	}

	// One row of each file, field by field: every column reaches its Input.
	if got := othersGet(t, env, svc.UoM.ByCode, "ROLL"); got.Name != "Rolls" || got.UQC != "ROL" || !got.WholeNumber || got.Disabled {
		t.Errorf("ROLL = %+v, want the file's values", got)
	}
	if !othersGet(t, env, svc.UoM.ByCode, "GRS").Disabled {
		t.Error("GRS: Disabled = false, want true")
	}
	if got := othersGet(t, env, svc.TaxCode.ByCode, "GST7.5"); got.Name != "GST 7.5%" || got.Treatment != taxcode.Taxable || got.Rate != types.Percent(7500) {
		t.Errorf("GST7.5 = %+v, want GST 7.5%%, Taxable, 7500", got)
	}
	kgs, gst18 := othersGet(t, env, svc.UoM.ByCode, "KGS").ID, othersGet(t, env, svc.TaxCode.ByCode, "GST18").ID
	if got := othersGet(t, env, svc.Item.ByCode, "MS-ANGLE-50"); got.Name != "MS Angle 50x50x5" || got.Type != item.TypeGoods || got.HSNSAC != "7216" ||
		got.StockUoMID != kgs || got.DefaultTaxCodeID != gst18 || got.StandardRate != types.Rate(625000) {
		t.Errorf("MS-ANGLE-50 = %+v, want the file's values", got)
	}
	if got := othersGet(t, env, svc.ExpenseType.ByCode, "TRAVEL"); got.Name != "Travel" || got.DefaultHSNSAC != "996411" {
		t.Errorf("TRAVEL = %+v, want the file's values", got)
	}
	if got := othersGet(t, env, svc.Project.ByCode, "PRJ-METRO"); got.Name != "Pune Metro Depot" || got.Status != project.Open ||
		got.Start != (types.Date{Year: 2026, Month: time.April, Day: 1}) || got.End != (types.Date{Year: 2028, Month: time.March, Day: 31}) {
		t.Errorf("PRJ-METRO = %+v, want the file's values", got)
	}
	if got := othersGet(t, env, svc.Project.ByCode, "PRJ-TECHPARK"); got.Status != project.OnHold || got.End != (types.Date{}) {
		t.Errorf("PRJ-TECHPARK = %+v, want On Hold, no end date", got)
	}
	if got := othersGet(t, env, svc.Site.ByCode, "METRO-SITE"); got.Name != "Metro Depot Site" || got.Type != site.SiteKind {
		t.Errorf("METRO-SITE = %+v, want the file's values", got)
	}
	manager := othersGet(t, env, svc.Employee.ByCode, "EMP-102")
	if got := othersGet(t, env, svc.Employee.ByCode, "EMP-103"); got.Name != "Ravi Kulkarni" || got.UserID != env.Ravi.ID || got.ManagerID != manager.ID ||
		got.Joining != (types.Date{Year: 2021, Month: time.July, Day: 15}) || got.Birth != (types.Date{Year: 1984, Month: time.November, Day: 2}) || got.Status != employee.Active {
		t.Errorf("EMP-103 = %+v, want the file's values", got)
	}
	if got := othersGet(t, env, svc.Employee.ByCode, "EMP-106"); got.Status != employee.Left || !got.Disabled ||
		got.Relieving != (types.Date{Year: 2026, Month: time.March, Day: 31}) {
		t.Errorf("EMP-106 = %+v, want Left, disabled, relieved 31-03-2026", got)
	}
}
