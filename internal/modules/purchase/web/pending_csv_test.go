package purchaseweb_test

import (
	"bytes"
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	purchaseweb "github.com/vardhan/internaltools/internal/modules/purchase/web"
	"github.com/vardhan/internaltools/internal/types"
)

// p9ExportPath is the pending report's CSV (PO spec §13).
const p9ExportPath = "/reports/purchase/pending/export.csv"

// p9ExportDenied is AUTH's export refusal for Purchase Order (AUTH-080),
// copied exactly (CD-142).
const p9ExportDenied = "You don't have permission to export Purchase Order. Ask your administrator for a role that allows it."

// p9Header is the CSV header: PO spec §13's 15 columns in its order, with
// Expected on (§27.1).
var p9Header = []string{
	"PO number", "Order date", "Required by", "Expected on", "Vendor", "Project", "Site",
	"Item code", "Item name", "Ordered", "Received", "Pending", "UoM", "Net rate", "Pending value",
}

// p9BOM is the UTF-8 byte order mark csvio's exports start with.
var p9BOM = string([]byte{0xEF, 0xBB, 0xBF})

// p9Get sends GET path as p to the module routes on a bare router (AUTH's
// read guard is internal/app's; the handler checks export, CD-145).
func p9Get(t *testing.T, env *purchasetest.Env, p auth.Principal, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	purchaseweb.Routes(purchaseweb.Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: scs.New(),
		CSRF: func(*http.Request) string { return "p9-token" },
	})(r)
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// p9Records parses a CSV answer (a leading BOM allowed) into its records.
func p9Records(t *testing.T, what string, rec *httptest.ResponseRecorder) [][]string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d %q, want 200", what, rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("%s Content-Type = %q, want text/csv", what, ct)
	}
	body := bytes.TrimPrefix(rec.Body.Bytes(), []byte(p9BOM))
	rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		t.Fatalf("%s: parse CSV: %v\n%s", what, err, body)
	}
	if len(rows) == 0 || !slices.Equal(rows[0], p9Header) {
		t.Fatalf("%s header = %q, want the 15 columns %q", what, rows, p9Header)
	}
	return rows[1:]
}

// p9Numbers is the sorted PO numbers (first column) of the records.
func p9Numbers(rows [][]string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r[0])
	}
	slices.Sort(out)
	return out
}

func p9ID(t *testing.T, env *purchasetest.Env, table, code string) string {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}

// p9TechparkOrder is an approved PRJ-TECHPARK / TECH-SITE PO by Arjun through
// Engine.Do (the builders save as Priya, who may not create it), 10 kg
// MS-ANGLE-50 at ₹62.50, required by 26-10-2026.
func p9TechparkOrder(t *testing.T, env *purchasetest.Env) string {
	t.Helper()
	ctx := t.Context()
	in := doc.ActionInput{
		Values: map[string]string{
			"vendor_id": p9ID(t, env, "party", "SHREE"), "site_id": p9ID(t, env, "site", "TECH-SITE"),
			"project_id": p9ID(t, env, "project", "PRJ-TECHPARK"), "order_date": types.Today(env.Clock).ISO(),
			"required_by": "2026-10-26",
		},
		Lines: []map[string]string{{
			"item_id": p9ID(t, env, "item", "MS-ANGLE-50"), "uom_id": p9ID(t, env, "uom", "KGS"),
			"qty": "10", "price": "62.5", "tax_code_id": p9ID(t, env, "tax_code", "GST18"), "hsn_sac": "7216",
		}},
	}
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, in)
	if err != nil {
		t.Fatalf("save PRJ-TECHPARK PO as Arjun: %v", err)
	}
	if res, err = env.Engine.Do(ctx, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil || res.State != doctype.StateApproved {
		t.Fatalf("submit PRJ-TECHPARK PO as Arjun = %s, %v; want approved", res.State, err)
	}
	var number string
	if err := env.DB.SQL().QueryRowContext(ctx, `SELECT number FROM purchase_order WHERE id = ?`, res.ID).Scan(&number); err != nil {
		t.Fatalf("number of PO %d: %v", res.ID, err)
	}
	return number
}

