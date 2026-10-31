package app_test

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-PO P15-0 (hooks spec §3, HOOK-001's rule for a module): the app wires
// purchase's Modules for Order and Request, and the Order pages no screen
// task has changed yet (delete, and the close and cancel reason pages while
// Reason is P15b's stub) render byte-identically to the same page with no
// Module. P15d built the Request's Module and P15a the Order's steps, check
// page, Hide and line cards, so those pages differ by design (purchase/web's
// p15d and p15a tests).
func TestApp_PurchaseModulesUnchanged(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	id := func(table, code string) string {
		t.Helper()
		return w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
	}
	header := map[string]string{"site_id": id("site", "METRO-SITE"), "project_id": id("project", "PRJ-METRO")}
	line := map[string]string{"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"), "qty": "100"}
	save := func(dt string, values, line map[string]string, submit bool) string {
		t.Helper()
		res, err := w.app.Engine().Do(ctx, admin, dt, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: []map[string]string{line}})
		if err != nil {
			t.Fatalf("save %s: %v", dt, err)
		}
		if submit {
			if res, err = w.app.Engine().Do(ctx, admin, dt, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || res.State != doctype.StateApproved {
				t.Fatalf("submit %s %d = %s, %v; want approved", dt, res.ID, res.State, err)
			}
		}
		return strconv.FormatInt(res.ID, 10)
	}
	poValues := map[string]string{"vendor_id": id("party", "SHREE"), "order_date": "2026-10-24"}
	maps.Copy(poValues, header)
	poLine := map[string]string{"price": "62.5", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216"}
	maps.Copy(poLine, line)

	po := "/doc/" + purchase.DocType + "/"
	poDraft, poDone := po+save(purchase.DocType, poValues, poLine, false), po+save(purchase.DocType, poValues, poLine, true)
	paths := []string{poDraft + "/delete", poDone + "/close", poDone + "/cancel"}

	withMods := w.app.DocDeps()
	for _, dt := range []string{purchase.DocType, purchase.RequestDocType} {
		if _, ok := withMods.Modules[dt]; !ok {
			t.Fatalf("docscreen Deps.Modules has no %s entry", dt)
		}
	}
	none := withMods
	none.Modules = nil
	type page struct {
		code     int
		location string
		body     string
	}
	render := func(d docscreen.Deps, p auth.Principal, path string) page {
		d.Principal = func(*http.Request) (auth.Principal, bool) { return p, true }
		r := chi.NewRouter()
		docscreen.Routes(d)(r)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return page{rec.Code, rec.Header().Get("Location"), rec.Body.String()}
	}
	check := func(p auth.Principal, path string) {
		t.Helper()
		got, want := render(withMods, p, path), render(none, p, path)
		if want.code != http.StatusOK && want.code != http.StatusSeeOther {
			t.Errorf("%s GET %s without a Module = %d, want 200 or 303", p.Name, path, want.code)
		}
		if got != want {
			t.Errorf("%s GET %s differs with purchase's stub Module:\nwith: %+v\nwithout: %+v", p.Name, path, got, want)
		}
	}
	for _, path := range paths {
		check(admin, path)
	}
}
