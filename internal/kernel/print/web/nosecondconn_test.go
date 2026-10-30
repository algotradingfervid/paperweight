package web_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213: non-Administrators on the real auth.Service with the pool cut to one
// connection, so the print right must be read in serve's read transaction.

// nsType is registered because the real service refuses an unknown DocType;
// poType stays unregistered, which its authfake tests rely on.
const (
	nsType    = "printtest.NoSecondConnPO"
	nsPath    = "/print/" + nsType + "/1"
	nsPrinter = "No Second Conn Printer"
	nsOwn     = "No Second Conn Owner"
	nsViewer  = "No Second Conn Viewer"
)

var _ = func() bool {
	doctype.Register(doctype.DocType{Name: nsType, Label: "Purchase Order", PluralLabel: "Purchase Orders"})
	return true
}()

// nsServer sets up the print routes on the real auth.Service over document 1,
// created by the Administrator, and returns a GET of it on the pool of one.
func nsServer(t *testing.T) func(*testing.T, auth.Principal) *httptest.ResponseRecorder {
	t.Helper()
	ctx := t.Context()
	d := testkit.NewDB(t)
	a := auth.New(auth.Deps{DB: d, Clock: testkit.Clock(), Directory: authfake.NewDirectory(), Audit: auditfake.New()})
	id, err := a.CreateFirstAdmin(ctx, "ns-admin@vardhan.test", "No Second Conn Admin", "Correct-Horse-42")
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin := auth.Principal{ID: id, Roles: []string{auth.RoleAdministrator}}
	for role, rights := range map[string]map[auth.Right]auth.Scope{
		nsPrinter: {auth.RightRead: auth.ScopeAll, auth.RightPrint: auth.ScopeAll},
		nsViewer:  {auth.RightRead: auth.ScopeAll},
		nsOwn:     {auth.RightRead: auth.ScopeOwn, auth.RightPrint: auth.ScopeOwn},
	} {
		if err := a.CreateRole(ctx, admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		if err := a.SetRights(ctx, admin, role, nsType, rights); err != nil {
			t.Fatalf("SetRights(%s): %v", role, err)
		}
	}
	df := docfake.New()
	df.SetInfo(doctype.Info{
		DocType: nsType, DocLabel: "Purchase Order", ID: 1, State: doctype.StateApproved,
		Number: "PO/26-27/00001", Display: "PO/26-27/00001", CreatedBy: admin.ID,
	})
	reg := print.NewRegistry()
	reg.Register(nsType, titleView)
	h := web.Routes(web.Config{DB: d, Auth: a, Info: df, Letterhead: printtest.Letterhead, Registry: reg})
	return func(t *testing.T, p auth.Principal) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		err := testkit.NoSecondConn(t, d, func(ctx context.Context) error {
			h.ServeHTTP(rec, httptest.NewRequestWithContext(auth.WithPrincipal(ctx, p), http.MethodGet, nsPath, nil))
			return nil
		})
		if err != nil {
			t.Fatalf("NoSecondConn: %v", err)
		}
		return rec
	}
}

// nsUser is Priya holding only role.
func nsUser(t *testing.T, role string) auth.Principal {
	t.Helper()
	p := testkit.Principal(t, "priya@vardhan.test")
	p.Roles = []string{role}
	return p
}

// The print handler checks the print right on the record inside its read.
func TestNoSecondConn_PrintAllowed(t *testing.T) {
	get := nsServer(t)
	rec := get(t, nsUser(t, nsPrinter))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "PO/26-27/00001") {
		t.Errorf("got %d %q, want 200 with the number", rec.Code, rec.Body.String())
	}
}

// Read without print on the record is the 403 with the DocType label.
func TestNoSecondConn_PrintForbidden(t *testing.T) {
	get := nsServer(t)
	rec := get(t, nsUser(t, nsViewer))
	if want := forbiddenMsg + "\n"; rec.Code != http.StatusForbidden || rec.Body.String() != want {
		t.Errorf("got %d %q, want 403 %q", rec.Code, rec.Body.String(), want)
	}
}

// Own scope on another user's record is the 404.
func TestNoSecondConn_PrintNotFound(t *testing.T) {
	get := nsServer(t)
	rec := get(t, nsUser(t, nsOwn))
	if want := notFoundMsg + "\n"; rec.Code != http.StatusNotFound || rec.Body.String() != want {
		t.Errorf("got %d %q, want 404 %q", rec.Code, rec.Body.String(), want)
	}
}