// PO-038 (PO spec §13, §18; CD-145; AUTH-080) and the CSV part of PO-081
// (§27.1): GET …/export.csv checks `export` in the handler. Suresh (Stores:
// read, no export) gets 403 with AUTH's export text and no CSV. Priya
// (export) gets a CSV with the 15 §13 columns, Expected on included, scoped
// to PRJ-METRO (AUTH-110); `overdue=1` keeps only overdue rows, with the due
// date from Expected on ahead of required_by. Values follow csvio's export
// (ISO dates, plain numbers).
func TestPO_PendingExportRight(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	// m1: due 28-10 but Expected on 30-10, still overdue on 31-10; m2: due
	// 28-10, Expected on 05-11, not overdue; the PRJ-TECHPARK PO is overdue
	// but outside Priya's scope.
	m1 := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}))
	purchasetest.SetReceived(t, env, m1.Lines[0].ID, p9Qty(t, "40"))
	m2 := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "28-10-2026"}))
	tech := p9TechparkOrder(t, env)
	for _, s := range []struct {
		id int64
		on types.Date
	}{{m1.ID, types.Date{Year: 2026, Month: time.October, Day: 30}}, {m2.ID, types.Date{Year: 2026, Month: time.November, Day: 5}}} {
		if err := env.Service.SetExpected(ctx, env.Arjun, s.id, 0, s.on, ""); err != nil {
			t.Fatalf("SetExpected(%d, %s): %v", s.id, s.on.ISO(), err)
		}
	}
	env.SetNow(t, time.Date(2026, time.October, 31, 10, 30, 0, 0, types.IST))

	t.Run("read without export is refused", func(t *testing.T) {
		for _, path := range []string{p9ExportPath, p9ExportPath + "?overdue=1"} {
			rec := p9Get(t, env, env.Suresh, path)
			body := rec.Body.String()
			if rec.Code != http.StatusForbidden || !strings.Contains(body, p9ExportDenied) {
				t.Errorf("GET %s as Suresh = %d %q, want 403 with %q", path, rec.Code, body, p9ExportDenied)
			}
			if strings.Contains(body, "PO number") || strings.Contains(body, m1.Number) || strings.HasPrefix(rec.Header().Get("Content-Type"), "text/csv") {
				t.Errorf("GET %s as Suresh leaked the CSV: %q", path, body)
			}
		}
	})

	t.Run("export gives the scoped CSV", func(t *testing.T) {
		rows := p9Records(t, "Priya's CSV", p9Get(t, env, env.Priya, p9ExportPath))
		if got, want := p9Numbers(rows), []string{m1.Number, m2.Number}; !slices.Equal(got, want) {
			t.Errorf("Priya's CSV rows = %q, want %q (PRJ-METRO only, never %s)", got, want, tech)
		}
		for _, r := range rows {
			if len(r) != len(p9Header) {
				t.Errorf("row %q has %d columns, want %d", r, len(r), len(p9Header))
			}
		}
		want := []string{
			m1.Number, "2026-10-24", "2026-10-28", "2026-10-30", "Shree Steel Traders", "PRJ-METRO", "METRO-SITE",
			"MS-ANGLE-50", "MS Angle 50x50x5", p9Qty(t, "100").Plain(), p9Qty(t, "40").Plain(), p9Qty(t, "60").Plain(), "KGS",
			p9Rate(t, "62.5").Plain(), p9Money(t, "3750").Plain(),
		}
		for _, r := range rows {
			if r[0] == m1.Number && !slices.Equal(r, want) {
				t.Errorf("m1's CSV row =\n%q\nwant\n%q", r, want)
			}
			if r[0] == m2.Number && r[3] != "2026-11-05" {
				t.Errorf("m2's Expected on = %q, want 2026-11-05", r[3])
			}
		}
	})

	t.Run("overdue=1 uses the due date", func(t *testing.T) {
		rows := p9Records(t, "Priya's overdue CSV", p9Get(t, env, env.Priya, p9ExportPath+"?overdue=1"))
		if got, want := p9Numbers(rows), []string{m1.Number}; !slices.Equal(got, want) {
			t.Errorf("Priya's overdue CSV rows = %q, want %q (m2's Expected on 05-11 isn't past)", got, want)
		}
		rows = p9Records(t, "Admin's overdue CSV", p9Get(t, env, env.Admin, p9ExportPath+"?overdue=1"))
		want := []string{m1.Number, tech}
		slices.Sort(want)
		if got := p9Numbers(rows); !slices.Equal(got, want) {
			t.Errorf("Admin's overdue CSV rows = %q, want %q", got, want)
		}
	})
}

func p9Qty(t *testing.T, s string) types.Qty {
	t.Helper()
	q, err := types.ParseQty(s)
	if err != nil {
		t.Fatalf("ParseQty(%q): %v", s, err)
	}
	return q
}

func p9Rate(t *testing.T, s string) types.Rate {
	t.Helper()
	r, err := types.ParseRate(s)
	if err != nil {
		t.Fatalf("ParseRate(%q): %v", s, err)
	}
	return r
}

func p9Money(t *testing.T, s string) types.Money {
	t.Helper()
	m, err := types.ParseMoney(s)
	if err != nil {
		t.Fatalf("ParseMoney(%q): %v", s, err)
	}
	return m
}
