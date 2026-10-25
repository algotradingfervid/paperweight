package web_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

const (
	poType = "test.PurchaseOrder"

	// Project ids for PRJ-METRO and PRJ-TECHPARK; authfake compares ids only.
	prjMetro    int64 = 1
	prjTechpark int64 = 2

	notFoundMsg  = "This record doesn't exist or you don't have access to it."
	forbiddenMsg = "You don't have permission to print Purchase Order. Ask your administrator for a role that allows it."

	fixedUpdatedAt = "2026-10-24T05:00:00.000Z"
)

// seedRights seeds the 00-fixtures.md rights matrix rows for Purchase Order
// onto poType (CD-56).
func seedRights(f *authfake.Fake) {
	for _, r := range []auth.Right{"read", "approve", "cancel", "amend", "print", "export"} {
		f.Grant("Purchase Manager", poType, r, auth.ScopeAll)
	}
	for _, r := range []auth.Right{"read", "create", "write", "submit", "print", "export"} {
		f.Grant("Purchase User", poType, r, auth.ScopeAll)
	}
	f.Grant("Purchase User", poType, "delete", auth.ScopeOwn)
	f.Grant("Stores", poType, "read", auth.ScopeAll)
	for _, r := range []auth.Right{"read", "approve", "print", "export"} {
		f.Grant("Director", poType, r, auth.ScopeAll)
	}
	f.Grant("Administrator", "*", "print", auth.ScopeAll)
	f.Grant("Administrator", "*", "read", auth.ScopeAll)
}

// ravi returns Ravi (Purchase Manager) with his role added to f.
func ravi(t *testing.T, f *authfake.Fake) auth.Principal {
	t.Helper()
	p := testkit.Principal(t, "ravi@vardhan.test")
	f.AddRole(p.ID, "Purchase Manager")
	return p
}

func titleView(context.Context, *db.Tx, int64) (templ.Component, error) {
	return print.DocTitle("Purchase Order"), nil
}

func get(t *testing.T, h http.Handler, p auth.Principal, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), p), http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func approvedPO1() doctype.Info {
	return doctype.Info{
		DocType: poType, DocLabel: "Purchase Order", ID: 1, State: doctype.StateApproved,
		Number: "PO/26-27/00001", Display: "PO/26-27/00001", ProjectIDs: []int64{prjTechpark},
	}
}

// FILE-050 (R3): print needs the print right plus visibility of the record.
func TestPrint_RequiresPrintRight(t *testing.T) {
	fa := authfake.New()
	seedRights(fa)
	fa.Grant("Masters Manager", poType, "read", auth.ScopeAll)

	priya := testkit.Principal(t, "priya@vardhan.test")
	fa.AddRole(priya.ID, "Purchase User")
	fa.Restrict(priya.ID, []int64{prjMetro}, nil)
	kavya := testkit.Principal(t, "kavya@vardhan.test")
	fa.AddRole(kavya.ID, "Masters Manager")
	rv := ravi(t, fa)

	df := docfake.New()
	df.SetInfo(approvedPO1())
	reg := print.NewRegistry()
	reg.Register(poType, titleView)
	h := web.Routes(web.Config{DB: testkit.NewDB(t), Auth: fa, Info: df, Letterhead: printtest.Letterhead, Registry: reg})

	ref := &auth.RecordRef{DocType: poType, ID: 1, ProjectIDs: []int64{prjTechpark}}
	if err := fa.Can(t.Context(), kavya, poType, "read", ref); err != nil {
		t.Fatalf("setup: Kavya must be able to read the PO: %v", err)
	}
	if err := fa.Can(t.Context(), rv, poType, "write", ref); err == nil {
		t.Fatalf("setup: Ravi must not have write on the PO")
	}

	rec := get(t, h, priya, "/print/test.PurchaseOrder/1")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), notFoundMsg) {
		t.Errorf("Priya (restricted to PRJ-METRO): got %d %q, want 404 containing %q", rec.Code, rec.Body.String(), notFoundMsg)
	}

	rec = get(t, h, kavya, "/print/test.PurchaseOrder/1")
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), forbiddenMsg) {
		t.Errorf("Kavya (read, no print): got %d %q, want 403 containing %q", rec.Code, rec.Body.String(), forbiddenMsg)
	}

	rec = get(t, h, rv, "/print/test.PurchaseOrder/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("Ravi (read, print, no write): got %d %q, want 200", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PO/26-27/00001") {
		t.Errorf("Ravi's print lacks PO/26-27/00001:\n%s", rec.Body.String())
	}
}

// FILE-055: request parameters can't remove the watermark.
func TestPrint_WatermarkNotOverridable(t *testing.T) {
	fa := authfake.New()
	seedRights(fa)
	rv := ravi(t, fa)
	df := docfake.New()
	df.SetInfo(doctype.Info{DocType: poType, DocLabel: "Purchase Order", ID: 42, State: doctype.StateDraft, Display: "Draft 42"})
	reg := print.NewRegistry()
	reg.Register(poType, titleView)
	h := web.Routes(web.Config{DB: testkit.NewDB(t), Auth: fa, Info: df, Letterhead: printtest.Letterhead, Registry: reg})

	rec := get(t, h, rv, "/print/test.PurchaseOrder/42?watermark=0&no_draft=1&status=approved")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %q, want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		`<div class="watermark" data-status="draft">DRAFT</div>`,
		`<span class="doc_badge" data-status="draft">DRAFT</span>`,
		"Draft 42",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("print lacks %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "PO/26-27/") {
		t.Errorf("draft print shows a PO number:\n%s", body)
	}
}

