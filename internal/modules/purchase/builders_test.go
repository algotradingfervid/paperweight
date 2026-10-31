package purchase_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-PO P1 Do 7 (CD-272 research patterns, CD-257, CD-280 D7 (b)): the
// purchasetest builders reach draft and approved through the real Engine as
// the fixture users (the reference PO by Priya, PR-A by Suresh), never with
// SQL inserts, so each step leaves DOC's audit entries. The amount stub is 0,
// below every approval rule, so an Order approves on submit; a Request isn't
// approvable and approves on submit too. StopAtPending is proven in P2.
func TestPurchasetest_Builders(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()

	// audits is the document's audit trail as "action>to_state", oldest first.
	audits := func(dt string, id int64) []string {
		t.Helper()
		var actions []string
		rows, err := env.DB.SQL().QueryContext(ctx, `SELECT action || '>' || to_state FROM audit_log WHERE doc_type = ? AND doc_id = ? ORDER BY id`, dt, id)
		if err != nil {
			t.Fatalf("audit of %s %d: %v", dt, id, err)
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				t.Fatal(err)
			}
			actions = append(actions, a)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return actions
	}
	load := func(dt string, id int64) doc.Doc {
		t.Helper()
		var d doc.Doc
		if err := env.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			d, err = doc.LoadDoc(ctx, tx, dt, id)
			return err
		}); err != nil {
			t.Fatalf("load %s %d: %v", dt, id, err)
		}
		return d
	}

	cases := []struct {
		what   string
		build  func() doc.Doc
		dt     string
		owner  types.UserID
		state  doctype.State
		number string
	}{
		{"order draft", func() doc.Doc { return purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()) },
			purchase.DocType, env.Priya.ID, doctype.StateDraft, ""},
		{"order approved", func() doc.Doc { return purchasetest.NewOrder(t, env, purchasetest.StopAtApproved()) },
			purchase.DocType, env.Priya.ID, doctype.StateApproved, "PO/26-27/00001"},
		{"second order approved", func() doc.Doc { return purchasetest.NewOrder(t, env, purchasetest.StopAtApproved()) },
			purchase.DocType, env.Priya.ID, doctype.StateApproved, "PO/26-27/00002"},
		{"request draft", func() doc.Doc { return purchasetest.NewRequest(t, env, purchasetest.StopAtDraft()) },
			purchase.RequestDocType, env.Suresh.ID, doctype.StateDraft, ""},
		{"request approved", func() doc.Doc { return purchasetest.NewRequest(t, env, purchasetest.StopAtApproved()) },
			purchase.RequestDocType, env.Suresh.ID, doctype.StateApproved, "PR/26-27/00001"},
	}
	for _, c := range cases {
		got := c.build()
		if got.DocType != c.dt || got.ID <= 0 || got.State != c.state || got.Number != c.number || got.CreatedBy != c.owner {
			t.Errorf("%s = %s %d state %q number %q by %d; want %s, state %q number %q by %d",
				c.what, got.DocType, got.ID, got.State, got.Number, got.CreatedBy, c.dt, c.state, c.number, c.owner)
		}
		if len(got.Lines) == 0 {
			t.Errorf("%s has no lines, want the default line", c.what)
		}
		// The returned Doc is the stored one.
		stored := load(c.dt, got.ID)
		if stored.State != got.State || stored.Number != got.Number || stored.Version != got.Version || len(stored.Lines) != len(got.Lines) {
			t.Errorf("%s: stored %+v differs from the builder's %+v", c.what, stored, got)
		}
		// DOC wrote the history: the create first, and the last entry
		// moved the document to the state the builder stopped at.
		acts := audits(c.dt, got.ID)
		if len(acts) == 0 || acts[0] != "create>draft" || !strings.HasSuffix(acts[len(acts)-1], ">"+string(c.state)) {
			t.Errorf("%s audit trail = %v, want DOC's create>draft first and the last entry ending in %s (built through Engine.Do)", c.what, acts, c.state)
		}
	}
}

