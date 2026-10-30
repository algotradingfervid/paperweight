package allmasters_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// templateBOM is the UTF-8 byte order mark every import template starts with.
const templateBOM = "\xef\xbb\xbf"

func registered(t *testing.T, name string) *doctype.DocType {
	t.Helper()
	dt, ok := doctype.Lookup(name)
	if !ok {
		t.Fatalf("doctype.Lookup(%s): not registered", name)
	}
	return dt
}

func TestImport_Columns(t *testing.T) {
	cases := []struct {
		docType string
		want    []string
	}{
		{party.DocType, []string{"Code", "Name", "Vendor", "Customer", "PAN", "GST Category", "Payment Terms", "Disabled"}},
		{address.DocType, []string{"Party", "Title", "Line 1", "Line 2", "City", "State", "Country", "Pincode", "GSTIN", "Billing", "Shipping", "Disabled"}},
		{address.ContactDocType, []string{"Party", "First Name", "Middle Name", "Last Name", "Email", "Mobile", "Primary"}},
		{uom.DocType, []string{"Code", "Name", "Whole Number", "UQC", "Disabled"}},
		{taxcode.DocType, []string{"Code", "Name", "Treatment", "Rate", "Disabled"}},
		{item.DocType, []string{"Code", "Name", "Type", "UoM", "HSN/SAC", "Tax Code", "Standard Rate", "Disabled"}},
		{expensetype.DocType, []string{"Code", "Name", "Default HSN/SAC", "Default Tax Code", "Disabled"}},
		{project.DocType, []string{"Code", "Name", "Branch", "Customer", "Start Date", "End Date", "Status", "Disabled"}},
		{site.DocType, []string{"Code", "Name", "Branch", "Type", "Project", "Disabled"}},
		{employee.DocType, []string{"Code", "Name", "Branch", "User Email", "Reporting Manager", "Date of Joining", "Date of Birth", "Status", "Relieving Date"}},
	}
	for _, c := range cases {
		t.Run(c.docType, func(t *testing.T) {
			body, ok := bytes.CutPrefix(csvio.Template(registered(t, c.docType)), []byte(templateBOM))
			if !ok {
				t.Fatal("template does not start with the BOM")
			}
			got, err := csv.NewReader(bytes.NewReader(body)).Read()
			if err != nil {
				t.Fatalf("template header is not CSV: %v", err)
			}
			if !slices.EqualFunc(got, c.want, strings.EqualFold) {
				t.Errorf("template labels = %q, want %q (case-insensitive, same order)", got, c.want)
			}
		})
	}
}

func TestImport_ItemTemplateHeader(t *testing.T) { // CD-37
	want := templateBOM + "Code,Name,Type,UoM,HSN/SAC,Tax Code,Standard Rate,Disabled\r\n"
	if got := string(csvio.Template(registered(t, item.DocType))); got != want {
		t.Errorf("Template(Item) = %q, want %q", got, want)
	}
}

func TestImport_ImportOnlyFields(t *testing.T) {
	for _, c := range []struct{ docType, field string }{
		{address.ContactDocType, "email"},
		{address.ContactDocType, "mobile"},
		{employee.DocType, "user_email"},
	} {
		fields := registered(t, c.docType).ImportFields()
		i := slices.IndexFunc(fields, func(f doctype.Field) bool { return f.Name == c.field })
		if i < 0 {
			t.Errorf("%s: ImportFields has no %s", c.docType, c.field)
			continue
		}
		if !fields[i].ImportOnly {
			t.Errorf("%s.%s: ImportOnly = false, want true", c.docType, c.field)
		}
	}

	f, ok := registered(t, employee.DocType).Field("user_id")
	if !ok {
		t.Fatal("Employee has no user_id field")
	}
	if f.Importable {
		t.Error("Employee user_id is Importable, want not: the file carries User Email")
	}
}

// CD-210: an ImportOnly field never reaches a stored column or a screen.
func TestImportOnly_NeverStoredOrShown(t *testing.T) {
	seen := map[string]bool{}
	for _, dt := range doctype.All() {
		seen[dt.Name] = true
		for _, f := range dt.Fields {
			if f.ImportOnly {
				t.Errorf("%s: dt.Fields holds ImportOnly field %s", dt.Name, f.Name)
			}
		}
	}
	if !seen[address.ContactDocType] || !seen[employee.DocType] {
		t.Fatalf("doctype.All() lacks Contact or Employee: the walk covers nothing")
	}

	env := msttest.New(t)
	ctx := context.Background()
	for _, c := range []struct {
		docType, table, stored string
		importOnly             []string
	}{
		{address.ContactDocType, "contact", "first_name", []string{"email", "mobile"}},
		{employee.DocType, "employee", "user_id", []string{"user_email"}},
	} {
		dt := registered(t, c.docType)
		// The field exists for import, so its absence below means something.
		for _, col := range c.importOnly {
			if !slices.ContainsFunc(dt.ImportFields(), func(f doctype.Field) bool { return f.Name == col && f.ImportOnly }) {
				t.Errorf("%s: ImportFields has no ImportOnly %s", c.docType, col)
			}
		}
		sql, args, err := list.Build(dt, list.Query{Page: 1}, auth.Predicate{})
		if err != nil {
			t.Fatalf("list.Build(%s): %v", c.docType, err)
		}
		// The form these checks look for: a stored column is in the SQL so.
		if !strings.Contains(sql, c.table+"."+c.stored+" AS "+c.stored) {
			t.Fatalf("%s: SQL has no %s.%s AS %s: %s", c.docType, c.table, c.stored, c.stored, sql)
		}
		for _, col := range c.importOnly {
			if strings.Contains(sql, c.table+"."+col) || strings.Contains(sql, " AS "+col) {
				t.Errorf("%s: SQL selects %s: %s", c.docType, col, sql)
			}
		}
		// It runs on the real table.
		err = env.DB.Read(ctx, func(tx *db.Tx) error {
			rows, err := tx.SQL().QueryContext(ctx, sql, args...)
			if err != nil {
				return err
			}
			return rows.Close()
		})
		if err != nil {
			t.Errorf("%s: list query on the real table: %v", c.docType, err)
		}
	}
}
