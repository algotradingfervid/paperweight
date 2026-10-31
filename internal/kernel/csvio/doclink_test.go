package csvio

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/list/listtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-297: a Link to a DocType that isn't a master exports the target's
// naming.Label, only when the viewer can read the target (CD-233).

const (
	nameDLOrder   = "csviotest.DocLinkOrder"
	nameDLReceipt = "csviotest.DocLinkReceipt"
	dlOrderTable  = "numt_csvio_dlorder"
	dlRecptTable  = "numt_csvio_dlreceipt"
)

func init() {
	doctype.Register(doctype.DocType{
		Name: nameDLOrder, Label: "Order", PluralLabel: "Orders", Table: dlOrderTable, TestOnly: true,
		Fields: []doctype.Field{{Name: "title", Label: "Title", Type: doctype.Text}},
	})
	doctype.Register(doctype.DocType{
		Name: nameDLReceipt, Label: "Receipt", PluralLabel: "Receipts", Table: dlRecptTable, TestOnly: true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Type: doctype.Text},
			{Name: "order_id", Label: "Order", Type: doctype.Link, Link: nameDLOrder},
		},
		DefaultSort: []doctype.SortKey{{Field: "code"}},
	})
}

// dlSetup creates both tables on a listtest database.
func dlSetup(t *testing.T) *db.DB {
	t.Helper()
	d := listtest.Setup(t)
	ctx := t.Context()
	if err := d.Write(ctx, func(tx *db.Tx) error {
		for _, q := range []string{
			"CREATE TABLE " + dlOrderTable + " (id INTEGER PRIMARY KEY, number TEXT, state TEXT, title TEXT, created_by INTEGER, employee_id INTEGER)",
			"CREATE TABLE " + dlRecptTable + " (id INTEGER PRIMARY KEY, code TEXT, order_id INTEGER, created_at TEXT, created_by INTEGER, updated_at TEXT, updated_by INTEGER)",
		} {
			if _, err := tx.SQL().ExecContext(ctx, q); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("create tables: %v", err)
	}
	return d
}

// dlOrder inserts an order; a blank number is a draft.
func dlOrder(t *testing.T, d *db.DB, number string, createdBy auth.UserID) int64 {
	t.Helper()
	var num any
	if number != "" {
		num = number
	}
	return listtest.Insert(t, d, dlOrderTable, map[string]any{"number": num, "state": "approved", "created_by": int64(createdBy)})
}

func dlReceipt(t *testing.T, d *db.DB, code string, order int64) {
	t.Helper()
	listtest.Insert(t, d, dlRecptTable, map[string]any{"code": code, "order_id": order})
}

func dlExport(t *testing.T, ex *Exporter, p auth.Principal) string {
	t.Helper()
	dt, ok := doctype.Lookup(nameDLReceipt)
	if !ok {
		t.Fatalf("%s is not registered", nameDLReceipt)
	}
	var buf bytes.Buffer
	if _, err := ex.Export(t.Context(), &buf, p, dt, list.Query{}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.String()
}

func TestExport_DocLinkReadableNumber(t *testing.T) {
	d := dlSetup(t)
	dlReceipt(t, d, "R1", dlOrder(t, d, "PO/26-27/00007", 0))
	got := dlExport(t, NewExporter(d, listtest.NewAuth()), testkit.Principal(t, "admin@vardhan.test"))
	if want := string(utf8BOM) + "Code,Order\r\nR1,PO/26-27/00007\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}

func TestExport_DocLinkDraftLabel(t *testing.T) {
	d := dlSetup(t)
	dlOrder(t, d, "PO/26-27/00001", 0)
	draft := dlOrder(t, d, "", 0)
	dlReceipt(t, d, "R1", draft)
	got := dlExport(t, NewExporter(d, listtest.NewAuth()), testkit.Principal(t, "admin@vardhan.test"))
	if want := string(utf8BOM) + "Code,Order\r\nR1,Draft 2\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}

func TestExport_DocLinkHiddenBlank(t *testing.T) {
	d := dlSetup(t)
	shown := dlOrder(t, d, "PO/26-27/00007", 0)
	hidden := dlOrder(t, d, "PO/26-27/00009", 0)
	dlReceipt(t, d, "R1", shown)
	dlReceipt(t, d, "R2", hidden)
	a := listtest.NewAuth()
	a.Service.(*authfake.Fake).Hide(nameDLOrder, hidden)
	got := dlExport(t, NewExporter(d, a), testkit.Principal(t, "admin@vardhan.test"))
	if want := string(utf8BOM) + "Code,Order\r\nR1,PO/26-27/00007\r\nR2,\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
	if strings.Contains(got, "00009") {
		t.Errorf("export names the hidden order: %q", got)
	}
}

// dlRealAuth returns the real auth.Service over d and a non-Administrator who
// may read and export receipts and read only their own orders.
func dlRealAuth(t *testing.T, d *db.DB) (auth.Service, auth.Principal) {
	t.Helper()
	ctx := t.Context()
	a := auth.New(auth.Deps{DB: d, Clock: testkit.Clock(), Directory: authfake.NewDirectory(), Audit: auditfake.New(), Info: authtest.Info{}})
	id, err := a.CreateFirstAdmin(ctx, "dl-admin@vardhan.test", "Doc Link Admin", "Correct-Horse-42")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin := auth.Principal{ID: id, Roles: []string{auth.RoleAdministrator}}
	const role = "Doc Link Exporter"
	if err := a.CreateRole(ctx, admin, role); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := a.SetRights(ctx, admin, role, nameDLReceipt, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightExport: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights receipt: %v", err)
	}
	if err := a.SetRights(ctx, admin, role, nameDLOrder, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn}); err != nil {
		t.Fatalf("SetRights order: %v", err)
	}
	p := testkit.Principal(t, "kavya@vardhan.test")
	p.Roles = []string{role}
	return a, p
}

// dlExportOneConn runs the export with the pool cut to one connection, so the
// read check must use the export's read tx (CD-213).
func dlExportOneConn(t *testing.T, d *db.DB, a auth.Service, p auth.Principal) string {
	t.Helper()
	dt, _ := doctype.Lookup(nameDLReceipt)
	var buf bytes.Buffer
	if err := testkit.NoSecondConn(t, d, func(ctx context.Context) error {
		_, err := NewExporter(d, a).Export(ctx, &buf, p, dt, list.Query{})
		return err
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return buf.String()
}

// On the real auth.Service, a non-Administrator reading orders with own scope
// sees only their own order's number; the check runs in the export's read tx
// (pool of one, CD-213).
func TestExport_DocLinkRealService(t *testing.T) {
	d := dlSetup(t)
	a, p := dlRealAuth(t, d)
	dlReceipt(t, d, "R1", dlOrder(t, d, "PO/26-27/00007", p.ID))
	dlReceipt(t, d, "R2", dlOrder(t, d, "PO/26-27/00009", p.ID+1000))
	dlReceipt(t, d, "R3", dlOrder(t, d, "", p.ID))

	got := dlExportOneConn(t, d, a, p)
	if want := string(utf8BOM) + "Code,Order\r\nR1,PO/26-27/00007\r\nR2,\r\nR3,Draft 3\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
	if strings.Contains(got, "00009") {
		t.Errorf("export names the hidden order: %q", got)
	}
}

// A link to an order that no longer exists exports a blank cell, and the
// export still succeeds. This runs on the real auth.Service because only it
// answers ErrNotFound for a missing id; authfake's CheckLink allows any id it
// wasn't told to hide.
func TestExport_DocLinkGoneTargetBlank(t *testing.T) {
	d := dlSetup(t)
	a, p := dlRealAuth(t, d)
	dlReceipt(t, d, "R1", dlOrder(t, d, "PO/26-27/00007", p.ID))
	dlReceipt(t, d, "R2", 999)

	got := dlExportOneConn(t, d, a, p)
	if want := string(utf8BOM) + "Code,Order\r\nR1,PO/26-27/00007\r\nR2,\r\n"; got != want {
		t.Errorf("export = %q, want %q", got, want)
	}
}