// tableServer returns the print routes over print_test_po with Ravi signed in.
func tableServer(t *testing.T) (http.Handler, *db.DB, auth.Principal) {
	t.Helper()
	d := testkit.NewDB(t)
	printtest.CreateTable(t, d)
	fa := authfake.New()
	seedRights(fa)
	rv := ravi(t, fa)
	reg := print.NewRegistry()
	reg.Register(printtest.TableDocType, printtest.TableView)
	h := web.Routes(web.Config{DB: d, Auth: fa, Info: printtest.TableInfo{}, Letterhead: printtest.Letterhead, Registry: reg})
	return h, d, rv
}

func exec(t *testing.T, d *db.DB, q string, args ...any) {
	t.Helper()
	err := d.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), q, args...)
		return err
	})
	if err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func insertPO1(t *testing.T, d *db.DB) {
	t.Helper()
	exec(t, d, `INSERT INTO print_test_po (id, state, number, party_name, party_address, version, updated_at)
		VALUES (1, 'approved', 'PO/26-27/00001', 'Shree Steel Traders', 'Mumbai, Maharashtra', 1, ?)`, fixedUpdatedAt)
}

// FILE-062: printing doesn't change the document.
func TestPrint_IsReadOnly(t *testing.T) {
	h, d, rv := tableServer(t)
	insertPO1(t, d)

	for i := range 3 {
		rec := get(t, h, rv, "/print/test.PurchaseOrder/1")
		if rec.Code != http.StatusOK {
			t.Fatalf("print %d: got %d %q, want 200", i+1, rec.Code, rec.Body.String())
		}
	}

	var version int64
	var updatedAt string
	err := d.Read(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(),
			`SELECT version, updated_at FROM print_test_po WHERE id = 1`).Scan(&version, &updatedAt)
	})
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if version != 1 || updatedAt != fixedUpdatedAt {
		t.Errorf("after 3 prints: version %d, updated_at %q; want 1, %q", version, updatedAt, fixedUpdatedAt)
	}

	// The handler uses only DB.Read: no two-argument Write(ctx, fn) call in
	// print/web's non-test sources, so a later change fails loudly.
	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	checked := 0
	for _, src := range srcs {
		if strings.HasSuffix(src, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, src, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", src, err)
		}
		checked++
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Write" && len(call.Args) == 2 {
				t.Errorf("%s: print/web calls a database Write; printing must use DB.Read only", fset.Position(call.Pos()))
			}
			return true
		})
	}
	if checked == 0 {
		t.Errorf("no non-test Go files found in print/web")
	}
}

// FILE-063: prints use the party and address saved on the document.
func TestPrint_UsesDocumentSnapshot(t *testing.T) {
	h, d, rv := tableServer(t)
	insertPO1(t, d)
	exec(t, d, `INSERT INTO print_test_party (id, name, address) VALUES (1, 'Shree Steel Traders', 'Mumbai, Maharashtra')`)
	exec(t, d, `UPDATE print_test_party SET name = 'SG Steel Pvt Ltd', address = 'Navi Mumbai' WHERE id = 1`)

	rec := get(t, h, rv, "/print/test.PurchaseOrder/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %q, want 200", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{"Shree Steel Traders", "Mumbai, Maharashtra"} {
		if !strings.Contains(body, want) {
			t.Errorf("print lacks the saved party value %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"SG Steel", "Navi Mumbai"} {
		if strings.Contains(body, bad) {
			t.Errorf("print shows the current master value %q:\n%s", bad, body)
		}
	}
}

// Supporting: the print response headers and the stylesheet route.
func TestPrint_HeadersAndCSS(t *testing.T) {
	h, d, rv := tableServer(t)
	insertPO1(t, d)

	rec := get(t, h, rv, "/print/test.PurchaseOrder/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("print: got %d %q, want 200", rec.Code, rec.Body.String())
	}
	for name, want := range map[string]string{
		"Content-Type":            "text/html; charset=utf-8",
		"Content-Security-Policy": "default-src 'self'; img-src 'self' data:",
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "private, no-store",
	} {
		if got := rec.Header().Get(name); got != want {
			t.Errorf("print header %s = %q, want %q", name, got, want)
		}
	}

	rec = get(t, h, rv, "/print/assets/print.css")
	if rec.Code != http.StatusOK {
		t.Fatalf("print.css: got %d, want 200", rec.Code)
	}
	if got, want := rec.Header().Get("Content-Type"), "text/css; charset=utf-8"; got != want {
		t.Errorf("print.css Content-Type = %q, want %q", got, want)
	}
	if print.CSS == "" || rec.Body.String() != print.CSS {
		t.Errorf("print.css body (%d bytes) is not print.CSS (%d bytes)", rec.Body.Len(), len(print.CSS))
	}
}
