package purchaseweb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// Helpers for P15a's tests (Order wizard; UX §5.4 to §5.6, §5.8). All names
// carry p15a. The pages are P15d's p15dWeb over a purchase world.

// newP15aWeb is newP15dWeb on env, with svc as the screens' Service (nil is
// env's).
func newP15aWeb(t *testing.T, env *purchasetest.Env, svc *purchase.Service) *p15dWeb {
	t.Helper()
	if svc == nil {
		svc = env.Service
	}
	sm := scs.New()
	d := Deps{
		Service: svc, Approvals: env.Approvals, Comments: env.Comments, Sessions: sm,
		CSRF: func(*http.Request) string { return "p15a-token" },
	}
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{
		DB: env.DB, Engine: env.Engine, Auth: env.Auth, Approval: env.Approvals, Extras: docscreentest.NewExtras(),
		Today: func() (string, string) {
			today := types.Today(env.Clock)
			return types.FormatDate(today), types.FormatDate(types.AddDays(today, -1))
		},
		Modules: Modules(d),
	})(r)
	Routes(d)(r)
	r.Get("/p15a-flash", func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewEncoder(w).Encode(web.TakeFlash(req.Context()))
	})
	h := sm.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.ServeHTTP(w, req.WithContext(web.WithSessions(req.Context(), sm)))
	}))
	return &p15dWeb{env: env, h: h}
}

// p15aCement is a line of qty Bag of CEMENT-OPC at price (blank: the
// default), GST18, HSN 2523.
func p15aCement(t *testing.T, w *p15dWeb, qty, price string) map[string]string {
	t.Helper()
	l := map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "qty": qty,
		"tax_code_id": w.id(t, "tax_code", "GST18"), "hsn_sac": "2523", "price": price,
	}
	return l
}

// p15aSource is Priya's approved SHREE PO on PRJ-METRO of 40 Bag CEMENT-OPC at
// ₹345.00, ordered 12-10-2026 (PUX-011).
func p15aSource(t *testing.T, w *p15dWeb) doc.Doc {
	t.Helper()
	return purchasetest.NewOrder(t, w.env, purchasetest.OrderHeader(map[string]string{"order_date": "12-10-2026"}),
		purchasetest.OrderLines(p15aCement(t, w, "40", "345")))
}

// p15aNote is the last-price note for src's ₹345.00 (UX §5.6).
func p15aNote(src doc.Doc) string {
	return "Last bought at ₹345.00 from Shree Steel Traders on 12-10-2026 (" + src.Number + ")"
}

// p15aSave creates a PO as p through Engine.Do with header and lines, stopping
// at a draft, and returns its id.
func p15aSave(t *testing.T, env *purchasetest.Env, p auth.Principal, header map[string]string, lines ...map[string]string) int64 {
	t.Helper()
	res, err := env.Engine.Do(t.Context(), p, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{Values: header, Lines: lines})
	if err != nil {
		var he doctype.HTTPError
		var fields []doctype.FieldError
		if errors.As(err, &he) {
			fields = he.FieldErrors()
		}
		t.Fatalf("save PO as %s: %v %+v", p.Name, err, fields)
	}
	return res.ID
}

// p15aHeader is a PO header of vendor, site and project codes (blank codes
// leave the field blank), ordered today.
func p15aHeader(t *testing.T, w *p15dWeb, vendor, siteCode, projectCode string) map[string]string {
	t.Helper()
	h := map[string]string{"order_date": types.Today(w.env.Clock).ISO()}
	for field, c := range map[string][2]string{"vendor_id": {"party", vendor}, "site_id": {"site", siteCode}, "project_id": {"project", projectCode}} {
		if c[1] != "" {
			h[field] = w.id(t, c[0], c[1])
		}
	}
	return h
}

// p15aKit is MST's Env over env, for the master services.
func p15aKit(env *purchasetest.Env) mstkit.Env {
	return mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}
}

// p15aDisable disables the payment terms or item with code through its
// service, as an admin would.
func p15aDisable(t *testing.T, env *purchasetest.Env, table, code string) {
	t.Helper()
	var svc interface {
		SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error
	}
	switch table {
	case "payment_terms":
		svc = paymentterms.New(p15aKit(env))
	case "item":
		svc = item.New(p15aKit(env))
	default:
		t.Fatalf("p15aDisable: no service for %s", table)
	}
	ctx := t.Context()
	if err := env.DB.Write(ctx, func(tx *db.Tx) error {
		var id int64
		var version int
		if err := tx.SQL().QueryRowContext(ctx, `SELECT id, version FROM `+table+` WHERE code = ?`, code).Scan(&id, &version); err != nil { //nolint:gosec // G202: fixed table names
			return err
		}
		return svc.SetDisabledTx(ctx, tx, env.Admin, id, version, true)
	}); err != nil {
		t.Fatalf("disable %s %s: %v", table, code, err)
	}
}

// p15aScope is AUTH with Scope counted, and failing with err when set.
type p15aScope struct {
	*auth.Auth
	err   error
	calls atomic.Int32
}

func (a *p15aScope) Scope(ctx context.Context, p auth.Principal, dt string, r auth.Right) (auth.Predicate, error) {
	if dt == purchase.DocType {
		a.calls.Add(1)
	}
	if a.err != nil {
		return auth.Predicate{}, a.err
	}
	return a.Auth.Scope(ctx, p, dt, r)
}

// p15aService is a purchase Service over env with a as its AUTH.
func p15aService(env *purchasetest.Env, a purchase.Auth) *purchase.Service {
	return purchase.New(purchase.ServiceDeps{
		DB: env.DB, Engine: env.Engine, Auth: a, Audit: audit.Default,
		Approvals: env.Approvals, Comments: env.Comments, Clock: env.Clock,
	})
}

// p15aReader is a docscreen.Reader over db that counts its reads and fails
// them with err when set.
type p15aReader struct {
	db    *db.DB
	err   error
	reads atomic.Int32
}

func (r *p15aReader) Read(ctx context.Context, fn func(tx *db.Tx) error) error {
	r.reads.Add(1)
	if r.err != nil {
		return r.err
	}
	return r.db.Read(ctx, fn)
}

var errP15a = errors.New("p15a: read refused")

// p15aLogs captures the default slog logger's output until the test ends.
// Tests using it don't run in parallel.
func p15aLogs(t *testing.T) func() string {
	t.Helper()
	var mu sync.Mutex
	var b bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&p15aWriter{mu: &mu, b: &b}, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return b.String()
	}
}

type p15aWriter struct {
	mu *sync.Mutex
	b  *bytes.Buffer
}

func (w *p15aWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

// p15aContains reports whether s holds each of parts, in order.
func p15aContains(s string, parts ...string) bool {
	at := 0
	for _, p := range parts {
		i := strings.Index(s[at:], p)
		if i < 0 {
			return false
		}
		at += i + len(p)
	}
	return true
}