// MOD-PO P1 Do 7 (00-fixtures.md "Approval rules (test seed)", "Module-only
// users"; CD-257): the env has the fixture users including arjun, the clock
// at authtest.T0, and the seed's purchase.Order rules (Purchase Manager from
// ₹50,000, Director from ₹5,00,000); WithNoRules() has none.
func TestPurchasetest_Env(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()

	if now := env.Clock.Now(); !now.Equal(authtest.T0) {
		t.Errorf("env clock = %v, want authtest.T0 %v", now, authtest.T0)
	}
	for _, c := range []struct {
		p     auth.Principal
		name  string
		roles []string
	}{
		{env.Arjun, "Arjun Mehta", []string{"Employee", "Purchase User"}},
		{env.Priya, "Priya Shah", []string{"Employee", "Purchase User"}},
		{env.Ravi, "Ravi Kulkarni", []string{"Employee", "Purchase Manager"}},
	} {
		if c.p.ID <= 0 || c.p.Name != c.name {
			t.Errorf("principal = %+v, want %s", c.p, c.name)
			continue
		}
		for _, role := range c.roles {
			if ok, err := env.Auth.HasRole(ctx, c.p.ID, role); err != nil || !ok {
				t.Errorf("%s HasRole(%s) = %v, %v; want true", c.name, role, ok, err)
			}
		}
	}

	rulesOf := func(e *purchasetest.Env) []string {
		t.Helper()
		rules, err := e.Approvals.Rules(ctx, e.Admin)
		if err != nil {
			t.Fatalf("Rules: %v", err)
		}
		var out []string
		for _, r := range rules {
			if r.DocType == purchase.DocType {
				out = append(out, types.FormatMoney(r.FromAmount)+" "+string(r.ApproverType)+" "+r.Role)
			}
		}
		slices.Sort(out) // order-free
		return out
	}
	want := []string{"5,00,000.00 role Director", "50,000.00 role Purchase Manager"}
	if got := rulesOf(env); !slices.Equal(got, want) {
		t.Errorf("purchase.Order rules = %q, want %q", got, want)
	}
	if got := rulesOf(purchasetest.New(t, purchasetest.WithNoRules())); len(got) != 0 {
		t.Errorf("WithNoRules: purchase.Order rules = %q, want none", got)
	}
}

// MOD-PO P1 Do 7 (CD-280 D7 (b)): SetReceived stands in for the GRN roll-up.
// It sets one line's received_qty with one UPDATE and leaves the header's
// version alone; setting 0 undoes it.
func TestPurchasetest_SetReceived(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{}, map[string]string{}))
	if len(po.Lines) != 2 {
		t.Fatalf("order has %d lines, want 2", len(po.Lines))
	}
	five, err := types.ParseQty("5")
	if err != nil {
		t.Fatal(err)
	}
	received := func() (first, second types.Qty, version int) {
		t.Helper()
		for i, l := range po.Lines {
			var q types.Qty
			if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT received_qty FROM purchase_order_line WHERE id = ?`, l.ID).Scan(&q); err != nil {
				t.Fatalf("received_qty of line %d: %v", l.ID, err)
			}
			if i == 0 {
				first = q
			} else {
				second = q
			}
		}
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, po.ID).Scan(&version); err != nil {
			t.Fatal(err)
		}
		return first, second, version
	}

	purchasetest.SetReceived(t, env, po.Lines[0].ID, five)
	if a, b, v := received(); a != five || b != 0 || v != po.Version {
		t.Errorf("after SetReceived(5): lines %v, %v, version %d; want 5, 0, %d", a, b, v, po.Version)
	}
	purchasetest.SetReceived(t, env, po.Lines[0].ID, 0)
	if a, b, v := received(); a != 0 || b != 0 || v != po.Version {
		t.Errorf("after SetReceived(0): lines %v, %v, version %d; want 0, 0, %d", a, b, v, po.Version)
	}
}
