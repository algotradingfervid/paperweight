package naming_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// realNaming is DOC's fixture harness with its engine rebuilt on the real
// Namer (fixture.Order's series PO/{FY}/{#####}) and NAM's number rules.
func realNaming(t *testing.T) (*fixturetest.H, *naming.Namer) {
	t.Helper()
	h := fixturetest.New(t)
	nm := naming.New(h.DB, h.Clock, h.Auth, audit.Default)
	dt, ok := doc.Lookup(fixture.Order)
	if !ok {
		t.Fatalf("doc.Lookup(%s): not registered", fixture.Order)
	}
	if err := nm.EnsureSeries(context.Background(), fixture.Order, dt.Label, "PO/{FY}/{#####}"); err != nil {
		t.Fatalf("EnsureSeries(%s): %v", fixture.Order, err)
	}
	h.Engine = doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: h.Auth, Naming: nm, Approval: h.Approvals, Files: h.Files,
	})
	return h, nm
}

// TestAmend_RequiresCancelled covers NAM-047.
func TestAmend_RequiresCancelled(t *testing.T) {
	h, nm := realNaming(t)
	ctx := context.Background()
	if err := nm.SetCounter(ctx, h.Admin, fixture.Order, "PO/26-27/", 6); err != nil {
		t.Fatalf("SetCounter(PO/26-27/, 6): %v", err)
	}
	d := h.ApprovedOrder(t, fixturetest.OL{Item: "MS-ANGLE-50", Qty: "10"})
	if d.Number != "PO/26-27/00007" {
		t.Fatalf("approved order number = %q, want PO/26-27/00007", d.Number)
	}

	_, err := h.Do(t, ctx, h.Ravi, fixture.Order, d.ID, doc.ActionAmend, doc.ActionInput{Version: d.Version})
	fixturetest.ErrIs(t, err, doc.KindRefused,
		"Only cancelled documents can be amended. Cancel PO/26-27/00007 first.")

	var n int
	if err := h.DB.Read(ctx, func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx,
			`SELECT COUNT(*) FROM naming_issued WHERE root = ? AND amendment_no > 0`, "PO/26-27/00007").Scan(&n)
	}); err != nil {
		t.Fatalf("count amendment ledger rows: %v", err)
	}
	if n != 0 {
		t.Errorf("naming_issued has %d amendment rows for PO/26-27/00007, want 0", n)
	}
}

// TestAssign_AuditedWithTransition covers NAM-064.
func TestAssign_AuditedWithTransition(t *testing.T) {
	h, _ := realNaming(t)
	d := h.PendingOrder(t)
	h.MustDo(t, context.Background(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove, doc.ActionInput{Version: d.Version})

	entries := h.Audit(t, fixture.Order, d.ID)
	var approve *audit.Entry
	for i := range entries {
		if entries[i].Action == "approve" {
			approve = &entries[i]
		}
	}
	if approve == nil {
		t.Fatalf("no approve audit entry in %+v", entries)
	}
	if approve.From != string(doc.StatePending) || approve.To != string(doc.StateApproved) || approve.By != h.Ravi.ID {
		t.Errorf("approve entry = %s → %s by %d, want %s → %s by %d",
			approve.From, approve.To, approve.By, doc.StatePending, doc.StateApproved, h.Ravi.ID)
	}
	var found bool
	for _, c := range approve.Changes {
		if c.Field == "number" {
			found = true
			if c.Before != "" || c.After != "PO/26-27/00001" {
				t.Errorf("number change = %q → %q, want \"\" → PO/26-27/00001", c.Before, c.After)
			}
		}
	}
	if !found {
		t.Errorf("approve entry has no number change: %+v", approve.Changes)
	}
}

// TestPrint_DraftLabel covers NAM-065.
func TestPrint_DraftLabel(t *testing.T) {
	h, _ := realNaming(t)
	fa := authfake.New()
	fa.Grant("Purchase Manager", fixture.Order, "read", auth.ScopeAll) // print needs read too, as in the real service
	fa.Grant("Purchase Manager", fixture.Order, "print", auth.ScopeAll)
	fa.AddRole(h.Ravi.ID, "Purchase Manager")
	reg := print.NewRegistry()
	printtest.RegisterFixtureViews(reg)
	srv := printweb.Routes(printweb.Config{DB: h.DB, Auth: fa, Info: h.Engine, Letterhead: printtest.Letterhead, Registry: reg})

	const watermark = `<div class="watermark" data-status="draft">DRAFT</div>`
	res := h.RefPO(t)
	draft := "Draft " + strconv.FormatInt(res.ID, 10)
	printPO := func(when string) string {
		t.Helper()
		req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), h.Ravi), http.MethodGet,
			"/print/"+fixture.Order+"/"+strconv.FormatInt(res.ID, 10), nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: print got %d %q, want 200", when, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}
	wantDraft := func(when string) {
		t.Helper()
		body := printPO(when)
		for _, want := range []string{draft, watermark} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: print lacks %q:\n%s", when, want, body)
			}
		}
		if strings.Contains(body, "PO/26-27/") {
			t.Errorf("%s: print shows a PO number:\n%s", when, body)
		}
	}

	wantDraft("draft")
	sub := h.MustDo(t, context.Background(), h.Priya, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if sub.State != doc.StatePending {
		t.Fatalf("submit state = %s, want %s", sub.State, doc.StatePending)
	}
	wantDraft("pending_approval")

	h.MustDo(t, context.Background(), h.Ravi, fixture.Order, res.ID, doc.ActionApprove, doc.ActionInput{Version: sub.Version})
	body := printPO("approved")
	if !strings.Contains(body, "PO/26-27/00001") {
		t.Errorf("approved: print lacks PO/26-27/00001:\n%s", body)
	}
	if strings.Contains(body, `class="watermark"`) || strings.Contains(body, draft) {
		t.Errorf("approved: print still has a watermark or %q:\n%s", draft, body)
	}
}
